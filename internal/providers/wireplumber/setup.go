// Package wireplumber allows to configure audio devices.
package main

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "wireplumber"
	NamePretty = "Wireplumber"
)

//go:embed README.md
var readme string

type Config struct {
	common.Config   `koanf:",squash"`
	VolumeStepSize  int    `koanf:"volume-step-size" desc:"volume step size (in percent, max: 100)" default:"5"`
	IconOutputMuted string `koanf:"icon_output_muted" desc:"icon for muted output device" default:"audio-volume-muted"`
	IconOutput      string `koanf:"icon_output" desc:"icon for output device" default:"audio-volume-high"`

	IconInputMuted string `koanf:"icon_input_muted" desc:"icon for muted input device" default:"audio-input-microphone-muted"`
	IconInput      string `koanf:"icon_input" desc:"icon for input device" default:"audio-input-microphone-high"`
}

var config *Config

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon:     "multimedia-volume-control",
			MinScore: 50,
		},
		VolumeStepSize:  5,
		IconOutput:      "audio-volume-high",
		IconOutputMuted: "audio-volume-muted",
		IconInputMuted:  "audio-input-microphone-muted",
		IconInput:       "audio-input-microphone-high",
	}

	common.LoadConfig(Name, config)
}

func Setup() {
	start := time.Now()

	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	if config.VolumeStepSize >= 100 {
		slog.Error(Name, "volume-step-size", config.VolumeStepSize)
	}

	slog.Info(Name, "loaded", time.Since(start))
}

func executableExists(command string) bool {
	p, err := exec.LookPath(command)

	if p == "" || err != nil {
		slog.Info(Name, "available", fmt.Sprintf("%s not found. disabling.", command))
		return false
	}

	return true
}

func Available() bool {
	return executableExists("pw-dump") && executableExists("wpctl")
}

func PrintDoc(write bool) {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name, write)
}

const (
	ActionIncreaseVolume   = "increase_volume"
	ActionDecreaseVolume   = "decrease_volume"
	ActionUnmute           = "unmute"
	ActionMute             = "mute"
	ActionSetDefaultDevice = "set_default_device"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionIncreaseVolume:
		deviceId, err := strconv.Atoi(identifier)
		if err != nil {
			slog.Error(Name, "invalid deviceId", err)
			return
		}

		if ok := setVolume(deviceId, false); !ok {
			return
		}
	case ActionDecreaseVolume:
		deviceId, err := strconv.Atoi(identifier)
		if err != nil {
			slog.Error(Name, "invalid deviceId", err)
			return
		}

		if ok := setVolume(deviceId, true); !ok {
			return
		}
	case ActionMute, ActionUnmute:
		deviceId, err := strconv.Atoi(identifier)
		if err != nil {
			slog.Error(Name, "invalid deviceId", err)
			return
		}

		if ok := toggleMute(deviceId); !ok {
			return
		}
	case ActionSetDefaultDevice:
		deviceId, err := strconv.Atoi(identifier)
		if err != nil {
			slog.Error(Name, "invalid deviceId", err)
			return
		}

		if ok := setDefaultDevice(deviceId); !ok {
			return
		}
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
	}

	devices, err := devices()
	if err != nil {
		slog.Error(Name, "activate update", err)
	}

	for _, v := range devices {
		if strconv.Itoa(v.ID) == identifier {
			handlers.UpdateItem(format, query, conn, v.toEntry())
			break
		}
	}
}

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	devices, err := devices()
	if err != nil {
		slog.Error(Name, "query", err)
		return entries
	}

	// sort to move the currently selected in/outputs to the top
	// only relevant if the search query is empty
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Selected && !devices[j].Selected {
			return true
		}

		return devices[i].PipewireType == PipewireTypeSink
	})

	for _, dev := range devices {
		entry := dev.toEntry()

		if query != "" {
			match, score, positions, start, found := calcScore(query, entry.Text, entry.Subtext, exact)

			if found {
				field := "subtext"

				if match == entry.Text {
					field = "text"
				}

				entry.Score = score
				entry.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Start:     start,
					Field:     field,
					Positions: positions,
				}
			}
		}

		if entry.Score > config.MinScore || query == "" {
			entries = append(entries, entry)
		}
	}

	slog.Debug(Name, "query", time.Since(start))

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

func calcScore(q, v, vv string, exact bool) (string, int32, []int32, int32, bool) {
	var scoreRes int32
	var posRes []int32
	var startRes int32
	var match string
	var modifier int32

	toSearch := []string{v, vv}

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
