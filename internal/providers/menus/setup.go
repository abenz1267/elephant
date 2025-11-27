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

func Available() bool {
	return true
}

const (
	ActionGoParent = "menus:parent"
	ActionOpen     = "menus:open"
	ActionDefault  = "menus:default"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
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

		defer func() {
			if e.Menu != "" && e.Value != "" {
				common.LastMenuValueMut.Lock()
				common.LastMenuValue[e.Menu] = e.Value
				common.LastMenuValueMut.Unlock()
			}
		}()

		submenu := ""
		m := ""

		if strings.HasPrefix(identifier, "menus:") {
			splits := strings.Split(identifier, ":")
			submenu = splits[1]
			m = splits[2]
		} else {
			m = strings.Split(identifier, ":")[0]
		}

		terminal := false

		if v, ok := common.Menus[m]; ok {
			for _, entry := range v.Entries {
				if identifier == entry.Identifier {
					menu = v
					e = entry

					terminal = v.Terminal || entry.Terminal

					break
				}
			}
		}

		if submenu != "" {
			handlers.ProviderUpdated <- fmt.Sprintf("%s:%s", Name, submenu)
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
			if menu == nil {
				return
			}

			state := menu.NewLuaState()

			if state != nil {
				functionName := after

				if err := state.CallByParam(lua.P{
					Fn:      state.GetGlobal(functionName),
					NRet:    0,
					Protect: true,
				}, lua.LString(e.Value), lua.LString(args), lua.LString(query)); err != nil {
					slog.Error(Name, "lua function call", err, "function", functionName)
				}

				if menu.History {
					h.Save(query, identifier)
				}
			} else {
				slog.Error(Name, "no lua state available for menu", menu.Name)
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

		if slices.Contains(menu.AsyncActions, action) {
			updated := itemToEntry(format, query, conn, menu.Actions, menu.NamePretty, single, menu.Icon, &e)
			handlers.UpdateItem(format, query, conn, updated)

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
			v.CreateLuaEntries(query)
		}

		for k, me := range v.Entries {
			e := itemToEntry(format, query, conn, v.Actions, v.NamePretty, single, v.Icon, &v.Entries[k])

			if v.FixedOrder {
				e.Score = 1_000_000 - int32(k)
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

	slog.Debug(Name, "query", time.Since(start))

	return entries
}

func Icon() string {
	return ""
}

func HideFromProviderlist() bool {
	return common.MenuConfigLoaded.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	menu := strings.Split(provider, ":")[1]

	if val, ok := common.Menus[menu]; ok {
		if val.Parent != "" {
			return &pb.ProviderStateResponse{
				Actions: []string{ActionGoParent},
			}
		}
	}

	return &pb.ProviderStateResponse{}
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

func itemToEntry(format uint8, query string, conn net.Conn, menuActions map[string]string, namePretty string, single bool, icon string, me *common.Entry) *pb.QueryResponse_Item {
	if me.Icon != "" {
		icon = me.Icon
	}

	sub := me.Subtext

	if !single {
		if sub == "" {
			sub = namePretty
		} else {
			sub = fmt.Sprintf("%s: %s", namePretty, sub)
		}
	}

	var actions []string

	for k := range me.Actions {
		actions = append(actions, k)
	}

	for k := range menuActions {
		if !slices.Contains(actions, k) {
			actions = append(actions, k)
		}
	}

	if strings.HasPrefix(me.Identifier, "menus:") {
		actions = append(actions, ActionOpen)
	}

	if len(actions) == 0 {
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

	if me.Async != "" {
		me.Value = ""

		go func() {
			cmd := exec.Command("sh", "-c", me.Async)
			out, err := cmd.CombinedOutput()

			if err == nil {
				e.Text = strings.TrimSpace(string(out))
				me.Value = e.Text
			} else {
				e.Text = "%DELETE%"
			}

			handlers.UpdateItem(format, query, conn, e)
		}()
	}

	return e
}
