package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/abenz1267/elephant/internal/comm/handlers"
	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/common"
	"github.com/abenz1267/elephant/pkg/common/history"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var (
	Name       = "websearch"
	NamePretty = "Websearch"
	config     *Config
	prefixes   = make(map[string]int)
	results    = providers.QueryData{}
	h          = history.Load(Name)
)

//go:embed README.md
var readme string

type Config struct {
	common.Config           `koanf:",squash"`
	Entries                 []Entry `koanf:"entries" desc:"entries" default:""`
	MaxGlobalItemsToDisplay int     `koanf:"max_global_items_to_display" desc:"will only show the global websearch entry if there are at most X results." default:"1"`
	History                 bool    `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty        bool    `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
}

type Entry struct {
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
		MaxGlobalItemsToDisplay: 1,
		History:                 true,
		HistoryWhenEmpty:        false,
	}

	common.LoadConfig(Name, config)
	handlers.MaxGlobalItemsToDisplayWebsearch = config.MaxGlobalItemsToDisplay

	for k, v := range config.Entries {
		if v.Prefix != "" {
			prefixes[v.Prefix] = k
			handlers.WebsearchPrefixes[v.Prefix] = v.Name
		}
	}
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Cleanup(qid uint32) {
	slog.Info(Name, "cleanup", qid)
	results.Lock()
	delete(results.Queries, qid)
	results.Unlock()
}

func Activate(qid uint32, identifier, action string, arguments string) {
	if action == history.ActionDelete {
		h.Remove(identifier)
		return
	}

	i, _ := strconv.Atoi(identifier)

	for k := range prefixes {
		if after, ok := strings.CutPrefix(arguments, k); ok {
			arguments = after
			break
		}
	}

	splits := strings.Split(arguments, common.GetElephantConfig().ArgumentDelimiter)
	if len(splits) > 1 {
		arguments = splits[1]
	}

	url := strings.ReplaceAll(config.Entries[i].URL, "%TERM%", url.QueryEscape(strings.TrimSpace(arguments)))

	prefix := common.LaunchPrefix("")

	cmd := exec.Command("sh", "-c", strings.TrimSpace(fmt.Sprintf("%s xdg-open '%s'", prefix, url)))

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
		var last uint32

		for k := range results.Queries[qid] {
			if k > last {
				last = k
			}
		}

		if last != 0 {
			h.Save(results.Queries[qid][last], identifier)
		} else {
			h.Save("", identifier)
		}
	}
}

func Query(qid uint32, iid uint32, query string, single bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	if query != "" {
		results.GetData(query, qid, iid, exact)
	}

	prefix := ""

	for k := range prefixes {
		if strings.HasPrefix(query, k) {
			prefix = k
			break
		}
	}

	if single {
		for k, v := range config.Entries {
			icon := v.Icon
			if icon == "" {
				icon = config.Icon
			}

			e := &pb.QueryResponse_Item{
				Identifier: strconv.Itoa(k),
				Text:       v.Name,
				Subtext:    "",
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
					}

					e.Score = e.Score + usageScore
				}
			}

			if e.Score > config.MinScore || query == "" {
				entries = append(entries, e)
			}
		}
	} else {
		for k, v := range config.Entries {
			if v.Default || v.Prefix == prefix {
				icon := v.Icon
				if icon == "" {
					icon = config.Icon
				}

				e := &pb.QueryResponse_Item{
					Identifier: strconv.Itoa(k),
					Text:       v.Name,
					Subtext:    "",
					Icon:       icon,
					Provider:   Name,
					Score:      int32(100 - k),
					Type:       0,
				}

				entries = append(entries, e)
			}
		}
	}

	return entries
}

func Icon() string {
	return config.Icon
}
