package main

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"al.essio.dev/pkg/shellescape"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "snippets"
	NamePretty = "Snippets"
	config     *Config
)

//go:embed README.md
var readme string

const (
	ActionPaste = "paste"
)

type Config struct {
	common.Config    `koanf:",squash"`
	Command          string    `koanf:"command" desc:"default command to be executed. supports %VALUE%." default:"wtype %CONTENT%"`
	Snippets         []Snippet `koanf:"snippets" desc:"available snippets" default:""`
	Preview          bool      `koanf:"preview" desc:"set preview content" default:"true"`
	PreviewAsSubtext bool      `koanf:"preview_as_subtext" desc:"display content as subtext" default:"true"`
	Delay            int       `koanf:"delay" desc:"delay in ms before executing command to avoid potential focus issues" default:"100"`
}

type Snippet struct {
	Keywords []string `koanf:"keywords" desc:"searchable keywords" default:""`
	Name     string   `koanf:"name" desc:"displayed name" default:""`
	Content  string   `koanf:"content" desc:"content to paste" default:""`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "insert-text",
			MinScore: 50,
		},
		Command:          "wtype %CONTENT%",
		Delay:            100,
		Preview:          true,
		PreviewAsSubtext: true,
	}

	common.LoadConfig(Name, config)

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}
}

func Available() bool {
	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	time.Sleep(time.Duration(config.Delay) * time.Millisecond)

	i, _ := strconv.Atoi(identifier)
	s := config.Snippets[i]

	toRun := strings.ReplaceAll(config.Command, "%CONTENT%", shellescape.Quote(s.Content))
	cmd := exec.Command("sh", "-c", toRun)

	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate", err)
	} else {
		go func() {
			cmd.Wait()
		}()
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	for k, v := range config.Snippets {
		e := &pb.QueryResponse_Item{
			Identifier: fmt.Sprintf("%d", k),
			Text:       v.Name,
			Actions:    []string{ActionPaste},
			Icon:       Icon(),
			Provider:   Name,
			Score:      int32(100000 - k),
			Type:       0,
		}

		if config.Preview {
			if !config.PreviewAsSubtext {
				e.PreviewType = util.PreviewTypeText
				e.Preview = v.Content
			} else {
				e.Subtext = v.Content
			}
		}

		if query != "" {
			e.Score = 0

			score, positions, start, found := calcScore(query, v, exact)

			if found {
				e.Score = score
				e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Start:     start,
					Field:     "text",
					Positions: positions,
				}
			}
		}

		if query == "" || e.Score > config.MinScore {
			entries = append(entries, e)
		}
	}

	slog.Debug(Name, "query", time.Since(start))

	return entries
}

func calcScore(q string, d Snippet, exact bool) (int32, []int32, int32, bool) {
	var scoreRes int32
	var posRes []int32
	var startRes int32

	toSearch := []string{d.Name}
	toSearch = append(toSearch, d.Keywords...)

	for _, v := range toSearch {
		score, pos, start := common.FuzzyScore(q, v, exact)

		if score > scoreRes {
			scoreRes = score
			posRes = pos
			startRes = start
		}
	}

	if scoreRes == 0 {
		return 0, nil, 0, false
	}

	scoreRes = max(scoreRes-startRes, 10)

	return scoreRes, posRes, startRes, true
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
