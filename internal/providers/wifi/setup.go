package main

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
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
	common.Config      `koanf:",squash"`
	MessageTime        int    `koanf:"message_time" desc:"seconds to show status messages" default:"1"`
	ErrorTime          int    `koanf:"error_time" desc:"seconds to show error messages" default:"3"`
	Backend            string `koanf:"backend" desc:"wifi backend: auto, nm" default:"auto"`
	SubtextFormat      string `koanf:"subtext_format" desc:"subtext format. placeholders: %LOCK%, %STATUS%, %SIGNAL%, %FREQUENCY%, %SECURITY%" default:"%LOCK%  %STATUS%  %SIGNAL%  %FREQUENCY%  %SECURITY%"`
	ReopenAfterFail    bool   `koanf:"reopen_after_fail" desc:"reopen wifi menu after connection failure" default:"true"`
	ReopenAfterConnect bool   `koanf:"reopen_after_connect" desc:"reopen wifi menu after successful connection" default:"false"`
	ShowPasswordDots   bool   `koanf:"show_password_dots" desc:"show dots while typing password in terminal" default:"true"`
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
		MessageTime:        1,
		ErrorTime:          3,
		Backend:            "auto",
		SubtextFormat:      "%LOCK%  %STATUS%  %SIGNAL%  %FREQUENCY%  %SECURITY%",
		ReopenAfterFail:    true,
		ReopenAfterConnect: false,
		ShowPasswordDots:   true,
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
		backend.WaitForNetworks()
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
		backend.WaitForNetworks()
	case ActionConnect:
		if !selNetwork.Known && selNetwork.Security != "" {
			handlers.ProviderUpdated <- "wifi:connect"
			go connectWithTerminal(identifier)
			return
		}

		handlers.ProviderUpdated <- "wifi:connect"

		if err := backend.Connect(identifier, ""); err != nil {
			handlers.ProviderUpdated <- "wifi:connect_failed"
			slog.Error(Name, "activate", err)
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
		"%LOCK%", lock,
		"%STATUS%", status,
		"%SIGNAL%", signal,
		"%FREQUENCY%", n.Frequency,
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

func connectWithTerminal(ssid string) {
	r, w, err := os.Pipe()
	if err != nil {
		slog.Error(Name, "activate", err)
		return
	}
	defer r.Close()

	dot := "●"
	backspace := `printf '\b \b'`
	if !config.ShowPasswordDots {
		dot = ""
		backspace = ""
	}

	repl := strings.NewReplacer(
		"__DOT__", dot,
		"__BACKSPACE__", backspace,
		"__FD__", fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), w.Fd()),
	)

	script := repl.Replace(`
		printf 'Password for %s: ' "$WIFI_SSID"
		pw=""
		bs=$(printf '\177')
		while IFS= read -rsn1 c; do
			[ -z "$c" ] && break
			if [ "$c" = "$bs" ]; then
				if [ -n "$pw" ]; then
					pw="${pw%?}"
					__BACKSPACE__
				fi
			else
				pw="$pw$c"
				printf '__DOT__'
			fi
		done
		echo
		echo "$pw" > __FD__`,
	)

	terminal := common.GetTerminal()
	if terminal == "" {
		w.Close()
		slog.Error(Name, "activate", "no terminal found")
		return
	}

	cmd := exec.Command(terminal, "-e", "bash", "-c", script)
	cmd.Env = append(os.Environ(), "WIFI_SSID="+ssid) // Injected for sanitization
	if err := cmd.Start(); err != nil {
		w.Close()
		slog.Error(Name, "activate", err)
		return
	}

	cmd.Wait()
	w.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		slog.Error(Name, "activate", err)
		return
	}

	password := strings.TrimSpace(string(data))
	if password == "" {
		return
	}

	if err := backend.Connect(ssid, password); err != nil {
		slog.Error(Name, "activate", err)
		backend.Forget(ssid)

		if config.ReopenAfterFail {
			reopenWifiMenu()
			for range int(config.ErrorTime * 4) {
				handlers.ProviderUpdated <- "wifi:connect_failed"
				time.Sleep(250 * time.Millisecond)
			}
			handlers.ProviderUpdated <- "wifi:reset"
		}
	} else {
		if config.ReopenAfterConnect {
			reopenWifiMenu()
		}
		time.Sleep(time.Duration(config.MessageTime) * time.Second)
	}
}

func reopenWifiMenu() {
	// "elephant menu wifi" doesnt seem to work
	cmd := exec.Command("walker", "-m", "wifi")
	if err := cmd.Start(); err != nil {
		slog.Error(Name, "reopen", err)
		return
	}
	go cmd.Wait()
}
