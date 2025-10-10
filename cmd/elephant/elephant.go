package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/abenz1267/elephant/v2/internal/comm"
	"github.com/abenz1267/elephant/v2/internal/comm/client"
	"github.com/abenz1267/elephant/v2/internal/providers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/adrg/xdg"
	"github.com/urfave/cli/v3"
)

//go:embed version.txt
var version string

func main() {
	var config string
	var debug bool

	common.LoadGlobalConfig()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGKILL,
		syscall.SIGQUIT, syscall.SIGUSR1)

	go func() {
		<-signalChan
		os.Remove(comm.Socket)
		os.Exit(0)
	}()

	cmd := &cli.Command{
		Name:                   "Elephant",
		Usage:                  "Data provider and executor",
		UseShortOptionHandling: true,
		Commands: []*cli.Command{
			{
				Name:  "service",
				Usage: "manage the user systemd service",
				Commands: []*cli.Command{
					{
						Name:  "enable",
						Usage: "enables the systemd service",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							h := xdg.ConfigHome
							file := filepath.Join(h, "systemd", "user", "elephant.service")
							os.MkdirAll(filepath.Dir(file), 0o755)

							data := `
[Unit]
Description=Elephant
After=graphical-session.target

[Service]
Type=simple
ExecStart=elephant
Restart=on-failure

[Install]
WantedBy=graphical-session.target
							`

							if !common.FileExists(file) {
								err := os.WriteFile(file, []byte(data), 0o755)
								if err != nil {
									slog.Error("service", "enable write file", err)
								}
							}

							sc := exec.Command("systemctl", "--user", "enable", "elephant.service")
							out, err := sc.CombinedOutput()
							if err != nil {
								slog.Error("service", "enable systemd", err, "out", out)
							}

							slog.Info("service", "enable", out)

							return nil
						},
					},
					{
						Name:  "disable",
						Usage: "disables the systemd service",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							sc := exec.Command("systemctl", "--user", "disable", "elephant.service")
							out, err := sc.CombinedOutput()
							if err != nil {
								slog.Error("service", "disable systemd", err, "out", out)
							}

							slog.Info("service", "disable", out)

							h := xdg.ConfigHome
							file := filepath.Join(h, "systemd", "user", "elephant.service")

							err = os.Remove(file)
							if err != nil {
								slog.Error("service", "disable", err)
							}

							return nil
						},
					},
				},
			},
			{
				Name:    "version",
				Aliases: []string{"v"},
				Usage:   "prints the version",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					fmt.Println(version)
					return nil
				},
			},
			{
				Name:    "listproviders",
				Aliases: []string{"l"},
				Usage:   "lists all installed providers",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					logger := slog.New(slog.DiscardHandler)
					slog.SetDefault(logger)

					providers.Load(false)

					for _, v := range providers.Providers {
						if *v.Name == "menus" {
							for _, m := range common.Menus {
								fmt.Printf("%s;menus:%s\n", m.NamePretty, m.Name)
							}
						} else {
							fmt.Printf("%s;%s\n", *v.NamePretty, *v.Name)
						}
					}

					return nil
				},
			},
			{
				Name:    "menu",
				Aliases: []string{"m"},
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name: "menu",
					},
				},
				Usage: "send request to open a menu",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client.RequestMenu(cmd.StringArg("menu"))
					return nil
				},
			},
			{
				Name:    "generatedoc",
				Aliases: []string{"d"},
				Usage:   "generates a markdown documentation",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					logger := slog.New(slog.DiscardHandler)
					slog.SetDefault(logger)

					providers.Load(false)

					util.GenerateDoc()
					return nil
				},
			},
			{
				Name: "query",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:        "async",
						Category:    "",
						DefaultText: "run async, close manually",
						Usage:       "use to not close after querying, in case of async querying.",
					},
				},
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name: "content",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client.Query(cmd.StringArg("content"), cmd.Bool("async"))

					return nil
				},
			},
			{
				Name: "activate",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name: "content",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client.Activate(cmd.StringArg("content"))

					return nil
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Value:       "",
				Destination: &config,
				Usage:       "config folder location",
				Action: func(ctx context.Context, cmd *cli.Command, val string) error {
					common.SetExplicitDir(val)
					return nil
				},
			},
			&cli.BoolFlag{
				Name:        "debug",
				Aliases:     []string{"d"},
				Usage:       "enable debug logging",
				Destination: &debug,
			},
		},
		Action: func(context.Context, *cli.Command) error {
			start := time.Now()

			if debug {
				logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				}))
				slog.SetDefault(logger)
			}

			common.InitRunPrefix()

			providers.Load(true)

			slog.Info("elephant", "startup", time.Since(start))

			comm.StartListen()

			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
