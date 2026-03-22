package main

import (
	_ "embed"
	"strconv"

	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"sync"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "playerctl"
	NamePretty = "Playerctl"
	config  *Config
)

//go:embed README.md
var readme string

const (
	ActionPlayPause     = "toggle_pause"
	ActionNext          = "next"
	ActionPrev          = "prev"
	ActionVolUp         = "vol_up"
	ActionVolDown       = "vol_down"
	ActionToggleMute    = "toggle_mute"
	ActionSeekForward   = "seek_forward"
	ActionSeekBack      = "seek_back"
	ActionToggleShuffle = "toggle_shuffle"
	ActionToggleLoop    = "toggle_loop"
)

var actions = []string{ActionPlayPause, ActionNext, ActionPrev, ActionVolUp, ActionVolDown, ActionSeekForward, ActionSeekBack}

type Config struct {
	common.Config          `koanf:",squash"`
	VolStep        float64 `koanf:"vol_step"  desc:"volume step size" default:"0.05"`
	SeekStep       int     `koanf:"seek_step" desc:"seek step in seconds" default:"5"`
}

func Setup() {
	LoadConfig()
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{},
		VolStep:  0.05,
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

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	volStep   := fmt.Sprintf("%g+", config.VolStep)
	volStepD  := fmt.Sprintf("%g-", config.VolStep)
	seekStep  := fmt.Sprintf("%d+", config.SeekStep)
	seekStepB := fmt.Sprintf("%d-", config.SeekStep)

	switch action {
	case ActionPlayPause:
		exec.Command("playerctl", "-p", identifier, "play-pause").Run()
	case ActionNext:
		exec.Command("playerctl", "-p", identifier, "next").Run()
	case ActionPrev:
		exec.Command("playerctl", "-p", identifier, "previous").Run()
	case ActionVolUp:
		exec.Command("playerctl", "-p", identifier, "volume", volStep).Run()
	case ActionVolDown:
		exec.Command("playerctl", "-p", identifier, "volume", volStepD).Run()
	case ActionToggleMute:
		get_vol, err := exec.Command("playerctl", "-p", identifier, "volume").Output()
		if err != nil {
			slog.Error(Name, "activate", "Failed to get volume", "action", action)
		}

		vol, _ := strconv.ParseFloat(string(get_vol), 64)
		
		if vol == 0.0 {
			vol = 1.0
		} else {
			vol = 0.0
		}

		exec.Command("playerctl", "-p", identifier, "volume", fmt.Sprintf("%g", vol))
	case ActionSeekForward:
		exec.Command("playerctl", "-p", identifier, "position", seekStep).Run()
	case ActionSeekBack:
		exec.Command("playerctl", "-p", identifier, "position", seekStepB).Run()
	case ActionToggleShuffle:
		exec.Command("playerctl", "-p", identifier, "shuffle", "Toggle").Run()
	case ActionToggleLoop:
		loop, err := exec.Command("playerctl", "-p", identifier, "loop").Output()
		if err != nil {
			slog.Error(Name, "activate", "Failed to get loop state", "action", action)
		}

		loop_str := string(loop)
		switch loop_str {
		case "none":
			loop_str = "Track"
		case "Track":
			loop_str = "Playlist"
		case "Playlist":
			loop_str = "none"
		}

		exec.Command("playerctl", "-p", identifier, "loop", loop_str).Run()
	default:
		slog.Warn(Name, "activate", "unknown action", "action", action)
	}
}

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	players, err := exec.Command("playerctl", "-l").Output()
	if err != nil {
		slog.Error(Name, "query", "failed to list players", "err", err)
		return nil
	}

	playerList := strings.Fields(string(players))
	if len(playerList) == 0 {
		return nil
	}

	type result struct {
		title  string
		artist string
		status string
		player string
	}

	resultsCh := make(chan result, len(playerList))

	var wg sync.WaitGroup
	for _, player := range playerList {
		wg.Add(1)
		go func(playerName string) {
			defer wg.Done()

			meta, err := exec.Command("playerctl", "-p", playerName, "metadata", "--format",
				"{{xesam:title}}\n{{xesam:artist}}\n{{status}}").Output()
			if err != nil {
				slog.Warn(Name, "player", playerName, "err", err)
				return
			}

			lines := strings.SplitN(strings.TrimSpace(string(meta)), "\n", 3)
			if len(lines) < 3 {
				return
			}

			resultsCh <- result{
				player: playerName,
				title:  lines[0],
				artist: lines[1],
				status: lines[2],
			}
		}(player)
	}

	// Close channel once all goroutines finish
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	entries := []*pb.QueryResponse_Item{}
	for r := range resultsCh {
		if strings.EqualFold(r.status, "") {
			continue
		}
		
		icon := "media-playback-start"
		if strings.EqualFold(r.status, "Playing") {
		    icon = "media-playback-pause"
		}

		entries = append(entries, &pb.QueryResponse_Item{
			Identifier: r.player,
			Text:       r.title,
			Type:       pb.QueryResponse_REGULAR,
			Subtext:    fmt.Sprintf("%s · %s", r.artist, r.status),
			Icon: icon,
			Actions: actions,
			Provider: Name,
		})
	}

	return entries
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
