package main

import (
	"github.com/abenz1267/elephant/internal/common"
)

type Config struct {
	common.Config           `koanf:",squash"`
	Locale                  string `koanf:"locale" desc:"to overwrite systems locale" default:""`
	Runner                  string `koanf:"runner" desc:"program to use during process spawning" default:"app2unit"`
	ShowActions             bool   `koanf:"show_actions" desc:"include application actions, f.e. 'New Private Window' for Firefox" default:"false"`
	ShowGeneric             bool   `koanf:"show_generic" desc:"include generic info when show_actions is true" default:"false"`
	PrioritizeNew           bool   `koanf:"prioritize_new" desc:"prioritize freshly installed applications" default:"false"`
	ShowActionsWithoutQuery bool   `koanf:"show_actions_without_query" desc:"show application actions, if the search query is empty" default:"false"`
}

var config *Config

func loadConfig() {
	config = &Config{
		Config:                  common.Config{},
		Runner:                  "app2unit",
		ShowActions:             false,
		ShowGeneric:             false,
		PrioritizeNew:           false,
		ShowActionsWithoutQuery: false,
	}

	common.LoadConfig(Name, config)
}
