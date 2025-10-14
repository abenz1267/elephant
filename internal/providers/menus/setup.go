package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "menus"
	NamePretty = "Menus"
	h          = history.Load(Name)
)

//go:embed README.md
var readme string

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(common.MenuConfig{}, Name)
	util.PrintConfig(common.Menu{}, Name)
}

func Setup() {}

const (
	ActionGoParent = "menus:parent"
	ActionOpen     = "menus:open"
	ActionDefault  = "menus:default"
)

func Activate(identifier, action string, query string, args string) {
	switch action {
	case ActionGoParent:
		identifier = strings.TrimPrefix(identifier, "menus:")

		for _, v := range common.Menus {
			if identifier == v.Name {
				handlers.ProviderUpdated <- fmt.Sprintf("%s:%s", Name, v.Parent)
				break
			}
		}
	case history.ActionDelete:
		h.Remove(identifier)
		return
	default:
		var e common.Entry
		var menu common.Menu

		identifier = strings.TrimPrefix(identifier, "menus:")

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

		if len(e.Actions) != 0 {
			run = e.Actions[action]
		}

		if run == "" {
			return
		}

		pipe := false

		val := e.Value
		if args != "" {
			val = args
		}

		if !strings.Contains(run, "%VALUE%") {
			pipe = true
		} else {
			run = strings.ReplaceAll(run, "%VALUE%", val)
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

		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "activate", err, "msg", out)
		} else {
			go func() {
				cmd.Wait()
			}()
		}

		if menu.History {
			h.Save(query, identifier)
		}
	}
}

func Query(conn net.Conn, query string, single bool, exact bool) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}
	menu := ""

	initialQuery := query

	split := strings.Split(query, ":")

	if len(split) > 1 {
		menu = split[0]
		query = split[1]
	}

	for _, v := range common.Menus {
		if menu != "" && v.Name != menu {
			continue
		}

		for k, me := range v.Entries {
			icon := v.Icon

			if me.Icon != "" {
				icon = me.Icon
			}

			sub := me.Subtext

			if !single {
				if sub == "" {
					sub = v.NamePretty
				}
			}

			var actions []string

			for k := range me.Actions {
				actions = append(actions, k)
			}

			if strings.HasPrefix(me.Identifier, "menus:") {
				actions = append(actions, ActionOpen)
			}

			if len(actions) == 0 {
				actions = append(actions, ActionDefault)
			}

			e := &pb.QueryResponse_Item{
				Identifier: me.Identifier,
				Text:       me.Text,
				Subtext:    sub,
				Provider:   fmt.Sprintf("%s:%s", Name, me.Menu),
				Icon:       icon,
				Actions:    actions,
				Type:       pb.QueryResponse_REGULAR,
				Preview:    me.Preview,
			}

			if v.FixedOrder {
				e.Score = 1_000_000 - int32(k)
			}

			if me.Async != "" {
				v.Entries[k].Value = ""

				go func() {
					cmd := exec.Command("sh", "-c", me.Async)
					out, err := cmd.CombinedOutput()

					if err == nil {
						e.Text = strings.TrimSpace(string(out))
						v.Entries[k].Value = e.Text
					} else {
						e.Text = "%DELETE%"
					}

					handlers.UpdateItem(query, conn, e)
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

			var usageScore int32
			if v.History {
				if e.Score > v.MinScore || query == "" && v.HistoryWhenEmpty {
					usageScore = h.CalcUsageScore(initialQuery, e.Identifier)

					if usageScore != 0 {
						e.State = append(e.State, "history")
						e.Actions = append(e.Actions, history.ActionDelete)
					}

					e.Score = e.Score + usageScore
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
