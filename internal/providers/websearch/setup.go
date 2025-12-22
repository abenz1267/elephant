package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"al.essio.dev/pkg/shellescape"
	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "websearch"
	NamePretty = "Websearch"
	config     *Config
	prefixes   = make(map[string]int)
	h          = history.Load(Name)
)

//go:embed README.md
var readme string

type Config struct {
	common.Config     `koanf:",squash"`
	Engines           []Engine `koanf:"entries" desc:"entries" default:"google"`
	History           bool     `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty  bool     `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
	EnginesAsActions  bool     `koanf:"engines_as_actions" desc:"run engines as actions" default:"true"`
	AlwaysShowDefault bool     `koanf:"always_show_default" desc:"always show the default search engine when queried" default:"true"`
	TextPrefix        string   `koanf:"text_prefix" desc:"prefix for the entry text" default:"Search: "`
	Command           string   `koanf:"command" desc:"default command to be executed. supports %VALUE%." default:"xdg-open"`
}

type Engine struct {
	Name    string `koanf:"name" desc:"name of the entry" default:""`
	Default bool   `koanf:"default" desc:"entry to display when querying multiple providers" default:""`
	Prefix  string `koanf:"prefix" desc:"prefix to actively trigger this entry" default:""`
	URL     string `koanf:"url" desc:"url, example: 'https://www.google.com/search?q=%TERM%'" default:""`
	Icon    string `koanf:"icon" desc:"icon to display, fallsback to global" default:""`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "applications-internet",
			MinScore: 20,
		},
		History:           true,
		HistoryWhenEmpty:  false,
		EnginesAsActions:  false,
		TextPrefix:        "Search: ",
		Command:           "xdg-open",
		AlwaysShowDefault: true,
	}

	common.LoadConfig(Name, config)

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	if len(config.Engines) == 0 {
		config.Engines = append(config.Engines, Engine{
			Name:    "Google",
			Default: true,
			URL:     "https://www.google.com/search?q=%TERM%",
		})
	}

	if len(config.Engines) == 1 {
		config.Engines[0].Default = true
	}

	handlers.WebsearchAlwaysShow = config.AlwaysShowDefault

	for k, v := range config.Engines {
		if v.Default {
			handlers.MaxGlobalItemsToDisplayWebsearch++
		}

		if v.Prefix != "" {
			prefixes[v.Prefix] = k
			handlers.WebsearchPrefixes[v.Prefix] = v.Name
		}
	}

	slices.SortFunc(config.Engines, func(a, b Engine) int {
		if a.Default {
			return -1
		}

		if b.Default {
			return 1
		}

		return 0
	})
}

func Available() bool {
	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

const (
	ActionSearch  = "search"
	ActionOpenURL = "open_url"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionOpenURL:
		cmd := exec.Command("sh", "-c", strings.TrimSpace(fmt.Sprintf("%s xdg-open %s", common.LaunchPrefix(""), shellescape.Quote(fmt.Sprintf("https://%s", query)))))

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "activate", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	case history.ActionDelete:
		h.Remove(identifier)
		return
	case ActionSearch:
		i, _ := strconv.Atoi(identifier)

		for k := range prefixes {
			if after, ok := strings.CutPrefix(query, k); ok {
				query = after
				break
			}
		}

		if args == "" {
			args = query
		}

		q := ""

		if strings.Contains(config.Engines[i].URL, "%CLIPBOARD%") {
			clipboard := common.ClipboardText()

			if clipboard == "" {
				slog.Error(Name, "activate", "empty clipbpoard")
				return
			}

			q = strings.ReplaceAll(os.ExpandEnv(config.Engines[i].URL), "%CLIPBOARD%", url.QueryEscape(clipboard))
		} else {
			q = strings.ReplaceAll(os.ExpandEnv(config.Engines[i].URL), "%TERM%", url.QueryEscape(strings.TrimSpace(args)))
		}

		run(query, identifier, q)
	default:
		q := ""

		if !config.EnginesAsActions {
			slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
			return
		}

		for _, v := range config.Engines {
			if v.Name == action {
				q = v.URL
				break
			}
		}

		if strings.Contains(q, "%CLIPBOARD%") {
			clipboard := common.ClipboardText()

			if clipboard == "" {
				slog.Error(Name, "activate", "empty clipbpoard")
				return
			}

			q = strings.ReplaceAll(q, "%CLIPBOARD%", url.QueryEscape(clipboard))
		} else {
			q = strings.ReplaceAll(q, "%TERM%", url.QueryEscape(strings.TrimSpace(query)))
		}

		run(query, identifier, q)
	}
}

func run(query, identifier, q string) {
	cmd := exec.Command("sh", "-c", strings.TrimSpace(fmt.Sprintf("%s %s %s", common.LaunchPrefix(""), config.Command, shellescape.Quote(q))))

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate", err)
	} else {
		go func() {
			cmd.Wait()
		}()
	}

	if config.History {
		h.Save(query, identifier)
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	prefix := ""

	for k := range prefixes {
		if strings.HasPrefix(query, k) {
			prefix = k
			break
		}
	}

	isURL := false

	if strings.Contains(query, ".") && !strings.HasSuffix(query, ".") {
		_, err := url.ParseRequestURI(fmt.Sprintf("https://%s", query))
		if err == nil {
			e := &pb.QueryResponse_Item{
				Identifier: "websearch",
				Text:       fmt.Sprintf("Open: %s", query),
				Actions:    []string{ActionOpenURL},
				Icon:       Icon(),
				Provider:   Name,
				Score:      1000000,
			}

			entries = append(entries, e)
			isURL = true
		}
	}

	if !isURL {
		if config.EnginesAsActions {
			a := []string{}

			for _, v := range config.Engines {
				a = append(a, v.Name)
			}

			e := &pb.QueryResponse_Item{
				Identifier: "websearch",
				Text:       fmt.Sprintf("%s%s", config.TextPrefix, query),
				Actions:    a,
				Icon:       Icon(),
				Provider:   Name,
				Score:      1,
				Type:       0,
			}

			entries = append(entries, e)
		} else {
			if single {
				for k, v := range config.Engines {
					icon := v.Icon
					if icon == "" {
						icon = config.Icon
					}

					e := &pb.QueryResponse_Item{
						Identifier: strconv.Itoa(k),
						Text:       v.Name,
						Subtext:    "",
						Actions:    []string{"search"},
						Icon:       icon,
						Provider:   Name,
						Score:      int32(100 - k),
						Type:       0,
					}

					if query != "" {
						score, pos, start := common.FuzzyScore(query, v.Name, exact)

						e.Score = score
						e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
							Field:     "text",
							Positions: pos,
							Start:     start,
						}
					}

					var usageScore int32
					if config.History {
						if e.Score > config.MinScore || query == "" && config.HistoryWhenEmpty {
							usageScore = h.CalcUsageScore(query, e.Identifier)

							if usageScore != 0 {
								e.State = append(e.State, "history")
								e.Actions = append(e.Actions, history.ActionDelete)
							}

							e.Score = e.Score + usageScore
						}
					}

					if e.Score > config.MinScore || query == "" {
						entries = append(entries, e)
					}
				}
			}

			if len(entries) == 0 || !single {
				for k, v := range config.Engines {
					if (prefix == "" && v.Default) || (v.Prefix == prefix) {
						icon := v.Icon
						if icon == "" {
							icon = config.Icon
						}

						e := &pb.QueryResponse_Item{
							Identifier: strconv.Itoa(k),
							Text:       v.Name,
							Subtext:    "",
							Actions:    []string{"search"},
							Icon:       icon,
							Provider:   Name,
							Score:      int32(15 - k),
							Type:       0,
						}

						entries = append(entries, e)
					}
				}
			}
		}
	}

	return entries
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
