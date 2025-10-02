// Package symbols provides symbols/emojis.
package main

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/internal/comm/handlers"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/common"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var (
	Name       = "bluetooth"
	NamePretty = "Bluetooth"
	find       = false
)

//go:embed README.md
var readme string

type Config struct {
	common.Config `koanf:",squash"`
}

type Device struct {
	Name      string
	Mac       string
	Icon      string
	Paired    bool
	Trusted   bool
	Connected bool
}

var devices []Device

var config *Config

func Setup() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "bluetooth-symbolic",
			MinScore: 20,
		},
	}

	common.LoadConfig(Name, config)

	slog.Info(Name, "loaded", time.Since(start))
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Cleanup(qid uint32) {
}

const (
	ActionDisconnect = "disconnect"
	ActionConnect    = "connect"
	ActionRemove     = "remove"
	ActionPair       = "pair"
	ActionTrust      = "trust"
	ActionUntrust    = "untrust"
	ActionFind       = "find"
	ActionStopFind   = "stop_find"
)

func Activate(qid uint32, identifier, action string, arguments string) {
	cmd := exec.Command("bluetoothctl")

	removed := false
	added := false
	disconnect := false
	connect := false

	switch action {
	case ActionFind:
		handlers.ProviderUpdated <- "bluetooth:find"
		find = !find
		return
	case ActionPair:
		added = true
		handlers.ProviderUpdated <- "bluetooth:pair"
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`power on
pair %s
quit
`, identifier))
	case ActionRemove:
		removed = true
		handlers.ProviderUpdated <- "bluetooth:remove"
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`power on
remove %s
quit
`, identifier))
	case ActionTrust:
		handlers.ProviderUpdated <- "bluetooth:trust"
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`power on
trust %s
quit
`, identifier))
	case ActionConnect:
		connect = true
		handlers.ProviderUpdated <- "bluetooth:connect"
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`power on
connect %s
quit
`, identifier))
	case ActionUntrust:
		handlers.ProviderUpdated <- "bluetooth:untrust"
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`power on
untrust %s
quit
`, identifier))
	case ActionDisconnect:
		disconnect = true
		handlers.ProviderUpdated <- "bluetooth:disconnect"
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`power on
disconnect %s
quit
`, identifier))
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "activate", err)
	}

	slog.Debug(Name, "activate", out)

	if added || removed {
		for {
			found := make(map[string]struct{})
			time.Sleep(1 * time.Second)

			cmd = exec.Command("bluetoothctl", "devices", "Paired")
			out, err = cmd.CombinedOutput()
			if err != nil {
				slog.Error(Name, "get devices", err)
			}

			for v := range strings.Lines(strings.TrimSpace(string(out))) {
				fields := strings.Fields(v)

				found[fields[1]] = struct{}{}
			}

			if _, ok := found[identifier]; removed && !ok || added && ok {
				break
			}
		}
	}

	if connect || disconnect {
	outer:
		for {
			time.Sleep(1 * time.Second)

			cmd := exec.Command("bluetoothctl", "info", identifier)
			out, err := cmd.CombinedOutput()
			if err != nil {
				slog.Error(Name, "get info", err)
			}

			for l := range strings.Lines(string(out)) {
				if strings.HasPrefix(strings.TrimSpace(l), "Connected") {
					if connect && strings.Contains(l, "yes") {
						break outer
					}

					if disconnect && !strings.Contains(l, "yes") {
						break outer
					}
				}
			}
		}
	}

	handlers.ProviderUpdated <- "bluetooth:reload"
}

func Query(qid uint32, iid uint32, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	// if len(devices) == 0 {
	getDevices()
	// }

	for k, v := range devices {
		s := []string{}

		if v.Paired {
			s = append(s, "can_remove")

			if v.Trusted {
				s = append(s, "can_untrust")
			} else {
				s = append(s, "can_trust")
			}

			if v.Connected {
				s = append(s, "can_disconnect")
			} else {
				s = append(s, "can_connect")
			}
		} else {
			s = append(s, "can_pair")
		}

		e := &pb.QueryResponse_Item{
			Identifier: v.Mac,
			Score:      1000 - int32(k),
			State:      s,
			Icon:       v.Icon,
			Text:       v.Name,
			Subtext:    v.Mac,
			Provider:   Name,
			Type:       pb.QueryResponse_REGULAR,
		}

		if query != "" {
			score, pos, start := common.FuzzyScore(query, v.Name, exact)

			e.Score = score
			e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Field:     "text",
				Positions: pos,
				Start:     start,
			}
		}

		if e.Score > config.MinScore || query == "" {
			entries = append(entries, e)
		}
	}

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))
	return entries
}

func Icon() string {
	return config.Icon
}

func getDevices() {
	devices = []Device{}

	if find {
		cmd := exec.Command("bluetoothctl", "--timeout", "5", "scan", "on")
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "find devices", err)
		}

		for l := range strings.Lines(string(out)) {
			if strings.Contains(l, "Device") {
				f := strings.SplitN(l, " ", 4)

				d := Device{
					Name: strings.TrimSpace(f[3]),
					Mac:  f[2],
				}

				devices = append(devices, d)
			}
		}

		find = false

		cmd = exec.Command("bluetoothctl", "scan", "off")
		cmd.Run()

		return
	}

	devices = []Device{}

	cmd := exec.Command("bluetoothctl", "devices", "Paired")

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "get devices", err)
	}

	for v := range strings.Lines(string(out)) {
		fields := strings.SplitN(v, " ", 3)
		d := Device{
			Name: strings.TrimSpace(fields[2]),
			Mac:  fields[1],
		}

		cmd := exec.Command("bluetoothctl", "info", d.Mac)
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "get info", err)
		}

		for l := range strings.Lines(string(out)) {
			if strings.HasPrefix(strings.TrimSpace(l), "Icon") {
				d.Icon = strings.TrimPrefix(strings.TrimSpace(l), "Icon: ")
			}

			if strings.HasPrefix(strings.TrimSpace(l), "Paired") {
				if strings.Contains(l, "yes") {
					d.Paired = true
				}
			}

			if strings.HasPrefix(strings.TrimSpace(l), "Connected") {
				if strings.Contains(l, "yes") {
					d.Connected = true
				}
			}

			if strings.HasPrefix(strings.TrimSpace(l), "Trusted") {
				if strings.Contains(l, "yes") {
					d.Trusted = true
				}
			}
		}

		if d.Paired {
			devices = append(devices, d)
		}
	}
}
