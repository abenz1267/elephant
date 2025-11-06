// Package symbols provides symbols/emojis.
package main

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "unicode"
	NamePretty = "Unicode"
	h          = history.Load(Name)
)

//go:embed README.md
var readme string

//go:embed data/UnicodeData.txt
var data string

type Config struct {
	common.Config    `koanf:",squash"`
	Locale           string `koanf:"locale" desc:"locale to use for symbols" default:"en"`
	History          bool   `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty bool   `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
	Command          string `koanf:"command" desc:"default command to be executed. supports %VALUE%." default:"wl-copy"`
}

var (
	config  *Config
	symbols = make(map[string]string)
)

func Setup() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "accessories-character-map-symbolic",
			MinScore: 50,
		},
		Locale:           "en",
		History:          true,
		HistoryWhenEmpty: false,
		Command:          "wl-copy",
	}

	common.LoadConfig(Name, config)

	for v := range strings.Lines(data) {
		if v == "" {
			continue
		}

		fields := strings.SplitN(v, ";", 3)
		symbols[fields[1]] = fields[0]
	}

	slog.Info(Name, "loaded", time.Since(start))
}

func Available() bool {
	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

const ActionRunCmd = "run_cmd"

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case history.ActionDelete:
		h.Remove(identifier)
		return
	case ActionRunCmd:
		codePoint, err := strconv.ParseInt(symbols[identifier], 16, 32)
		if err != nil {
			slog.Error(Name, "activate parse unicode", err)
			return
		}
		toUse := string(rune(codePoint))

		cmd := common.ReplaceResultOrStdinCmd(config.Command, toUse)

		err = cmd.Start()
		if err != nil {
			slog.Error(Name, "activate run cmd", err)
			return
		} else {
			go func() {
				cmd.Wait()
			}()
		}

		if config.History {
			h.Save(query, identifier)
		}
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	for k, v := range symbols {
		score, positions, start := common.FuzzyScore(query, k, exact)

		var usageScore int32
		if config.History {
			if score > config.MinScore || query == "" && config.HistoryWhenEmpty {
				usageScore = h.CalcUsageScore(query, k)
				score = score + usageScore
			}
		}

		if usageScore != 0 || score > config.MinScore || query == "" {
			state := []string{}

			if usageScore != 0 {
				state = append(state, "history")
			}

			entries = append(entries, &pb.QueryResponse_Item{
				Identifier: k,
				Score:      score,
				State:      state,
				Text:       k,
				Icon:       v,
				Provider:   Name,
				Actions:    []string{ActionRunCmd},
				Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
					Start:     start,
					Field:     "text",
					Positions: positions,
				},
				Type: pb.QueryResponse_REGULAR,
			})
		}
	}

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))
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
