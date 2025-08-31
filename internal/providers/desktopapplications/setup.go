package main

import (
	"log/slog"
	"time"

	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/common/history"
)

type DesktopFile struct {
	Data
	Actions []Data
}

var (
	Name       = "desktopapplications"
	NamePretty = "Desktop Applications"
	h          = history.Load(Name)
	config     *Config
)

type Config struct {
	common.Config           `koanf:",squash"`
	LaunchPrefix            string `koanf:"launch_prefix" desc:"overrides the default app2unit or uwsm prefix, if set. 'CLEAR' to not prefix." default:""`
	Locale                  string `koanf:"locale" desc:"to override systems locale" default:""`
	ShowActions             bool   `koanf:"show_actions" desc:"include application actions, f.e. 'New Private Window' for Firefox" default:"false"`
	ShowGeneric             bool   `koanf:"show_generic" desc:"include generic info when show_actions is true" default:"false"`
	ShowActionsWithoutQuery bool   `koanf:"show_actions_without_query" desc:"show application actions, if the search query is empty" default:"false"`
	History                 bool   `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty        bool   `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`

	IconPlaceholder string            `koanf:"icon_placeholder" desc:"placeholder icon for apps without icon" default:"applications-other"`
	Aliases         map[string]string `koanf:"aliases" desc:"setup aliases for applications. Matched aliases will always be placed on top of the list. Example: 'ffp' => '<identifier>'. Check elephant log output when activating an item to get its identifier." default:""`
	Pinned          []string          `koanf:"pinned" desc:"Pinned items will appear in order on top of list. Value = <identifier>. Check elephant log output when activating an item to get its identifier." default:"<empty>"`
}

func init() {
	start := time.Now()
	config = &Config{
		Config: common.Config{
			Icon:     "applications-other",
			MinScore: 30,
		},
		ShowActions:             false,
		ShowGeneric:             false,
		ShowActionsWithoutQuery: false,
		History:                 true,
		HistoryWhenEmpty:        false,
		IconPlaceholder:         "applications-other",
		Aliases:                 map[string]string{},
	}

	common.LoadConfig(Name, config)

	loadFiles()

	slog.Info(Name, "desktop files", len(files), "time", time.Since(start))
}

func Icon() string {
	return config.Icon
}
