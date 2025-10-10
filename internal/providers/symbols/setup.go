// Package symbols provides symbols/emojis.
package main

import (
	_ "embed"
	"fmt"
	"log"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "symbols"
	NamePretty = "Symbols/Emojis"
	h          = history.Load(Name)
)

//go:embed README.md
var readme string

type Config struct {
	common.Config    `koanf:",squash"`
	Locale           string `koanf:"locale" desc:"locale to use for symbols" default:"en"`
	History          bool   `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty bool   `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
	Command          string `koanf:"command" desc:"default command to be executed. supports %VALUE%." default:"wl-copy"`
}

var config *Config

func Setup() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "face-smile",
			MinScore: 50,
		},
		Locale:           "en",
		History:          true,
		HistoryWhenEmpty: false,
		Command:          "wl-copy",
	}

	common.LoadConfig(Name, config)

	parse()

	slog.Info(Name, "symbols/emojis", len(symbols), "time", time.Since(start))
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	fmt.Println("#### Possible locales")

	entries, err := files.ReadDir("data")
	if err != nil {
		log.Fatal(err)
	}

	for _, v := range entries {
		fmt.Printf("%s,", strings.TrimSuffix(filepath.Base(v.Name()), ".xml"))
	}

	fmt.Println()
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

const ActionRunCmd = "run_cmd"

func Activate(identifier, action string, query string, args string) {
	switch action {
	case history.ActionDelete:
		h.Remove(identifier)
		return
	case ActionRunCmd:
		cmd := common.ReplaceResultOrStdinCmd(config.Command, symbols[identifier].CP)

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "activate", err)
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

func Query(conn net.Conn, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	for k, v := range symbols {
		field := "subtext"
		var positions []int32
		var fs int32
		var score int32

		if query != "" {
			var bestScore int32
			var bestPos []int32
			var bestStart int32

			for _, m := range v.Searchable {
				score, positions, start := common.FuzzyScore(query, m, exact)

				if score > bestScore {
					bestScore = score
					bestPos = positions
					bestStart = start
				}
			}

			positions = bestPos
			fs = bestStart
			score = bestScore
		}

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
				Text:       v.Searchable[len(v.Searchable)-1],
				Icon:       v.CP,
				State:      state,
				Actions:    []string{ActionRunCmd},
				Provider:   Name,
				Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
					Start:     fs,
					Field:     field,
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
