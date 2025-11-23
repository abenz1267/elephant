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
	LaunchPrefix                   string            `koanf:"launch_prefix" desc:"overrides the default app2unit or uwsm prefix, if set." default:""`
	Locale                         string            `koanf:"locale" desc:"to override systems locale" default:""`
	ActionMinScore                 int               `koanf:"action_min_score" desc:"min score for actions to be shown" default:"20"`
	ShowActions                    bool              `koanf:"show_actions" desc:"include application actions, f.e. 'New Private Window' for Firefox" default:"false"`
	ShowGeneric                    bool              `koanf:"show_generic" desc:"include generic info when show_actions is true" default:"true"`
	ShowActionsWithoutQuery        bool              `koanf:"show_actions_without_query" desc:"show application actions, if the search query is empty" default:"false"`
	History                        bool              `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty               bool              `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
	OnlySearchTitle                bool              `koanf:"only_search_title" desc:"ignore keywords, comments etc from desktop file when searching" default:"false"`
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
	config = &Config{
		Config: common.Config{
			Icon:     "applications-other",
			MinScore: 30,
		},
		ScoreOpenWindows:        true,
		ActionMinScore:          20,
		IgnorePinWithWindow:     true,
		OnlySearchTitle:         false,
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

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	parseRegexp()
	loadFiles()

	if config.WindowIntegration {
		if !wlr.IsSetup {
			go wlr.Init()
		}
	}

	if config.WMIntegration {
		switch os.Getenv("XDG_CURRENT_DESKTOP") {
		case "niri":
			wmi = Niri{}
		case "Hyprland":
			wmi = Hyprland{}
		}
	}

	slog.Info(Name, "desktop files", len(files), "time", time.Since(start))
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

func Icon() string {
	return config.Icon
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}
