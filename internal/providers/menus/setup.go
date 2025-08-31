package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/abenz1267/elephant/internal/comm/handlers"
	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var (
	Name       = "menus"
	NamePretty = "Menus"
)

//go:embed other.toml
var other string

//go:embed screenshots.toml
var screenshots string

func PrintDoc() {
	fmt.Printf("### %s\n", NamePretty)
	fmt.Println("Custom menus.")
	fmt.Println("Default location for menu definitions is `~/.config/elephant/menus/`.")
	util.PrintConfig(common.MenuConfig{}, Name)
	fmt.Println("#### Example Menus")
	fmt.Println()
	fmt.Printf("```toml\n%s\n```", other)
	fmt.Println()
	fmt.Printf("```toml\n%s\n```", screenshots)
	fmt.Println()
}

func Cleanup(qid uint32) {
}

func Activate(qid uint32, identifier, action string, arguments string) {
	var e common.Entry
	var menu common.Menu

	identifier = strings.TrimPrefix(identifier, "keepopen:")
	identifier = strings.TrimPrefix(identifier, "menus:")

	splits := strings.Split(arguments, common.GetElephantConfig().ArgumentDelimiter)
	if len(splits) > 1 {
		arguments = splits[1]
	}

	openmenu := false

	terminal := false

	for _, v := range common.Menus {
		if identifier == v.Name {
			menu = v
			openmenu = true
			break
		}

		for _, entry := range v.Entries {
			if identifier == entry.Identifier {
				menu = v
				e = entry

				terminal = v.Terminal || entry.Terminal

				break
			}
		}
	}

	if openmenu {
		handlers.ProviderUpdated <- fmt.Sprintf("%s:%s", Name, menu.Name)
		return
	}

	run := menu.Action

	if after, ok := strings.CutPrefix(identifier, "dmenu:"); ok {
		run = after

		if strings.Contains(run, "~") {
			home, _ := os.UserHomeDir()
			run = strings.ReplaceAll(run, "~", home)
		}
	}

	if e.Action != "" {
		run = e.Action
	}

	if run == "" {
		return
	}

	pipe := false

	val := e.Value
	if val == "" && len(splits) > 1 {
		val = arguments
	}

	if !strings.Contains(run, "%RESULT%") {
		pipe = true
	} else {
		run = strings.ReplaceAll(run, "%RESULT%", val)
	}

	if terminal {
		run = common.WrapWithTerminal(run)
	}

	cmd := exec.Command("sh", "-c", run)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if pipe && e.Value != "" {
		cmd.Stdin = strings.NewReader(val)
	}

	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate", err)
	} else {
		go func() {
			cmd.Wait()
		}()
	}
}

func Query(qid uint32, iid uint32, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}
	menu := ""

	split := strings.Split(query, ":")
	single := len(split) > 1

	if len(split) > 1 {
		menu = split[0]
		query = split[1]
	}

	for _, v := range common.Menus {
		if menu != "" && v.Name != menu || (!single && !v.GlobalSearch) {
			continue
		}

		icon := v.Icon

		for _, me := range v.Entries {
			if me.Icon != "" {
				icon = me.Icon
			}

			sub := me.Subtext

			if !single && v.GlobalSearch {
				if sub == "" {
					sub = v.NamePretty
				}
			}

			e := &pb.QueryResponse_Item{
				Identifier: me.Identifier,
				Text:       me.Text,
				Subtext:    sub,
				Provider:   fmt.Sprintf("%s:%s", Name, me.Menu),
				Icon:       icon,
				Type:       pb.QueryResponse_REGULAR,
				Preview:    me.Preview,
			}

			if me.Async != "" {
				go func() {
					cmd := exec.Command("sh", "-c", me.Async)
					out, err := cmd.CombinedOutput()

					if err == nil {
						e.Text = strings.TrimSpace(string(out))
					} else {
						e.Text = "%DELETE%"
					}

					providers.AsyncChannels[qid][iid] <- e
				}()
			}

			if query != "" {
				e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Field: "text",
				}

				e.Score, e.Fuzzyinfo.Positions, e.Fuzzyinfo.Start = common.FuzzyScore(query, e.Text, exact)

				for _, v := range me.Keywords {
					score, positions, start := common.FuzzyScore(query, v, exact)

					if score > e.Score {
						e.Score = score
						e.Fuzzyinfo.Positions = positions
						e.Fuzzyinfo.Start = start
					}
				}
			}

			if e.Score > common.MenuConfigLoaded.MinScore || query == "" {
				entries = append(entries, e)
			}
		}
	}

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))

	return entries
}

func Icon() string {
	return ""
}
