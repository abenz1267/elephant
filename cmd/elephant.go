package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/abenz1267/elephant/internal/comm"
	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/joho/godotenv"
	"github.com/urfave/cli/v3"
)

func main() {
	var config string
	var socketrequest string
	var debug bool

	cmd := &cli.Command{
		Name:                   "Elephant",
		Usage:                  "Data provider and executor",
		UseShortOptionHandling: true,
		Commands: []*cli.Command{
			{
				Name:    "generatedoc",
				Aliases: []string{"d"},
				Usage:   "generates a markdown documentation",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					providers.Load()
					util.GenerateDoc()
					return nil
				},
			},
			{
				Name:    "send",
				Aliases: []string{"s"},
				Usage:   "sends a request to the elephant service",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:        "request",
						Destination: &socketrequest,
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					comm.Send(socketrequest)
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
			providers.Setup()

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
