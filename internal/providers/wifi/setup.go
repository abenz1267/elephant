package main

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "wifi"
	NamePretty = "WiFi"
	on         = true
)

//go:embed README.md
var readme string

type Config struct {
	common.Config `koanf:",squash"`
	MessageTime   int    `koanf:"message_time" desc:"seconds to show status messages" default:"1"`
	ErrorTime     int    `koanf:"error_time" desc:"seconds to show error messages" default:"3"`
	Backend       string `koanf:"backend" desc:"wifi backend: auto, nm" default:"auto"`
	SubtextFormat string `koanf:"subtext_format" desc:"subtext format. placeholders: {lock}, {status}, {signal}, {frequency}, {security}" default:"{lock}  {status}  {signal}  {frequency}  {security}"`
}

type Network struct {
	SSID      string
	Signal    string
	Security  string
	Frequency string
	InUse     bool
	Known     bool
	UUID      string
}

var (
	networks []Network
	config   *Config
	backend  Backend
)

func Setup() {
	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	if config.Backend != "auto" {
		if b := detectBackend(config.Backend); b != nil {
			backend = b
		} else {
			slog.Warn(Name, "backend", fmt.Sprintf("configured backend %q not available, using auto-detected", config.Backend))
		}
	}

	on = backend.CheckWifiState()
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon:     "network-wireless-symbolic",
			MinScore: 20,
		},
		MessageTime:   1,
		ErrorTime:     3,
		Backend:       "auto",
		SubtextFormat: "{lock}  {status}  {signal}  {frequency}  {security}",
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	backend = detectBackend("auto")
	if backend == nil {
		slog.Info(Name, "available", "no wifi backend found (nmcli/iwctl). disabling")
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

const (
	ActionWifiOff    = "wifi_off"
	ActionWifiOn     = "wifi_on"
	ActionConnect    = "connect"
	ActionDisconnect = "disconnect"
	ActionForget     = "forget"
	ActionScan       = "scan"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	selNetwork := findNetwork(identifier)
	if selNetwork == nil && (action == ActionConnect || action == ActionDisconnect || action == ActionForget) {
		slog.Error(Name, "activate", fmt.Sprintf("network %q not found", identifier))
		return
	}

	switch action {
	case ActionWifiOn:
		handlers.ProviderUpdated <- "wifi:wifion"
		if err := backend.SetWifiEnabled(true); err != nil {
			slog.Error(Name, "activate", err)
		}
		on = true
		backend.Scan()
	case ActionWifiOff:
		handlers.ProviderUpdated <- "wifi:wifioff"
		if err := backend.SetWifiEnabled(false); err != nil {
			slog.Error(Name, "activate", err)
		}
		on = false
		networks = nil
		time.Sleep(time.Duration(config.MessageTime) * time.Second)
	case ActionScan:
		handlers.ProviderUpdated <- "wifi:scan"
		backend.Scan()
	case ActionConnect:
		if args == "" && !selNetwork.Known && selNetwork.Security != "" {
			handlers.ProviderUpdated <- "wifi:password_required"
			time.Sleep(time.Duration(config.ErrorTime) * time.Second)
			return
		}

		handlers.ProviderUpdated <- "wifi:connect"

		password := args
		if selNetwork.Known {
			password = ""
		}

		wasKnown := selNetwork.Known

		if err := backend.Connect(identifier, password); err != nil {
			handlers.ProviderUpdated <- "wifi:connect_failed"
			slog.Error(Name, "activate", err)
			if !wasKnown {
				backend.Forget(identifier)
			}
			time.Sleep(time.Duration(config.ErrorTime) * time.Second)
		} else {
			time.Sleep(time.Duration(config.MessageTime) * time.Second)
		}
	case ActionDisconnect:
		handlers.ProviderUpdated <- "wifi:disconnect"
		if err := backend.Disconnect(identifier); err != nil {
			slog.Error(Name, "activate", err)
		}
		time.Sleep(time.Duration(config.MessageTime) * time.Second)
	case ActionForget:
		handlers.ProviderUpdated <- "wifi:forget"
		if err := backend.Forget(identifier); err != nil {
			slog.Error(Name, "activate", err)
		}
		time.Sleep(time.Duration(config.MessageTime) * time.Second)
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	if !on {
		return entries
	}

	networks = backend.GetNetworks()

	for k, v := range networks {
		s := []string{}
		a := []string{}

		if v.InUse {
			s = append(s, "connected")
			a = append(a, ActionDisconnect)
		} else {
			a = append(a, ActionConnect)
		}

		if v.Known {
			s = append(s, "known")
			a = append(a, ActionForget)
		}

		subtext := formatSubtext(v)

		icon := signalIcon(v.Signal)

		score := 1000 - int32(k)
		if v.InUse {
			score = 10000
		} else if v.Known {
			score = 5000 - int32(k)
		}

		e := &pb.QueryResponse_Item{
			Identifier: v.SSID,
			Score:      score,
			State:      s,
			Actions:    a,
			Icon:       icon,
			Text:       v.SSID,
			Subtext:    subtext,
			Provider:   Name,
			Type:       pb.QueryResponse_REGULAR,
		}

		if query != "" {
			score, pos, start := common.FuzzyScore(query, v.SSID, exact)

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
	actions := []string{}

	if on {
		actions = append(actions, ActionWifiOff, ActionScan)
	} else {
		actions = append(actions, ActionWifiOn)
	}

	return &pb.ProviderStateResponse{
		States:   []string{},
		Actions:  actions,
		Provider: "",
	}
}

func signalIcon(signal string) string {
	if signal == "" {
		return "network-wireless-symbolic"
	}

	var level int
	fmt.Sscanf(signal, "%d", &level)

	switch {
	case level >= 75:
		return "network-wireless-signal-excellent-symbolic"
	case level >= 50:
		return "network-wireless-signal-good-symbolic"
	case level >= 25:
		return "network-wireless-signal-ok-symbolic"
	default:
		return "network-wireless-signal-weak-symbolic"
	}
}

// freqBand converts a frequency string like "5220 MHz" to a band label like "5 GHz".
func freqBand(freq string) string {
	freq = strings.TrimSpace(freq)
	freq = strings.TrimSuffix(freq, " MHz")
	mhz, err := strconv.Atoi(freq)
	if err != nil {
		return ""
	}

	switch {
	case mhz < 5000:
		return "2.4 GHz"
	case mhz < 5925:
		return "5 GHz"
	default:
		return "6 GHz"
	}
}

func formatSubtext(n Network) string {
	lock := "🌐"
	if n.Security != "" {
		if n.Known {
			lock = "🔓"
		} else {
			lock = "🔒"
		}
	}

	status := ""
	if n.InUse {
		status = "Connected"
	} else if n.Known {
		status = "Saved"
	}

	signal := ""
	if n.Signal != "" {
		signal = fmt.Sprintf("%s%%", n.Signal)
	}

	r := strings.NewReplacer(
		"{lock}", lock,
		"{status}", status,
		"{signal}", signal,
		"{frequency}", n.Frequency,
		"{security}", n.Security,
	)

	result := r.Replace(config.SubtextFormat)

	// collapse multiple consecutive spaces from empty placeholders
	for strings.Contains(result, "   ") {
		result = strings.ReplaceAll(result, "   ", "  ")
	}

	return strings.TrimSpace(result)
}

func findNetwork(ssid string) *Network {
	for k := range networks {
		if networks[k].SSID == ssid {
			return &networks[k]
		}
	}

	return nil
}
