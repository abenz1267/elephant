package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "systempower"
	NamePretty = "System Power"
	config     *Config
)

//go:embed README.md
var readme string

const (
	ActionLock      = "lock"
	ActionLogout    = "logout"
	ActionSuspend   = "suspend"
	ActionHibernate = "hibernate"
	ActionReboot    = "reboot"
	ActionShutdown  = "shutdown"
)

type Config struct {
	common.Config    `koanf:",squash"`
	LockCmd          string `koanf:"lock_cmd" desc:"command to lock the screen (auto-detected if empty)" default:""`
	LogoutCmd        string `koanf:"logout_cmd" desc:"command to logout (auto-detected if empty)" default:""`
	AutoDetectLock   bool   `koanf:"auto_detect_lock" desc:"auto-detect lock command (hyprlock, swaylock, loginctl)" default:"true"`
	AutoDetectLogout bool   `koanf:"auto_detect_logout" desc:"auto-detect logout command (hyprctl, swaymsg, loginctl)" default:"true"`
}

func detectLockCmd() string {
	for _, bin := range []string{"hyprlock", "swaylock", "loginctl"} {
		if _, err := exec.LookPath(bin); err == nil {
			switch bin {
			case "hyprlock":
				if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
					return "hyprlock"
				}
			case "swaylock":
				if os.Getenv("SWAYSOCK") != "" {
					return "swaylock"
				}
			default:
				return "loginctl lock-session"
			}
		}
	}
	return "loginctl lock-session"
}

func detectLogoutCmd() string {
	if hyprctl, _ := exec.LookPath("hyprctl"); hyprctl != "" {
		if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
			return "hyprctl dispatch exit"
		}
	}
	if swaymsg, _ := exec.LookPath("swaymsg"); swaymsg != "" {
		if os.Getenv("SWAYSOCK") != "" {
			return "swaymsg exit"
		}
	}
	return "loginctl terminate-session"
}

func Setup() {
	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon: "system-shutdown",
		},
		LockCmd:          "",
		LogoutCmd:        "",
		AutoDetectLock:   true,
		AutoDetectLogout: true,
	}

	common.LoadConfig(Name, config)

	if config.AutoDetectLock && config.LockCmd == "" {
		config.LockCmd = detectLockCmd()
	}
	if config.AutoDetectLogout && config.LogoutCmd == "" {
		config.LogoutCmd = detectLogoutCmd()
	}
}

func Available() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func PrintDoc(write bool) {
	if !write {
		fmt.Println(readme)
		fmt.Println()
	}

	util.PrintConfig(config, Name, write)
}

var actions = []struct {
	Name    string
	Action  string
	Icon    string
	Subtext string
}{
	{Name: "Lock", Action: ActionLock, Icon: "system-lock-screen", Subtext: "Lock screen now"},
	{Name: "Logout", Action: ActionLogout, Icon: "system-log-out", Subtext: "Log out of session"},
	{Name: "Suspend", Action: ActionSuspend, Icon: "system-suspend", Subtext: "Suspend to RAM"},
	{Name: "Hibernate", Action: ActionHibernate, Icon: "system-hibernate", Subtext: "Suspend to disk"},
	{Name: "Reboot", Action: ActionReboot, Icon: "system-reboot", Subtext: "Restart system"},
	{Name: "Shutdown", Action: ActionShutdown, Icon: "system-shutdown", Subtext: "Power off system"},
}

func Query(conn net.Conn, query string, _ bool, _ bool, _ uint8) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	for i, a := range actions {
		hasHibernate := a.Action == ActionHibernate

		if hasHibernate && !hibernationSupported() {
			continue
		}

		e := &pb.QueryResponse_Item{
			Identifier: a.Action,
			Text:       a.Name,
			Subtext:    a.Subtext,
			Icon:       a.Icon,
			Provider:   Name,
			Score:      int32(len(actions) - i + 100),
			Type:       pb.QueryResponse_REGULAR,
			Actions:    []string{a.Action},
		}

		if query != "" {
			score, _, _ := common.FuzzyScore(query, a.Name, false)
			e.Score = score

			if e.Score > config.MinScore {
				entries = append(entries, e)
			}
		} else {
			entries = append(entries, e)
		}
	}

	return entries
}

func Activate(_ bool, identifier, action string, _ string, _ string, _ uint8, _ net.Conn) {
	_ = identifier

	switch action {
	case ActionLock:
		runCmd(config.LockCmd, "Locking...")
	case ActionLogout:
		runCmd(config.LogoutCmd, "Logging out...")
	case ActionSuspend:
		runCmd("systemctl suspend", "Suspending...")
	case ActionHibernate:
		if hibernationSupported() {
			runCmd("systemctl hibernate", "Hibernating...")
		} else {
			slog.Error(Name, "hibernate", "not supported")
		}
	case ActionReboot:
		runCmd("systemctl reboot", "Rebooting...")
	case ActionShutdown:
		runCmd("systemctl poweroff", "Shutting down...")
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
	}
}

func runCmd(cmd, msg string) {
	if msg != "" {
		slog.Info(Name, "action", msg)
	}
	c := exec.Command("sh", "-c", cmd)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err := c.Start()
	if err != nil {
		slog.Error(Name, "runCmd", err)
	} else {
		go func() { c.Wait() }()
	}
}

func hibernationSupported() bool {
	data, err := os.ReadFile("/sys/power/image_size")
	if err != nil {
		return false
	}
	imageSize := strings.TrimSpace(string(data))
	if imageSize == "0" {
		return false
	}

	swapData, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return false
	}
	lines := strings.Split(string(swapData), "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "zram") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] != "0" {
			return true
		}
	}
	return false
}

func Icon() string {
	return config.Icon
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(_ string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}
