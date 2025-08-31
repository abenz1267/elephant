// Package symbols provides symbols/emojis.
package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/common/history"
	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var (
	Name       = "unicode"
	NamePretty = "Unicode"
	h          = history.Load(Name)
	results    = providers.QueryData{}
)

//go:embed data/UnicodeData.txt
var data string

type Config struct {
	common.Config    `koanf:",squash"`
	Locale           string `koanf:"locale" desc:"locale to use for symbols" default:"en"`
	History          bool   `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty bool   `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
}

var (
	config  *Config
	symbols = make(map[string]string)
)

func init() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "accessories-character-map-symbolic",
			MinScore: 50,
		},
		Locale:           "en",
		History:          true,
		HistoryWhenEmpty: false,
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

func PrintDoc() {
	fmt.Printf("### %s\n", NamePretty)
	fmt.Println("Find unicode symbols.")
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Cleanup(qid uint32) {
}

func Activate(qid uint32, identifier, action string, arguments string) {
	cmd := exec.Command("wl-copy")

	symbol := fmt.Sprintf("'\\u%s'", symbols[identifier])

	toUse, err := strconv.Unquote(symbol)
	if err != nil {
		slog.Error(Name, "activate", err)
		return

	}

	cmd.Stdin = strings.NewReader(toUse)

	err = cmd.Start()
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

func Query(qid uint32, iid uint32, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	if query != "" {
		results.GetData(query, qid, iid, exact)
	}

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
			entries = append(entries, &pb.QueryResponse_Item{
				Identifier: k,
				Score:      score,
				Text:       k,
				Icon:       v,
				Provider:   Name,
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
