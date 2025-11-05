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
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
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
	ActionClearCache    = "clear_cache"
)

type Config struct {
	common.Config        `koanf:",squash"`
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
	FullInfo    string
}

type AURPackage struct {
	Name           string  `json:"Name"`
	Description    string  `json:"Description"`
	Version        string  `json:"Version"`
	URL            string  `json:"URL"`
	URLPath        string  `json:"URLPath"`
	Maintainer     string  `json:"Maintainer"`
	Submitter      string  `json:"Submitter"`
	NumVotes       int     `json:"NumVotes"`
	Popularity     float64 `json:"Popularity"`
	FirstSubmitted int64   `json:"FirstSubmitted"`
	LastModified   int64   `json:"LastModified"`
	OutOfDate      *int64  `json:"OutOfDate"`
}

func formatSingle(label, value string) string {
	return fmt.Sprintf("%-15s : %s\n\n", label, value)
}

func writeField(b *strings.Builder, label, value string) {
	if value != "" {
		b.WriteString(formatSingle(label, value))
	}
}

func writeTimestamp(b *strings.Builder, label string, ts int64) {
	if ts > 0 {
		b.WriteString(formatSingle(label, time.Unix(ts, 0).Format("Mon, 02 Jan 2006 15:04:05")))
	}
}

func detectHelper() string {
	helpers := []string{"paru", "yay"}
	for _, h := range helpers {
		if _, err := exec.LookPath(h); err == nil {
			return h
		}
	}
	return "sudo pacman"
}

func Setup() {
	helper := detectHelper()

	config = &Config{
		Config: common.Config{
			Icon:     "applications-internet",
			MinScore: 20,
		},
		CommandInstall:       helper + " -S %VALUE%",
		CommandRemove:        helper + " -R %VALUE%",
		AutoWrapWithTerminal: true,
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionClearCache:
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		entryMap = make(map[string]Entry)
		installed = []string{}
		installedOnly = false
		isSetup = false
		debug.FreeOSMemory()
		runtime.ReadMemStats(&m)
		return
	case ActionShowAll:
		installedOnly = false
		return
	case ActionShowInstalled:
		installedOnly = true
		return
	}

	name := entryMap[identifier].Name
	var pkgcmd string

	switch action {
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
		getInstalled()
		queryPacman()
		setupAUR()
		isSetup = true
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

			subtext := fmt.Sprintf("[%s]", strings.ToLower(v.Repository))
			if v.Installed {
				subtext = fmt.Sprintf("[%s] [installed]", strings.ToLower(v.Repository))
			}

			entries = append(entries, &pb.QueryResponse_Item{
				Identifier:  k,
				Text:        v.Name,
				Type:        pb.QueryResponse_REGULAR,
				Subtext:     subtext,
				Provider:    Name,
				State:       state,
				Actions:     a,
				Score:       score,
				Preview:     v.FullInfo,
				PreviewType: util.PreviewTypeText,
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

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
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
	var fullInfo strings.Builder

	for line := range strings.Lines(string(out)) {
		if strings.TrimSpace(line) == "" {
			e.FullInfo = fullInfo.String()

			md5 := md5.Sum(fmt.Appendf(nil, "%s:%s", e.Name, e.Description))
			md5str := hex.EncodeToString(md5[:])

			entryMap[md5str] = e
			e = Entry{}
			fullInfo.Reset()
			continue
		}

		fullInfo.WriteString(line)
		fullInfo.WriteString("\n")

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

	var aurPackages []AURPackage
	err = decoder.Decode(&aurPackages)
	if err != nil {
		slog.Error(Name, "jsondecode", err)
		return
	}

	for _, pkg := range aurPackages {
		e := Entry{
			Name:        pkg.Name,
			Description: pkg.Description,
			Version:     pkg.Version,
			Repository:  "aur",
			Installed:   slices.Contains(installed, pkg.Name),
		}

		var info strings.Builder
		info.WriteString(formatSingle("Repository", "aur"))
		info.WriteString(formatSingle("Name", pkg.Name))
		info.WriteString(formatSingle("Version", pkg.Version))
		info.WriteString(formatSingle("Description", pkg.Description))
		writeField(&info, "URL", pkg.URL)
		info.WriteString(formatSingle("AUR URL", "https://aur.archlinux.org"+pkg.URLPath))
		writeField(&info, "Maintainer", pkg.Maintainer)
		writeField(&info, "Submitter", pkg.Submitter)
		info.WriteString(formatSingle("Votes", fmt.Sprintf("%d", pkg.NumVotes)))
		info.WriteString(formatSingle("Popularity", fmt.Sprintf("%f", pkg.Popularity)))
		writeTimestamp(&info, "First Submitted", pkg.FirstSubmitted)
		writeTimestamp(&info, "Last Modified", pkg.LastModified)
		if pkg.OutOfDate != nil {
			info.WriteString(formatSingle("Out Of Date", "Yes"))
		} else {
			info.WriteString(formatSingle("Out Of Date", "No"))
		}
		e.FullInfo = info.String()

		md5 := md5.Sum(fmt.Appendf(nil, "%s:%s", e.Name, e.Description))
		md5str := hex.EncodeToString(md5[:])

		entryMap[md5str] = e
	}
}

func getInstalled() {
	installed = []string{}

	cmd := exec.Command("pacman", "-Qe")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "installed", err)
	}

	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			installed = append(installed, fields[0])
		}
	}
}
