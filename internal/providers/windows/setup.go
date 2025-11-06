// Package windows provides window focusing.
package main

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/wlr"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"github.com/adrg/xdg"
	"github.com/charlievieth/fastwalk"
	"github.com/neurlang/wayland/wl"
)

var (
	Name       = "windows"
	NamePretty = "Windows"
)

var (
	icons = make(map[string]string)
	mu    sync.RWMutex
)

//go:embed README.md
var readme string

type Config struct {
	common.Config `koanf:",squash"`
	Delay         int `koanf:"delay" desc:"delay in ms before focusing to avoid potential focus issues" default:"100"`
}

var config *Config

func Setup() {
	start := time.Now()

	if !wlr.IsSetup {
		go wlr.Init()
	}

	config = &Config{
		Config: common.Config{
			Icon:     "view-restore",
			MinScore: 20,
		},
		Delay: 100,
	}

	common.LoadConfig(Name, config)

	findIcons()

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

const (
	ActionFocus = "focus"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	time.Sleep(time.Duration(config.Delay) * time.Millisecond)

	i, _ := strconv.Atoi(identifier)

	wlr.Activate(wl.ProxyId(i))
}

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	windows := wlr.Windows()

	for k, window := range windows {
		e := &pb.QueryResponse_Item{
			Identifier: fmt.Sprintf("%d", k),
			Text:       window.Title,
			Subtext:    window.AppID,
			Actions:    []string{ActionFocus},
			Provider:   Name,
			Icon:       config.Icon,
		}

		mu.RLock()
		if val, ok := icons[window.AppID]; ok {
			e.Icon = val
		}
		mu.RUnlock()

		if query != "" {
			matched, score, pos, start, ok := calcScore(query, window, exact)

			if ok {
				field := "text"
				e.Score = score

				if matched != window.Title {
					field = "subtext"
				}

				e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Start:     start,
					Field:     field,
					Positions: pos,
				}
			}
		}

		if query == "" || e.Score > config.MinScore {
			entries = append(entries, e)
		}
	}

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))

	return entries
}

func Icon() string {
	return config.Icon
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}

func calcScore(q string, d *wlr.Window, exact bool) (string, int32, []int32, int32, bool) {
	var scoreRes int32
	var posRes []int32
	var startRes int32
	var match string

	toSearch := []string{d.Title, d.AppID}

	for _, v := range toSearch {
		score, pos, start := common.FuzzyScore(q, v, exact)

		if score > scoreRes {
			scoreRes = score
			posRes = pos
			startRes = start
			match = v
		}
	}

	if scoreRes == 0 {
		return "", 0, nil, 0, false
	}

	scoreRes = max(scoreRes-startRes, 10)

	return match, scoreRes, posRes, startRes, true
}

func findIcons() {
	conf := fastwalk.Config{
		Follow: true,
	}

	for _, root := range xdg.ApplicationDirs {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		if err := fastwalk.Walk(&conf, root, walkFunction); err != nil {
			slog.Error(Name, "walk", err)
			continue
		}
	}
}

func walkFunction(path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}

	if filepath.Ext(path) != ".desktop" {
		return nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	class := ""
	icon := ""
	exec := ""

	for l := range strings.Lines(string(b)) {
		if after, ok := strings.CutPrefix(l, "StartupWMClass="); ok {
			class = strings.TrimSpace(after)
		}

		if after, ok := strings.CutPrefix(l, "Icon="); ok {
			icon = strings.TrimSpace(after)
		}

		if after, ok := strings.CutPrefix(l, "Exec="); ok {
			exec = strings.TrimSpace(after)
		}
	}

	if class != "" && icon != "" {
		mu.Lock()
		icons[class] = icon
		icons[icon] = icon
		mu.Unlock()
	}

	if exec != "" && icon != "" {
		mu.Lock()
		icons[strings.Fields(exec)[0]] = icon
		mu.Unlock()
	}

	return err
}
