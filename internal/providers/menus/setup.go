package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	lua "github.com/yuin/gopher-lua"
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
		var menu *common.Menu

		identifier = strings.TrimPrefix(identifier, "menus:")

		openmenu := false

		terminal := false

		for _, v := range common.Menus {
			if identifier == v.Name {
				menu = v
				openmenu = true
				break
			}

			process := v.Entries

			for _, entry := range process {
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

		run := ""

		if after, ok := strings.CutPrefix(identifier, "dmenu:"); ok {
			run = after

			if strings.Contains(run, "~") {
				home, _ := os.UserHomeDir()
				run = strings.ReplaceAll(run, "~", home)
			}
		}

		if len(e.Actions) != 0 {
			if val, ok := e.Actions[action]; ok {
				run = val
			}
		}

		if run == "" {
			if len(menu.Actions) != 0 {
				if val, ok := menu.Actions[action]; ok {
					run = val
				}
			}
		}

		if run == "" {
			run = menu.Action
		}

		if run == "" {
			return
		}

		if after, ok := strings.CutPrefix(run, "lua:"); ok {
			state := common.NewLuaState(menu.Name, menu.LuaString)

			if menu != nil && state != nil {
				functionName := after

				if err := state.CallByParam(lua.P{
					Fn:      state.GetGlobal(functionName),
					NRet:    0,
					Protect: true,
				}, lua.LString(e.Value), lua.LString(args)); err != nil {
					slog.Error(Name, "lua function call", err, "function", functionName)
				}

				if menu.History {
					h.Save(query, identifier)
				}
			} else {
				menuName := "unknown"
				if menu != nil {
					menuName = menu.Name
				}
				slog.Error(Name, "no lua state available for menu", menuName)
			}
			return
		}

		pipe := false

		if strings.Contains(run, "%CLIPBOARD%") {
			clipboard := common.ClipboardText()

			if clipboard == "" {
				slog.Error(Name, "activate", "empty clipboard")
				return
			}

			run = strings.ReplaceAll(run, "%CLIPBOARD%", clipboard)
		} else {
			if !strings.Contains(run, "%VALUE%") {
				pipe = true
			} else {
				run = strings.ReplaceAll(run, "%VALUE%", e.Value)
			}
		}

		run = strings.ReplaceAll(run, "%ARGS%", args)

		if terminal {
			run = common.WrapWithTerminal(run)
		}

		cmd := exec.Command("sh", "-c", run)

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}

		if pipe && e.Value != "" {
			cmd.Stdin = strings.NewReader(e.Value)
		}

		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "activate", err, "msg", out)
		} else {
			go func() {
				cmd.Wait()
			}()
		}

		if menu != nil && menu.History {
			h.Save(query, identifier)
		}
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, format uint8) []*pb.QueryResponse_Item {
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

		if v.IsLua && (len(v.Entries) == 0 || !v.Cache) {
			v.CreateLuaEntries()
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
				} else {
					sub = fmt.Sprintf("%s: %s", v.NamePretty, sub)
				}
			}

			var actions []string

			if v.Parent != "" && single {
				actions = append(actions, ActionGoParent)
			}

			for k := range me.Actions {
				actions = append(actions, k)
			}

			for k := range v.Actions {
				if !slices.Contains(actions, k) {
					actions = append(actions, k)
				}
			}

			if strings.HasPrefix(me.Identifier, "menus:") {
				actions = append(actions, ActionOpen)
			}

			if len(actions) == 0 || (len(actions) == 1 && actions[0] == ActionGoParent) {
				actions = append(actions, ActionDefault)
			}

			e := &pb.QueryResponse_Item{
				Identifier:  me.Identifier,
				Text:        me.Text,
				Subtext:     sub,
				Provider:    fmt.Sprintf("%s:%s", Name, me.Menu),
				Icon:        icon,
				State:       me.State,
				Actions:     actions,
				Type:        pb.QueryResponse_REGULAR,
				Preview:     me.Preview,
				PreviewType: me.PreviewType,
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

					handlers.UpdateItem(format, query, conn, e)
				}()
			}

			if query != "" {
				e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Field: "text",
				}

				if v.SearchName {
					me.Keywords = append(me.Keywords, me.Menu)
				}

				_, e.Score, e.Fuzzyinfo.Positions, e.Fuzzyinfo.Start, _ = calcScore(query, me, exact)
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

func calcScore(q string, d common.Entry, exact bool) (string, int32, []int32, int32, bool) {
	var scoreRes int32
	var posRes []int32
	var startRes int32
	var match string
	var modifier int32

	toSearch := []string{d.Text, d.Subtext}
	toSearch = append(toSearch, d.Keywords...)

	for k, v := range toSearch {
		score, pos, start := common.FuzzyScore(q, v, exact)

		if score > scoreRes {
			scoreRes = score
			posRes = pos
			startRes = start
			match = v
			modifier = int32(k)
		}
	}

	if scoreRes == 0 {
		return "", 0, nil, 0, false
	}

	scoreRes = max(scoreRes-min(modifier*5, 50)-startRes, 10)

	return match, scoreRes, posRes, startRes, true
}
