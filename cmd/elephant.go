package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/abenz1267/elephant/internal/comm"
	"github.com/abenz1267/elephant/internal/comm/client"
	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/joho/godotenv"
	"github.com/urfave/cli/v3"
)

//go:embed version.txt
var version string

func main() {
	var config string
	var debug bool

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
				Aliases: []string{"d"},
				Usage:   "lists all installed providers",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					logger := slog.New(slog.DiscardHandler)
					slog.SetDefault(logger)

					providers.Load()

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

					providers.Load()

					providers.Load()
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

			loadLocalEnv()

			common.InitRunPrefix()

			providers.Load()

			slog.Info("elephant", "startup", time.Since(start))

			comm.StartListen()

			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func loadLocalEnv() {
	envFile := filepath.Join(common.ConfigDir(), ".env")

	if common.FileExists(envFile) {
		err := godotenv.Load(envFile)
		if err != nil {
			slog.Error("elephant", "localenv", err)
			return
		}

		slog.Info("elephant", "localenv", "loaded")
	}
}
