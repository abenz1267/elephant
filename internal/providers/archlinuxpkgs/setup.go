package main

import (
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name          = "archlinuxpkgs"
	NamePretty    = "Arch Linux Packages"
	config        *Config
	isSetup       = false
	mut           sync.Mutex
	entryMap      = map[string]Entry{}
	installed     = []string{}
	installedOnly = false
)

//go:embed README.md
var readme string

const (
	ActionInstall       = "install"
	ActionRemove        = "remove"
	ActionShowInstalled = "show_installed"
	ActionShowAll       = "show_all"
)

type Config struct {
	common.Config        `koanf:",squash"`
	RefreshInterval      int    `koanf:"refresh_interval" desc:"refresh database every X minutes. 0 disables the automatic refresh and refreshing requires an elephant restart." default:"60"`
	CommandInstall       string `koanf:"command_install" desc:"default command for AUR packages to install. supports %VALUE%." default:"yay -S %VALUE%"`
	CommandRemove        string `koanf:"command_remove" desc:"default command to remove packages. supports %VALUE%." default:"sudo pacman -R %VALUE%"`
	AutoWrapWithTerminal bool   `koanf:"auto_wrap_with_terminal" desc:"automatically wraps the command with terminal" default:"true"`
}

type Entry struct {
	Name        string
	Description string
	Repository  string
	Version     string
	Installed   bool
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "applications-internet",
			MinScore: 20,
		},
		RefreshInterval:      60,
		CommandInstall:       "yay -S %VALUE%",
		CommandRemove:        "sudo pacman -R %VALUE%",
		AutoWrapWithTerminal: true,
	}

	common.LoadConfig(Name, config)

	go refresh()
}

func Available() bool {
	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Activate(identifier, action string, query string, args string) {
	name := entryMap[identifier].Name
	var pkgcmd string

	switch action {
	case ActionShowAll:
		installedOnly = false
		return
	case ActionShowInstalled:
		installedOnly = true
		return
	case ActionInstall:
		pkgcmd = config.CommandInstall
	case ActionRemove:
		pkgcmd = config.CommandRemove
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}

	pkgcmd = strings.ReplaceAll(pkgcmd, "%VALUE%", name)
	toRun := common.WrapWithTerminal(pkgcmd)

	if !config.AutoWrapWithTerminal {
		toRun = pkgcmd
	}

	cmd := exec.Command("sh", "-c", toRun)
	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate", err)
	} else {
		go func() {
			cmd.Wait()
		}()
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	if !isSetup {
		return entries
	}

	for k, v := range entryMap {
		score, positions, s := common.FuzzyScore(query, v.Name, exact)

		score2, positions2, s2 := common.FuzzyScore(query, v.Description, exact)

		if score2 > score {
			score = score2 / 2
			positions = positions2
			s = s2
		}

		if (score > config.MinScore || query == "") && (!installedOnly || (installedOnly && v.Installed)) {
			state := []string{}
			a := []string{}

			if v.Installed {
				state = append(state, "installed")
				a = append(a, ActionRemove)
			} else {
				state = append(state, "available")
				a = append(a, ActionInstall)
			}

			name := v.Name

			if !installedOnly && v.Installed {
				name = fmt.Sprintf("%s (installed)", name)
			}

			entries = append(entries, &pb.QueryResponse_Item{
				Identifier: k,
				Text:       name,
				Type:       pb.QueryResponse_REGULAR,
				Subtext:    fmt.Sprintf("%s (%s) (%s)", v.Description, v.Version, v.Repository),
				Provider:   Name,
				State:      state,
				Actions:    a,
				Score:      score,
				Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
					Start:     s,
					Field:     "text",
					Positions: positions,
				},
			})
		}
	}

	if query == "" {
		slices.SortFunc(entries, func(a, b *pb.QueryResponse_Item) int {
			return strings.Compare(a.Text, b.Text)
		})
	}

	return entries
}

func Icon() string {
	return config.Icon
}

func State() *pb.ProviderStateResponse {
	if installedOnly {
		return &pb.ProviderStateResponse{
			Actions: []string{ActionShowAll},
		}
	}

	return &pb.ProviderStateResponse{
		Actions: []string{ActionShowInstalled},
	}
}

func queryPacman() {
	cmd := exec.Command("pacman", "-Si")

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "pacman", err)
	}

	e := Entry{}

	for line := range strings.Lines(string(out)) {
		if strings.TrimSpace(line) == "" {
			md5 := md5.Sum(fmt.Appendf(nil, "%s:%s", e.Name, e.Description))
			md5str := hex.EncodeToString(md5[:])

			entryMap[md5str] = e
			e = Entry{}
		}

		switch {
		case strings.HasPrefix(line, "Repository"):
			e.Repository = strings.TrimSpace(strings.Split(line, ":")[1])
		case strings.HasPrefix(line, "Name"):
			e.Name = strings.TrimSpace(strings.Split(line, ":")[1])
			e.Installed = slices.Contains(installed, e.Name)
		case strings.HasPrefix(line, "Description"):
			e.Description = strings.TrimSpace(strings.Split(line, ":")[1])
		case strings.HasPrefix(line, "Version"):
			e.Version = strings.TrimSpace(strings.Split(line, ":")[1])
		}
	}
}

func setupAUR() {
	resp, err := http.Get("https://aur.archlinux.org/packages-meta-v1.json.gz")
	if err != nil {
		slog.Error(Name, "aurdownload", err)
		return
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)

	var entries []Entry
	err = decoder.Decode(&entries)
	if err != nil {
		slog.Error(Name, "jsondecode", err)
		return
	}

	for _, e := range entries {
		e.Repository = "AUR"

		e.Installed = slices.Contains(installed, e.Name)
		md5 := md5.Sum(fmt.Appendf(nil, "%s:%s", e.Name, e.Description))
		md5str := hex.EncodeToString(md5[:])

		entryMap[md5str] = e
	}
}

func getInstalled() {
	cmd := exec.Command("pacman", "-Qe")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "installed", err)
	}

	for line := range strings.Lines(string(out)) {
		installed = append(installed, strings.Fields(line)[0])
	}
}

func refresh() {
	for {
		mut.Lock()
		entryMap = make(map[string]Entry)
		getInstalled()
		queryPacman()
		setupAUR()
		mut.Unlock()

		isSetup = true

		if config.RefreshInterval == 0 {
			break
		}

		time.Sleep(time.Duration(config.RefreshInterval) * time.Minute)
	}
}
