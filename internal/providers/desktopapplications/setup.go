package main

import (
	"bytes"
	_ "embed"
	"encoding/gob"
	"fmt"
	"log"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/abenz1267/elephant/pkg/common"
	"github.com/abenz1267/elephant/pkg/common/history"
)

type DesktopFile struct {
	Data
	Actions []Data
}

var (
	Name       = "desktopapplications"
	NamePretty = "Desktop Applications"
	h          = history.Load(Name)
	pins       = loadpinned()
	config     *Config
	br         = []*regexp.Regexp{}
)

//go:embed README.md
var readme string

type Config struct {
	common.Config           `koanf:",squash"`
	LaunchPrefix            string `koanf:"launch_prefix" desc:"overrides the default app2unit or uwsm prefix, if set." default:""`
	Locale                  string `koanf:"locale" desc:"to override systems locale" default:""`
	ActionMinScore          int    `koanf:"action_min_score" desc:"min score for actions to be shown" default:"20"`
	ShowActions             bool   `koanf:"show_actions" desc:"include application actions, f.e. 'New Private Window' for Firefox" default:"false"`
	ShowGeneric             bool   `koanf:"show_generic" desc:"include generic info when show_actions is true" default:"true"`
	ShowActionsWithoutQuery bool   `koanf:"show_actions_without_query" desc:"show application actions, if the search query is empty" default:"false"`
	History                 bool   `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty        bool   `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
	OnlySearchTitle         bool   `koanf:"only_search_title" desc:"ignore keywords, comments etc from desktop file when searching" default:"false"`

	IconPlaceholder string            `koanf:"icon_placeholder" desc:"placeholder icon for apps without icon" default:"applications-other"`
	Aliases         map[string]string `koanf:"aliases" desc:"setup aliases for applications. Matched aliases will always be placed on top of the list. Example: 'ffp' => '<identifier>'. Check elephant log output when activating an item to get its identifier." default:""`
	Blacklist       []string          `koanf:"blacklist" desc:"blacklist desktop files from being parsed. Regexp." default:"<empty>"`
}

func loadpinned() []string {
	pinned := []string{}

	file := common.CacheFile(fmt.Sprintf("%s_pinned.gob", Name))

	if common.FileExists(file) {
		f, err := os.ReadFile(file)
		if err != nil {
			slog.Error("pinned", "load", err)
		} else {
			decoder := gob.NewDecoder(bytes.NewReader(f))

			err = decoder.Decode(&pinned)
			if err != nil {
				slog.Error("pinned", "decoding", err)
			}
		}
	}

	return pinned
}

func Setup() {
	start := time.Now()
	config = &Config{
		Config: common.Config{
			Icon:     "applications-other",
			MinScore: 30,
		},
		ActionMinScore:          20,
		OnlySearchTitle:         false,
		ShowActions:             false,
		ShowGeneric:             true,
		ShowActionsWithoutQuery: false,
		History:                 true,
		HistoryWhenEmpty:        false,
		IconPlaceholder:         "applications-other",
		Aliases:                 map[string]string{},
	}

	common.LoadConfig(Name, config)

	parseRegexp()
	loadFiles()

	slog.Info(Name, "desktop files", len(files), "time", time.Since(start))
}

func parseRegexp() {
	for _, v := range config.Blacklist {
		r, err := regexp.Compile(v)
		if err != nil {
			log.Panic(err)
		}

		br = append(br, r)
	}
}

func Icon() string {
	return config.Icon
}
