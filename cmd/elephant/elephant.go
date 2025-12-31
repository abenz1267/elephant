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
	"github.com/abenz1267/elephant/v2/internal/install"
	"github.com/abenz1267/elephant/v2/internal/providers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/adrg/xdg"
	"github.com/urfave/cli/v3"
)

//go:embed version.txt
var version string

func main() {
	cmd := &cli.Command{
		Name:                   "Elephant",
		Usage:                  "Data provider and executor",
		UseShortOptionHandling: true,
		CommandNotFound: func(ctx context.Context, cmd *cli.Command, s string) {
			fmt.Println(s)
		},
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

					common.LoadGlobalConfig()

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
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name: "provider",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					common.LoadGlobalConfig()

					logger := slog.New(slog.DiscardHandler)
					slog.SetDefault(logger)

					providers.Load(false)

					util.GenerateDoc(cmd.StringArg("provider"))
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
						Usage:       "Don't close after querying, in case of async querying.",
					},
					&cli.BoolFlag{
						Name:        "json",
						Category:    "",
						DefaultText: "output as json",
						Usage:       "if you want json. use this.",
					},
				},
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name: "content",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client.Query(cmd.StringArg("content"), cmd.Bool("async"), cmd.Bool("json"))

					return nil
				},
			},
			{
				Name: "state",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name: "content",
					},
				},
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:        "json",
						Category:    "",
						DefaultText: "output as json",
						Usage:       "if you want json. use this.",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client.ProviderState(cmd.StringArg("content"), cmd.Bool("json"))

					return nil
				},
			},
			{
				Name:  "community",
				Usage: "elephant-community based actions",
				Commands: []*cli.Command{
					{
						Name:        "install",
						Description: "installs the given menus, if no menu is given , it will list availables instead",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							install.Install(cmd.Args().Slice())

							return nil
						},
					},
					{
						Name:        "readme",
						Description: "displays the readme of the given menu",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							install.Readme(cmd.Args().First())

							return nil
						},
					},
					{
						Name:        "remove",
						Description: "if not provided with any menu names, it will list all installed menus",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							install.Remove(cmd.Args().Slice())

							return nil
						},
					},
					{
						Name:        "list",
						Description: "lists all available community menus",
						Action: func(ctx context.Context, cmd *cli.Command) error {
							install.List()

							return nil
						},
					},
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
				Name:    "config",
				Aliases: []string{"c"},
				Value:   "",
				Usage:   "config folder location",
				Action: func(ctx context.Context, cmd *cli.Command, val string) error {
					common.SetExplicitDir(val)
					return nil
				},
			},
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "enable debug logging",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() > 0 {
				fmt.Printf("'%s' is not a valid command.\n\n", cmd.Args().First())
				cli.ShowAppHelp(cmd)

				return nil
			}

			start := time.Now()

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

			if cmd.Bool("debug") {
				logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				}))
				slog.SetDefault(logger)
			}

			common.InitRunPrefix()

			runBeforeCommands()

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

func runBeforeCommands() {
	cfg := common.GetElephantConfig()

	if len(cfg.BeforeLoad) == 0 {
		return
	}

	for _, v := range cfg.BeforeLoad {
		for {
			cmd := exec.Command("sh", "-c", v.Command)

			out, err := cmd.CombinedOutput()
			if err == nil || !v.MustSucceed {
				break
			}

			slog.Error("elephant", "before_load", string(out))
		}
	}
}
