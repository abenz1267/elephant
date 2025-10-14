// Package common provides common functions used by all providers.
package common

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Icon                 string `koanf:"icon" desc:"icon for provider" default:"depends on provider"`
	MinScore             int32  `koanf:"min_score" desc:"minimum score for items to be displayed" default:"depends on provider"`
	HideFromProviderlist bool   `koanf:"hide_from_providerlist" desc:"hides a provider from the providerlist provider. provider provider." default:"false"`
}

type ElephantConfig struct {
	AutoDetectLaunchPrefix bool `koanf:"auto_detect_launch_prefix" desc:"automatically detects uwsm, app2unit or systemd-run" default:"true"`
	OverloadLocalEnv       bool `koanf:"overload_local_env" desc:"overloads the local env" default:"false"`
}

var elephantConfig *ElephantConfig

func LoadGlobalConfig() {
	elephantConfig = &ElephantConfig{
		AutoDetectLaunchPrefix: true,
		OverloadLocalEnv:       false,
	}

	LoadConfig("elephant", elephantConfig)

	for _, v := range ConfigDirs() {
		envFile := filepath.Join(v, ".env")

		if FileExists(envFile) {
			var err error

			if elephantConfig.OverloadLocalEnv {
				err = godotenv.Overload(envFile)
			} else {
				err = godotenv.Load(envFile)
			}

			if err != nil {
				slog.Error("elephant", "localenv", err)
				return
			}

			slog.Info("elephant", "localenv", "loaded")
		}
	}
}

func GetElephantConfig() *ElephantConfig {
	return elephantConfig
}

func LoadConfig(provider string, config any) {
	defaults := koanf.New(".")

	err := defaults.Load(structs.Provider(config, "koanf"), nil)
	if err != nil {
		slog.Error(provider, "config", err)
		os.Exit(1)
	}

	userConfig, err := ProviderConfig(provider)
	if err != nil {
		slog.Info(provider, "config", "using default config")
		return
	}

	user := koanf.New("")

	err = user.Load(file.Provider(userConfig), toml.Parser())
	if err != nil {
		slog.Error(provider, "config", err)
		os.Exit(1)
	}

	err = defaults.Merge(user)
	if err != nil {
		slog.Error(provider, "config", err)
		os.Exit(1)
	}

	err = defaults.Unmarshal("", &config)
	if err != nil {
		slog.Error(provider, "config", err)
		os.Exit(1)
	}
}
