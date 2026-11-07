package main

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name        = "wifi"
	NamePretty  = "WiFi"
	wifiEnabled = true
)

//go:embed README.md
var readme string

type Config struct {
	common.Config       `koanf:",squash"`
	MessageTime         int    `koanf:"message_time" desc:"seconds to show status messages" default:"1"`
	ErrorTime           int    `koanf:"error_time" desc:"seconds to show error messages" default:"3"`
	Backend             string `koanf:"backend" desc:"wifi backend: auto, nm" default:"auto"`
	PasswordPrompt      string `koanf:"password_prompt" desc:"password prompt method: auto, terminal, walker, rofi, wofi, custom" default:"walker"`
	CustomPromptCommand string `koanf:"custom_prompt_command" desc:"custom command for 'custom' password prompt. use %PROMPT% as placeholder" default:""`
	SubtextFormat       string `koanf:"subtext_format" desc:"subtext format. placeholders: %LOCK%, %STATUS%, %SIGNAL%, %FREQUENCY%, %SECURITY%" default:"%LOCK%  %STATUS%  %SIGNAL%  %FREQUENCY%  %SECURITY%"`
	ReopenAfterFail     bool   `koanf:"reopen_after_fail" desc:"reopen wifi menu after connection failure" default:"true"`
	ReopenAfterConnect  bool   `koanf:"reopen_after_connect" desc:"reopen wifi menu after successful connection" default:"false"`
	ShowPasswordDots    bool   `koanf:"show_password_dots" desc:"show dots while typing password in terminal" default:"true"`
	Notify              bool   `koanf:"notify" desc:"show desktop notifications" default:"true"`
}

type Network struct {
	SSID      string
	Signal    int
	Security  string
	Frequency int
	InUse     bool
	Known     bool
	UUID      string
}

var (
	networks       []Network
	config         *Config
	backend        Backend
	passwordPrompt PasswordPrompt
)

func Setup() {
	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	backend = detectBackend(config.Backend)
	if backend == nil {
		slog.Error(Name, "Setup", "no backend available")
		return
	}

	passwordPrompt = detectPasswordPrompt(config.PasswordPrompt)
	if passwordPrompt == nil {
		slog.Warn(Name, "Setup", "no password prompt found, password-protected networks will be skipped")
	}

	wifiEnabled = backend.CheckWifiState()
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon:     "network-wireless-symbolic",
			MinScore: 20,
		},
		MessageTime:        1,
		ErrorTime:          3,
		Backend:            "auto",
		PasswordPrompt:     "walker",
		SubtextFormat:      "%LOCK%  %STATUS%  %SIGNAL%  %FREQUENCY%  %SECURITY%",
		ReopenAfterFail:    true,
		ReopenAfterConnect: false,
		ShowPasswordDots:   true,
		Notify:             true,
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	if backend == nil {
		backend = detectBackend("auto")
	}

	if backend == nil {
		slog.Info(Name, "Available", "no wifi backend found (nmcli). disabling")
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
		slog.Error(Name, "Activate", fmt.Sprintf("network %q not found", identifier))
		return
	}

	switch action {
	case ActionWifiOn:
		handlers.ProviderUpdated <- "wifi:wifion"
		if err := backend.SetWifiEnabled(true); err != nil {
			slog.Error(Name, "Activate_WifiOn", err)
			return
		}
		wifiEnabled = true
		backend.WaitForNetworks()
	case ActionWifiOff:
		handlers.ProviderUpdated <- "wifi:wifioff"
		if err := backend.SetWifiEnabled(false); err != nil {
			slog.Error(Name, "Activate_WifiOff", err)
			return
		}
		wifiEnabled = false
		networks = nil
		time.Sleep(time.Duration(config.MessageTime) * time.Second)
	case ActionScan:
		handlers.ProviderUpdated <- "wifi:scan"
		backend.WaitForNetworks()
	case ActionConnect:
		handlers.ProviderUpdated <- "wifi:connect"

		password := ""
		if !selNetwork.Known && selNetwork.Security != "" {
			if passwordPrompt == nil {
				slog.Error(Name, "Activate_Connect", "no password prompt available for password-protected network")
				return
			}

			var err error
			password, err = passwordPrompt.RequestPassword(identifier)
			if err != nil {
				slog.Error(Name, "Activate_Connect", err)
				return
			}

			if password == "" {
				slog.Info(Name, "Activate_Connect", "password prompt cancelled")
				return
			}
		}

		if err := backend.Connect(identifier, password); err != nil {
			slog.Error(Name, "Activate_Connect", err)
			if err := backend.Forget(identifier); err != nil {
				slog.Warn(Name, "Activate_Connect", "forget after failed connect: "+err.Error())
			}

			if password != "" {
				notify(fmt.Sprintf("Wrong password for %s", identifier))
			} else {
				notify(fmt.Sprintf("Failed to connect to %s", identifier))
			}

			if config.ReopenAfterFail {
				reopenWifiMenu()
				delay := 100 //in ms
				for range int(config.ErrorTime * 1000 / delay) {
					handlers.ProviderUpdated <- "wifi:connect_failed"
					time.Sleep(time.Duration(delay) * time.Millisecond)
				}
				handlers.ProviderUpdated <- "wifi:reset"
			}
		} else {
			notify(fmt.Sprintf("Connected to %s", identifier))
			if config.ReopenAfterConnect {
				reopenWifiMenu()
			}
			time.Sleep(time.Duration(config.MessageTime) * time.Second)
		}
	case ActionDisconnect:
		handlers.ProviderUpdated <- "wifi:disconnect"
		if err := backend.Disconnect(selNetwork.SSID); err != nil {
			slog.Error(Name, "Activate_Disconnect", err)
		}
		time.Sleep(time.Duration(config.MessageTime) * time.Second)
	case ActionForget:
		handlers.ProviderUpdated <- "wifi:forget"
		if err := backend.Forget(selNetwork.SSID); err != nil {
			slog.Error(Name, "Activate_Forget", err)
		}
		time.Sleep(time.Duration(config.MessageTime) * time.Second)
	default:
		slog.Error(Name, "Activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	if !wifiEnabled {
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
			score, pos, matchStart := common.FuzzyScore(query, v.SSID, exact)

			e.Score = score
			e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Field:     "text",
				Positions: pos,
				Start:     matchStart,
			}
		}

		if e.Score > config.MinScore || query == "" {
			entries = append(entries, e)
		}
	}

	slog.Debug(Name, "Query", time.Since(start))
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

	if wifiEnabled {
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

func signalIcon(signal int) string {
	switch {
	case signal >= 75:
		return "network-wireless-signal-excellent-symbolic"
	case signal >= 50:
		return "network-wireless-signal-good-symbolic"
	case signal >= 25:
		return "network-wireless-signal-ok-symbolic"
	default:
		return "network-wireless-signal-weak-symbolic"
	}
}

func freqBand(mhz int) string {
	switch {
	case mhz <= 0:
		return ""
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
	if n.Signal > 0 {
		signal = fmt.Sprintf("%d%%", n.Signal)
	}

	r := strings.NewReplacer(
		"%LOCK%", lock,
		"%STATUS%", status,
		"%SIGNAL%", signal,
		"%FREQUENCY%", freqBand(n.Frequency),
		"%SECURITY%", n.Security,
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

func notify(msg string) {
	if !config.Notify {
		return
	}
	slog.Debug(Name, "notify", msg)
	err := exec.Command("notify-send", "-a", "elephant", "-i", config.Icon, "-e", msg).Run()
	if err != nil {
		slog.Error(Name, "notify", err)
	}
}

func reopenWifiMenu() {
	// "elephant menu wifi" doesnt seem to work
	err := exec.Command("walker", "-m", "wifi").Run()
	if err != nil {
		slog.Error(Name, "reopenWifiMenu", err)
	}
}
