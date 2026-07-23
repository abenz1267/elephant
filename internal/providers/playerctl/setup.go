package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "playerctl"
	NamePretty = "Playerctl"
	config     *Config
)

//go:embed README.md
var readme string

const (
	ActionPlay        = "play"
	ActionPause       = "pause"
	ActionNext        = "next"
	ActionPrev        = "prev"
	ActionVolUp       = "vol_up"
	ActionVolDown     = "vol_down"
	ActionMute        = "mute"
	ActionUnmute      = "unmute"
	ActionSeekForward = "seek_forward"
	ActionSeekBack    = "seek_back"
)

type Config struct {
	common.Config `koanf:",squash"`
	VolStep       float64 `koanf:"vol_step"  desc:"volume step size" default:"0.1"`
	SeekStep      int     `koanf:"seek_step" desc:"seek step in seconds" default:"5"`
}

func Setup() {
	LoadConfig()
}

func LoadConfig() {
	config = &Config{
		Config:   common.Config{},
		VolStep:  0.1,
		SeekStep: 5,
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	p, err := exec.LookPath("playerctl")

	if p == "" || err != nil {
		slog.Info(Name, "available", "playerctl not found. disabling.")
		return false
	}

	return true
}

func PrintDoc(write bool) {
	if !write {
		fmt.Println(readme)
		fmt.Println()
	}
	util.PrintConfig(config, Name, write)
}

var currentVolMap = make(map[string]string)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	volStep := fmt.Sprintf("%g+", config.VolStep)
	volStepD := fmt.Sprintf("%g-", config.VolStep)
	seekStep := fmt.Sprintf("%d+", config.SeekStep)
	seekStepB := fmt.Sprintf("%d-", config.SeekStep)

	var cmd *exec.Cmd

	switch action {
	case ActionPlay:
		cmd = exec.Command("playerctl", "-p", identifier, "play")
	case ActionPause:
		cmd = exec.Command("playerctl", "-p", identifier, "pause")
	case ActionNext:
		cmd = exec.Command("playerctl", "-p", identifier, "next")
	case ActionPrev:
		cmd = exec.Command("playerctl", "-p", identifier, "previous")
	case ActionVolUp:
		cmd = exec.Command("playerctl", "-p", identifier, "volume", volStep)
	case ActionVolDown:
		cmd = exec.Command("playerctl", "-p", identifier, "volume", volStepD)
	case ActionMute:
		volume, err := exec.Command("playerctl", "-p", identifier, "volume").Output()
		if err != nil {
			slog.Error(Name, "activate", "Failed to get volume", "action", action)
			return
		}

		currentVolMap[identifier] = string(volume)

		cmd = exec.Command("playerctl", "-p", identifier, "volume", "0.0")
	case ActionUnmute:
		target := "1.0"
		if val, ok := currentVolMap[identifier]; ok {
			target = val
		}

		cmd = exec.Command("playerctl", "-p", identifier, "volume", target)
	case ActionSeekForward:
		cmd = exec.Command("playerctl", "-p", identifier, "position", seekStep)
	case ActionSeekBack:
		cmd = exec.Command("playerctl", "-p", identifier, "position", seekStepB)
	default:
		slog.Warn(Name, "activate", "unknown action", "action", action)
		return
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "activate", action, string(out), err)
		return
	}

	time.Sleep(500 * time.Millisecond)
	handlers.UpdateItem(format, query, conn, getEntryForPlayer(identifier))
}

func Query(conn net.Conn, query string, runes []rune, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	slab := common.AcquireFuzzySlab()
	defer common.ReleaseFuzzySlab(slab)

	entries := []*pb.QueryResponse_Item{}

	players, err := exec.Command("playerctl", "-l").Output()
	if err != nil {
		slog.Error(Name, "query", "failed to list players", "err", err)
		return nil
	}

	playerList := strings.Fields(string(players))

	if len(playerList) == 0 {
		return entries
	}

	for _, player := range playerList {
		entry := getEntryForPlayer(player)
		if entry == nil {
			continue
		}

		if query != "" {
			match, score, positions, start, found := calcScore(runes, slab, entry.Text, entry.Subtext, exact)

			if found {
				field := "subtext"

				if match == entry.Text {
					field = "text"
				}

				entry.Score = score
				entry.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Start:     start,
					Field:     field,
					Positions: common.FuzzyPositionsToInt32(positions),
				}
			}
		}

		if entry.Score > config.MinScore || query == "" {
			entries = append(entries, entry)
		}
	}

	return entries
}

func getEntryForPlayer(player string) *pb.QueryResponse_Item {
	meta, err := exec.Command("playerctl", "-p", player, "metadata", "--format",
		"{{xesam:title}}\n{{xesam:artist}}\n{{status}}\n{{volume*100}}").Output()
	if err != nil {
		slog.Error(Name, "player", player, "err", err)
		return nil
	}

	lines := strings.SplitN(strings.TrimSpace(string(meta)), "\n", 4)

	if len(lines) < 4 || strings.EqualFold(lines[2], "") {
		return nil
	}

	actions := []string{ActionPrev, ActionNext, ActionVolUp, ActionVolDown, ActionSeekForward, ActionSeekBack}

	icon := "media-playback-start"

	if strings.EqualFold(lines[2], "Playing") {
		actions = append(actions, ActionPause)
		icon = "media-playback-pause"
	} else {
		actions = append(actions, ActionPlay)
	}

	if strings.EqualFold(lines[3], "0.0") {
		actions = append(actions, ActionUnmute)
	} else {
		actions = append(actions, ActionMute)
	}

	entry := &pb.QueryResponse_Item{
		Identifier: player,
		Text:       lines[0],
		Type:       pb.QueryResponse_REGULAR,
		Subtext:    fmt.Sprintf("%s · %s · %s", player, lines[1], lines[3]),
		Icon:       icon,
		Actions:    actions,
		Provider:   Name,
	}

	return entry
}

func Icon() string {
	return "media-playback-start"
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}

func calcScore(runes []rune, slab *common.FuzzySlab, text, subtext string, exact bool) (string, int32, *[]int, int32, bool) {
	var scoreRes int32
	var posRes *[]int
	var startRes int32
	var match string
	var modifier int32

	toSearch := []string{text, subtext}

	for k, v := range toSearch {
		score, pos, start := common.FuzzyScore(runes, v, exact, slab)

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
