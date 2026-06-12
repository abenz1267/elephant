package main

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"syscall"

	"al.essio.dev/pkg/shellescape"
	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "systempackage"
	NamePretty = "System Packages"
	config     *Config
)

//go:embed README.md
var readme string

const (
	ActionInstall = "install"
	ActionRemove  = "remove"
	ActionUpdate  = "update"
	ActionInfo    = "info"
)

type Config struct {
	common.Config `koanf:",squash"`
}

type PkgManager struct {
	name       string
	searchCmd  string // %s = query
	installCmd string // %s = package names
	removeCmd  string // %s = package names
	updateCmd  string
	infoCmd    string // %s = package name
	queryCmd   string // %s = package name (installed check)
}

var managers = map[string]PkgManager{
	"pacman": {
		name: "pacman", searchCmd: "pacman -Ss --color=never '%s'",
		installCmd: "sudo pacman -S --needed --noconfirm %s",
		removeCmd:  "sudo pacman -Rns --noconfirm %s",
		updateCmd:  "sudo pacman -Syu --noconfirm",
		infoCmd:    "pacman -Si '%s'",
		queryCmd:   "pacman -Q '%s'",
	},
	"apt": {
		name: "apt", searchCmd: "apt-cache search --names-only '%s'",
		installCmd: "sudo apt-get install -y %s",
		removeCmd:  "sudo apt-get remove -y %s",
		updateCmd:  "sudo apt-get update && sudo apt-get upgrade -y",
		infoCmd:    "apt-cache show '%s'",
		queryCmd:   "dpkg -s '%s'",
	},
	"dnf": {
		name: "dnf", searchCmd: "dnf search '%s'",
		installCmd: "sudo dnf install -y %s",
		removeCmd:  "sudo dnf remove -y %s",
		updateCmd:  "sudo dnf upgrade -y",
		infoCmd:    "dnf info '%s'",
		queryCmd:   "rpm -q '%s'",
	},
	"zypper": {
		name: "zypper", searchCmd: "zypper --no-refresh --no-abbrev search '%s'",
		installCmd: "sudo zypper install -y %s",
		removeCmd:  "sudo zypper remove -y %s",
		updateCmd:  "sudo zypper dup -y",
		infoCmd:    "zypper info '%s'",
		queryCmd:   "rpm -q '%s'",
	},
	"apk": {
		name: "apk", searchCmd: "apk search '%s'",
		installCmd: "sudo apk add %s",
		removeCmd:  "sudo apk del %s",
		updateCmd:  "sudo apk upgrade",
		infoCmd:    "apk info '%s'",
		queryCmd:   "apk info -e '%s'",
	},
}

func detectPkgManager() string {
	for _, bin := range []string{"pacman", "apt-get", "dnf", "zypper", "apk"} {
		_, err := exec.LookPath(bin)
		if err == nil {
			switch bin {
			case "pacman":
				return "pacman"
			case "apt-get":
				return "apt"
			case "dnf":
				return "dnf"
			case "zypper":
				return "zypper"
			case "apk":
				return "apk"
			}
		}
	}
	return ""
}

func getManager() (string, PkgManager) {
	manager := detectPkgManager()
	if mgr, ok := managers[manager]; ok {
		return manager, mgr
	}
	return "", PkgManager{}
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
			Icon:     "software-install",
			MinScore: 20,
		},
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	return detectPkgManager() != ""
}

func PrintDoc(write bool) {
	if !write {
		fmt.Println(readme)
		fmt.Println()
	}

	util.PrintConfig(config, Name, write)
}

func Query(conn net.Conn, query string, single bool, _ bool, _ uint8) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	managerName, mgr := getManager()
	if managerName == "" {
		e := &pb.QueryResponse_Item{
			Identifier: "none",
			Text:       "No supported package manager found",
			Subtext:    "Requires pacman, apt, dnf, zypper, or apk",
			Icon:       config.Icon,
			Provider:   Name,
			Score:      200,
			Type:       pb.QueryResponse_REGULAR,
		}
		return append(entries, e)
	}

	if query == "" {
		entries = append(entries, &pb.QueryResponse_Item{
			Identifier: "system-update",
			Text:       "Update System Packages",
			Subtext:    "Upgrade all installed packages",
			Icon:       "software-update-available",
			Provider:   Name,
			Score:      200,
			Type:       pb.QueryResponse_REGULAR,
			Actions:    []string{ActionUpdate},
		})
		return entries
	}

	if len(query) >= 2 {
		searchQuery := strings.ReplaceAll(query, "'", "'\\''")
		cmd := exec.Command("sh", "-c", fmt.Sprintf(mgr.searchCmd, searchQuery))
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "search", err, "out", string(out))
			return entries
		}

		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var pkgName, desc string
			switch managerName {
			case "pacman":
				parts := strings.SplitN(line, " ", 2)
				if len(parts) < 2 {
					continue
				}
				repoPkg := parts[0]
				if idx := strings.Index(repoPkg, "/"); idx != -1 {
					pkgName = repoPkg[idx+1:]
				} else {
					pkgName = repoPkg
				}
				desc = strings.TrimSpace(parts[1])
			case "apt":
				parts := strings.SplitN(line, " - ", 2)
				if len(parts) < 2 {
					continue
				}
				pkgName = strings.TrimSpace(parts[0])
				desc = strings.TrimSpace(parts[1])
			case "dnf":
				parts := strings.Fields(line)
				if len(parts) < 2 {
					continue
				}
				pkgName = strings.TrimSpace(parts[0])
				desc = strings.Join(parts[1:], " ")
			case "zypper":
				parts := strings.SplitN(line, "|", 3)
				if len(parts) < 3 {
					parts = strings.Fields(line)
					if len(parts) < 2 {
						continue
					}
				}
				pkgName = strings.TrimSpace(parts[1])
				desc = strings.TrimSpace(parts[len(parts)-1])
			case "apk":
				// Format: "pkgname-version description" or "pkg-name-1.2.3-r0 desc"
				spaceIdx := strings.Index(line, " ")
				var nameVer string
				if spaceIdx != -1 {
					nameVer = line[:spaceIdx]
					desc = strings.TrimSpace(line[spaceIdx+1:])
				} else {
					nameVer = line
				}
				segments := strings.Split(nameVer, "-")
				lastName := len(segments)
				for lastName > 0 {
					lastName--
					if len(segments[lastName]) > 0 && segments[lastName][0] >= '0' && segments[lastName][0] <= '9' {
						break
					}
				}
				if lastName > 0 {
					pkgName = strings.Join(segments[:lastName], "-")
				} else {
					pkgName = nameVer
				}
			}

			if pkgName == "" {
				continue
			}

			score, _, _ := common.FuzzyScore(query, pkgName, false)
			if score > config.MinScore {
				entries = append(entries, &pb.QueryResponse_Item{
					Identifier: pkgName,
					Text:       pkgName,
					Subtext:    desc,
					Icon:       config.Icon,
					Provider:   Name,
					Score:      score,
					Type:       pb.QueryResponse_REGULAR,
					Actions:    []string{ActionInstall, ActionInfo, ActionRemove},
				})
			}
		}
	}

	return entries
}

func Activate(_ bool, identifier, action string, _ string, _ string, _ uint8, _ net.Conn) {
	_, mgr := getManager()

	switch action {
	case ActionInstall:
		cmd := fmt.Sprintf(mgr.installCmd, shellescape.Quote(identifier))
		runTerminal(cmd)
	case ActionRemove:
		cmd := fmt.Sprintf(mgr.removeCmd, shellescape.Quote(identifier))
		runTerminal(cmd)
	case ActionUpdate:
		runTerminal(mgr.updateCmd)
	case ActionInfo:
		cmd := exec.Command("sh", "-c", fmt.Sprintf(mgr.infoCmd, shellescape.Quote(identifier)))
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "info", err, "out", string(out))
		} else {
			escaped := strings.ReplaceAll(string(out), "'", "'\\''")
			runTerminal(fmt.Sprintf("echo '%s'; read -p 'Press Enter...'", escaped))
		}
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
	}
}

func runTerminal(cmd string) {
	toRun := strings.TrimSpace(fmt.Sprintf("%s %s", common.LaunchPrefix(), common.WrapWithTerminal(cmd)))
	c := exec.Command("sh", "-c", toRun)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err := c.Start()
	if err != nil {
		slog.Error(Name, "runTerminal", err)
	} else {
		go func() { c.Wait() }()
	}
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
