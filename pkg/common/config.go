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
	NamePretty           string `koanf:"name_pretty" desc:"displayed name for the provider" default:"depends on provider"`
	MinScore             int32  `koanf:"min_score" desc:"minimum score for items to be displayed" default:"depends on provider"`
	HideFromProviderlist bool   `koanf:"hide_from_providerlist" desc:"hides a provider from the providerlist provider. provider provider." default:"false"`
}

type Command struct {
	MustSucceed bool   `koanf:"must_succeed" desc:"will try running this command until it completes successfully" default:"false"`
	Command     string `koanf:"command" desc:"command to execute" default:""`
}

type ElephantConfig struct {
	ProviderHosts          map[string][]string `koanf:"provider_hosts" desc:"providers will only be loaded on the specified hosts. If empty, all." default:""`
	AutoDetectLaunchPrefix bool                `koanf:"auto_detect_launch_prefix" desc:"automatically detects uwsm, app2unit or systemd-run" default:"true"`
	LaunchPrefix           string              `koanf:"launch_prefix" desc:"overrides the default app2unit or uwsm prefix, if set." default:""`
	OverloadLocalEnv       bool                `koanf:"overload_local_env" desc:"overloads the local env" default:"false"`
	IgnoredProviders       []string            `koanf:"ignored_providers" desc:"providers to ignore" default:"<empty>"`
	GitOnDemand            bool                `koanf:"git_on_demand" desc:"sets up git repositories on first query instead of on start" default:"true"`
	BeforeLoad             []Command           `koanf:"before_load" desc:"commands to run before starting to load the providers" default:""`
}

var elephantConfig *ElephantConfig

func LoadGlobalConfig() {
	elephantConfig = &ElephantConfig{
		AutoDetectLaunchPrefix: true,
		OverloadLocalEnv:       false,
		GitOnDemand:            true,
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
