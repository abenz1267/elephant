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
	common.Config  `koanf:",squash"`
	VolumeStepSize int `koanf:"volume-step-size" desc:"volume step size (in percent, max: 100)" default:"5"`
}

var (
	config *Config
)

func Setup() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "multimedia-volume-control-symbolic",
			MinScore: 50,
		},
		VolumeStepSize: 5,
	}

	common.LoadConfig(Name, config)

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

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

const ActionIncreaseVolume = "increase_volume"
const ActionDecreaseVolume = "decrease_volume"
const ActionToggleMute = "toggle_mute"
const ActionSetDefaultDevice = "set_default_device"

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionIncreaseVolume:
		deviceId, err := strconv.Atoi(identifier)
		if err != nil {
			slog.Error(Name, "invalid deviceId", err)
			return
		}

		SetVolume(deviceId, config.VolumeStepSize)

		return
	case ActionDecreaseVolume:
		deviceId, err := strconv.Atoi(identifier)
		if err != nil {
			slog.Error(Name, "invalid deviceId", err)
			return
		}

		SetVolume(deviceId, -config.VolumeStepSize)

		return
	case ActionToggleMute:
		deviceId, err := strconv.Atoi(identifier)
		if err != nil {
			slog.Error(Name, "invalid deviceId", err)
			return
		}

		ToggleMute(deviceId)

		return
	case ActionSetDefaultDevice:
		deviceId, err := strconv.Atoi(identifier)
		if err != nil {
			slog.Error(Name, "invalid deviceId", err)
			return
		}

		SetDefaultDevice(deviceId)
		return
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}

const (
	IconSink        = "audio-volume-high-symbolic"
	IconSinkMuted   = "audio-volume-muted-symbolic"
	IconSource      = "microphone-sensitivity-high-symbolic"
	IconSourceMuted = "microphone-sensitivity-muted-symbolic"
)

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	devices, err := GetDevices()
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
		score, positions, start := common.FuzzyScore(query, dev.Description, exact)

		var usageScore int32

		var icon string
		if dev.PipewireType == PipewireTypeSink {
			if dev.Muted {
				icon = IconSinkMuted
			} else {
				icon = IconSink
			}
		} else {
			if dev.Muted {
				icon = IconSourceMuted
			} else {
				icon = IconSource
			}
		}

		actions := []string{ActionSetDefaultDevice, ActionIncreaseVolume, ActionDecreaseVolume, ActionToggleMute}

		info := fmt.Sprintf("Volume: %d%%", dev.Volume)
		if dev.Muted {
			info += " (Muted)"
		}

		if dev.Selected {
			info += " ✓"
		}

		if usageScore != 0 || score > config.MinScore || query == "" {
			entries = append(entries, &pb.QueryResponse_Item{
				Identifier: strconv.Itoa(dev.ID),
				Score:      score,
				Text:       dev.Description,
				Subtext:    info,
				Icon:       icon,
				Provider:   Name,
				Actions:    actions,
				Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
					Start:     start,
					Field:     "text",
					Positions: positions,
				},
				Type: pb.QueryResponse_REGULAR,
			})
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
