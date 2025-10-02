// Package runner provides access to binaries in $PATH.
package main

import (
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/common"
	"github.com/abenz1267/elephant/pkg/common/history"
	"github.com/abenz1267/elephant/pkg/pb/pb"
	"github.com/charlievieth/fastwalk"
)

var (
	Name       = "runner"
	NamePretty = "Runner"
	results    = providers.QueryData{}
)

//go:embed README.md
var readme string

type ExplicitItem struct {
	Exec  string `koanf:"exec" desc:"executable/command to run" default:""`
	Alias string `koanf:"alias" desc:"alias" default:""`
}

type Config struct {
	common.Config    `koanf:",squash"`
	History          bool           `koanf:"history" desc:"make use of history for sorting" default:"true"`
	HistoryWhenEmpty bool           `koanf:"history_when_empty" desc:"consider history when query is empty" default:"false"`
	Explicits        []ExplicitItem `koanf:"explicits" desc:"use this explicit list, instead of searching $PATH" default:""`
}

var (
	config *Config
	items  = []Item{}
	h      = history.Load(Name)
)

type Item struct {
	Identifier string
	Bin        string
	Alias      string
}

func Setup() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "utilities-terminal",
			MinScore: 50,
		},
		History:          true,
		HistoryWhenEmpty: false,
	}

	common.LoadConfig(Name, config)

	if len(config.Explicits) == 0 {
		bins := []string{}

		conf := fastwalk.Config{
			Follow: true,
		}

		for p := range strings.SplitSeq(os.Getenv("PATH"), ":") {
			walkFn := func(path string, d fs.DirEntry, err error) error {
				info, serr := os.Stat(path)
				if info == nil || serr != nil {
					return nil
				}

				if info.Mode()&0o111 != 0 && !d.IsDir() {
					bins = append(bins, filepath.Base(path))
				}

				return nil
			}

			if err := fastwalk.Walk(&conf, p, walkFn); err != nil {
				slog.Error("runner", "load", err)
			}
		}

		slices.Sort(bins)
		bins = slices.Compact(bins)

		for _, v := range bins {
			md5 := md5.Sum([]byte(v))
			md5str := hex.EncodeToString(md5[:])

			items = append(items, Item{
				Identifier: md5str,
				Bin:        v,
			})
		}
	} else {
		for _, v := range config.Explicits {
			md5 := md5.Sum([]byte(v.Exec))
			identifier := hex.EncodeToString(md5[:])

			items = append(items, Item{
				Identifier: identifier,
				Bin:        v.Exec,
				Alias:      v.Alias,
			})
		}
	}

	slog.Info(Name, "executables", len(items), "time", time.Since(start))
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

const (
	ActionRun           = "run"
	ActionRunInTerminal = "runterminal"
)

func Activate(qid uint32, identifier, action string, arguments string) {
	if action == history.ActionDelete {
		h.Remove(identifier)
		return
	}

	bin := ""

	splits := strings.Split(arguments, common.GetElephantConfig().ArgumentDelimiter)
	if len(splits) > 1 {
		arguments = splits[1]
	} else {
		arguments = ""
	}

	for _, v := range items {
		if v.Identifier == identifier {
			bin = v.Bin
			break
		}
	}

	run := strings.TrimSpace(fmt.Sprintf("%s %s", bin, arguments))
	if action == ActionRunInTerminal {
		run = common.WrapWithTerminal(run)
	}

	cmd := exec.Command("sh", "-c", run)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

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
	entries := []*pb.QueryResponse_Item{}

	if query != "" {
		results.GetData(query, qid, iid, exact)
	}

	for _, v := range items {
		e := &pb.QueryResponse_Item{
			Identifier: v.Identifier,
			Text:       v.Bin,
			Provider:   Name,
			Icon:       config.Icon,
			Score:      0,
			Fuzzyinfo:  &pb.QueryResponse_Item_FuzzyInfo{},
			Type:       pb.QueryResponse_REGULAR,
		}

		if query != "" {
			var score int32
			var positions []int32
			var start int32

			score, positions, start = common.FuzzyScore(query, v.Bin, exact)
			s2, p2, ss2 := common.FuzzyScore(query, v.Alias, exact)

			if s2 > score {
				e.Text = v.Alias
				score = s2
				positions = p2
				start = ss2
			}

			e.Score = score
			e.Fuzzyinfo.Positions = positions
			e.Fuzzyinfo.Start = start
		}

		var usageScore int32
		if config.History {
			if e.Score > config.MinScore || query == "" && config.HistoryWhenEmpty {
				usageScore = h.CalcUsageScore(query, e.Identifier)
				e.Score = e.Score + usageScore
			}
		}

		if e.Score > config.MinScore || query == "" {
			entries = append(entries, e)
		}
	}

	return entries
}

func Icon() string {
	return config.Icon
}
