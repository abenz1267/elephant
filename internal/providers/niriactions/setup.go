package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "niriactions"
	NamePretty = "Niri Actions"
	config     *Config
	actions    = make(map[string]string)
	h          = history.Load(Name)
)

//go:embed README.md
var readme string

const (
	ActionExecute = "execute"
)

type Config struct {
	common.Config    `koanf:",squash"`
	ActionDelay      int  `koanf:"action_delay" desc:"delay in ms before the action is dispatched" default:"0"`
	History          bool `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty bool `koanf:"history_when_empty" desc:"consider history when query is empty" default:"true"`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "view-grid",
			MinScore: 20,
		},
		ActionDelay:      0,
		History:          true,
		HistoryWhenEmpty: true,
	}

	common.LoadConfig(Name, config)

	parseActions()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}
}

func Available() bool {
	if os.Getenv("XDG_CURRENT_DESKTOP") == "niri" {
		return true
	}

	slog.Info(Name, "available", "not a niri session. disabling")
	return false
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case history.ActionDelete:
		h.Remove(identifier)
		return
	case ActionExecute:
		time.Sleep(time.Duration(config.ActionDelay) * time.Millisecond)

		run := []string{"msg", "action", identifier}
		run = append(run, strings.Fields(args)...)

		cmd := exec.Command("niri", run...)

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
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	for k, v := range actions {
		e := &pb.QueryResponse_Item{
			Identifier: k,
			Text:       v,
			Subtext:    k,
			Icon:       config.Icon,
			Provider:   Name,
			Actions:    []string{ActionExecute},
		}

		if query != "" {
			match, score, positions, start, found := calcScore(query, k, v, exact)

			if found {
				field := "subtext"

				if match == k {
					field = "text"
				}

				e.Score = score
				e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Start:     start,
					Field:     field,
					Positions: positions,
				}

			}
		}

		if config.History && e.Score > config.MinScore || query == "" && config.HistoryWhenEmpty {
			usageScore := h.CalcUsageScore(query, e.Identifier)

			if usageScore != 0 {
				e.Score = e.Score + usageScore
				e.State = append(e.State, "history")
				e.Actions = append(e.Actions, history.ActionDelete)
			}
		}

		if query == "" || e.Score > config.MinScore {
			entries = append(entries, e)
		}
	}

	slog.Debug(Name, "query", time.Since(start))

	return entries
}

const (
	StartMarker = "Actions:"
	EndMarker   = "Options:"
)

func parseActions() {
	cmd := exec.Command("niri", "msg", "action")
	out, _ := cmd.CombinedOutput()

	start := false
	action := ""

	for l := range strings.Lines(string(out)) {
		if start && strings.Contains(l, EndMarker) {
			break
		}

		if !start && strings.Contains(l, StartMarker) {
			start = true
			continue
		}

		l := strings.TrimSpace(l)

		if l == "" {
			continue
		}

		if action == "" {
			action = l
		} else {
			actions[action] = l
			action = ""
		}
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

func calcScore(q, k, v string, exact bool) (string, int32, []int32, int32, bool) {
	var scoreRes int32
	var posRes []int32
	var startRes int32
	var match string
	var modifier int32

	toSearch := []string{k, v}

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
