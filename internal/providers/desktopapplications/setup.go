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
	"sync"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/common/wlr"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
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
	pinsMu     sync.RWMutex
	config     *Config
	br         = []*regexp.Regexp{}
	wmi        WMIntegration
)

type WMIntegration interface {
	GetWorkspace() string
	GetCurrentWindows() []string
	MoveToWorkspace(workspace, initialWMClass string)
}

//go:embed README.md
var readme string

type Config struct {
	common.Config                  `koanf:",squash"`
	Locale                         string            `koanf:"locale" desc:"to override systems locale" default:""`
	ActionMinScore                 int               `koanf:"action_min_score" desc:"min score for actions to be shown" default:"20"`
	ShowActions                    bool              `koanf:"show_actions" desc:"include application actions, f.e. 'New Private Window' for Firefox" default:"false"`
	ShowGeneric                    bool              `koanf:"show_generic" desc:"include generic info when show_actions is true" default:"true"`
	ShowActionsWithoutQuery        bool              `koanf:"show_actions_without_query" desc:"show application actions, if the search query is empty" default:"false"`
	History                        bool              `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty               bool              `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
	OnlySearchTitle                bool              `koanf:"only_search_title" desc:"ignore keywords, comments etc from desktop file when searching. same as search_fields = [\"name\"]" default:"false"`
	SearchFields                   []string          `koanf:"search_fields" desc:"desktop file fields to search, in order of preference: a match on a later field scores lower. one or more of name, exec, parent (an action's application name), generic_name, keywords, comment" default:"[\"name\", \"exec\", \"parent\", \"generic_name\", \"keywords\", \"comment\"]"`
	SplitKeywords                  bool              `koanf:"split_keywords" desc:"match each keyword on its own, instead of all keywords joined into one string" default:"false"`
	Subtext                        string            `koanf:"subtext" desc:"'match' shows the field the query matched, if it isn't the name, otherwise the generic name. 'generic_name' always shows the generic name (an action's application name), and nothing if it repeats the name" default:"match"`
	IconPlaceholder                string            `koanf:"icon_placeholder" desc:"placeholder icon for apps without icon" default:"applications-other"`
	Aliases                        map[string]string `koanf:"aliases" desc:"setup aliases for applications. Matched aliases will always be placed on top of the list. Example: 'ffp' => '<identifier>'. Check elephant log output when activating an item to get its identifier." default:""`
	Blacklist                      []string          `koanf:"blacklist" desc:"blacklist desktop files from being parsed. Regexp." default:"<empty>"`
	WindowIntegration              bool              `koanf:"window_integration" desc:"will enable window integration, meaning focusing an open app instead of opening a new instance" default:"false"`
	IgnorePinWithWindow            bool              `koanf:"ignore_pin_with_window" desc:"will ignore pinned apps that have an opened window" default:"true"`
	WindowIntegrationIgnoreActions bool              `koanf:"window_integration_ignore_actions" desc:"will ignore the window integration for actions" default:"true"`
	WMIntegration                  bool              `koanf:"wm_integration" desc:"Moves apps to the workspace where they were launched at automatically. Currently Niri only." default:"false"`
	ScoreOpenWindows               bool              `koanf:"score_open_windows" desc:"Apps that have open windows, get their score halved. Requires window_integration." default:"true"`
	SingleInstanceApps             []string          `koanf:"single_instance_apps" desc:"application IDs that don't ever spawn a new window. " default:"[\"discord\"]"`
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
	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	parseRegexp()
	parseSearchConfig()
	loadFiles()

	if config.WindowIntegration {
		if !wlr.IsSetup {
			go wlr.Init()
		}
	}

	if config.WMIntegration {
		for _, d := range desktops {
			switch d {
			case "niri":
				wmi = Niri{}
			case "Hyprland":
				wmi = Hyprland{}
			}
		}
	}

	slog.Info(Name, "desktop files", len(files), "time", time.Since(start))
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon:     "applications-other",
			MinScore: 30,
		},
		ScoreOpenWindows:        true,
		ActionMinScore:          20,
		IgnorePinWithWindow:     true,
		OnlySearchTitle:         false,
		SearchFields:            []string{"name", "exec", "parent", "generic_name", "keywords", "comment"},
		SplitKeywords:           false,
		Subtext:                 "match",
		ShowActions:             false,
		ShowGeneric:             true,
		ShowActionsWithoutQuery: false,
		History:                 true,
		WMIntegration:           false,
		HistoryWhenEmpty:        false,
		IconPlaceholder:         "applications-other",
		Aliases:                 map[string]string{},
		WindowIntegration:       false,
		SingleInstanceApps:      []string{"discord"},
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	return true
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

func parseSearchConfig() {
	if config.Subtext != "match" && config.Subtext != "generic_name" {
		slog.Error(Name, "unknown subtext", config.Subtext)
	}

	searchFields = []searchField{}

	if config.OnlySearchTitle {
		searchFields = append(searchFields, fieldName)
		return
	}

	for _, v := range config.SearchFields {
		f, ok := searchFieldNames[v]
		if !ok {
			slog.Error(Name, "unknown search field", v)
			continue
		}

		searchFields = append(searchFields, f)
	}

	if len(searchFields) == 0 {
		slog.Error(Name, "search_fields", "no valid fields, searching names only")
		searchFields = append(searchFields, fieldName)
	}
}

func Icon() string {
	return config.Icon
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}
