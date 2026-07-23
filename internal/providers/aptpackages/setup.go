package main

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

type PackageDetail struct {
	Name      string `json:"name,omitempty"`
	Version   string `json:"version,omitempty"`
	Repo      string `json:"repo,omitempty"`
	Installed bool   `json:"installed,omitempty"`
	URL       string `json:"url,omitempty"`
	Summary   string `json:"summary,omitempty"`
	License   string `json:"license,omitempty"`
}

var (
	Name              = "aptpackages"
	NamePretty        = "APT Packages"
	config            *Config
	installedPackages = map[string]PackageDetail{}
	allPackages       = map[string]PackageDetail{}
	installedOnly     = false
)

const (
	ActionInstall       = "install"
	ActionRemove        = "remove"
	ActionRefresh       = "refresh"
	ActionShowInstalled = "show_installed"
	ActionShowAll       = "show_all"
	ActionVisitURL      = "visit_url"
)

var readme string

type Config struct {
	common.Config `koanf:",squash"`
}

func Setup() {
	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	refresh()
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon:     "system-software-install",
			MinScore: 20,
		},
	}

	common.LoadConfig(Name, config)
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	var pkgcmd string

	switch action {
	case ActionVisitURL:
		p := allPackages[identifier]
		run := strings.TrimSpace(fmt.Sprintf("%s xdg-open '%s'", common.LaunchPrefix(), p.URL))
		cmd := exec.Command("sh", "-c", run)

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "activate", err, "action", action)
		} else {
			go func() {
				_ = cmd.Wait()
			}()
		}

		return
	case ActionShowAll:
		installedOnly = false
		return
	case ActionShowInstalled:
		installedOnly = true
		return
	case ActionRefresh:
		refresh()
		return
	case ActionInstall:
		slog.Info(Name, "activate", fmt.Sprintf("Installing package %s", identifier))
		pkgcmd = "install"
	case ActionRemove:
		slog.Info(Name, "activate", fmt.Sprintf("Removing package %s", identifier))
		pkgcmd = "remove"
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}

	toRun := common.WrapWithTerminal(fmt.Sprintf("sudo /usr/bin/apt %s %s", pkgcmd, identifier))
	cmd := exec.Command("sh", "-c", toRun)
	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "activate", fmt.Sprintf("could not install package %s", err.Error()))
	} else {
		go func() {
			_ = cmd.Wait()
		}()
	}
}

func refresh() {
	refreshInstalledPackages()
	refreshAllPackages()
}

func refreshPackages(refreshInstalledOnly bool) (map[string]PackageDetail, error) {
	startTime := time.Now()
	packages := map[string]PackageDetail{}

	var cmd *exec.Cmd
	if refreshInstalledOnly {
		cmd = exec.Command("/usr/bin/dpkg-query", "-W", "-f", "${Package}|${Version}|${Section}|${Homepage}|${db:Status-Status}|${binary:Summary}\n")
	} else {
		cmd = exec.Command("/usr/bin/apt-cache", "dumpavail")
	}

	output, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error(Name, "could not fetch packages", err.Error())
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		slog.Error(Name, "could not fetch packages", err.Error())
		return nil, err
	}

	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 512*1024), 512*1024)

	if refreshInstalledOnly {
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), "|")
			if len(fields) >= 6 && fields[0] != "" && fields[4] == "installed" {
				entry := PackageDetail{
					Name:      fields[0],
					Version:   fields[1],
					Repo:      fields[2],
					Installed: true,
					URL:       fields[3],
					Summary:   strings.Join(fields[5:], "|"),
				}
				packages[fields[0]] = entry
			}
		}
	} else {
		var pkg PackageDetail
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if pkg.Name != "" {
					if _, ok := installedPackages[pkg.Name]; ok {
						pkg.Installed = true
					}
					packages[pkg.Name] = pkg
					pkg = PackageDetail{}
				}
				continue
			}
			if strings.HasPrefix(line, "Package: ") {
				pkg.Name = strings.TrimPrefix(line, "Package: ")
			} else if strings.HasPrefix(line, "Version: ") {
				pkg.Version = strings.TrimPrefix(line, "Version: ")
			} else if strings.HasPrefix(line, "Section: ") {
				pkg.Repo = strings.TrimPrefix(line, "Section: ")
			} else if strings.HasPrefix(line, "Homepage: ") {
				pkg.URL = strings.TrimPrefix(line, "Homepage: ")
			} else if strings.HasPrefix(line, "Description: ") {
				pkg.Summary = strings.TrimPrefix(line, "Description: ")
			}
		}
		if pkg.Name != "" {
			if _, ok := installedPackages[pkg.Name]; ok {
				pkg.Installed = true
			}
			packages[pkg.Name] = pkg
		}
	}

	_, _ = io.Copy(io.Discard, output)
	_ = cmd.Wait()
	slog.Debug(Name, "query", time.Since(startTime))
	return packages, nil
}

func refreshInstalledPackages() {
	installedPackages, _ = refreshPackages(true)
}

func refreshAllPackages() {
	allPackages, _ = refreshPackages(false)
}

func Query(conn net.Conn, query string, runes []rune, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	startTime := time.Now()
	entries := []*pb.QueryResponse_Item{}

	packages := allPackages
	if installedOnly {
		packages = installedPackages
	}

	for _, p := range packages {

		var actions []string
		if p.Installed {
			actions = []string{ActionRemove, ActionVisitURL}
		} else {
			actions = []string{ActionInstall, ActionVisitURL}
		}

		var subtext string
		if p.Installed && !installedOnly {
			subtext = fmt.Sprintf("%s (installed)", p.Version)
		} else {
			subtext = p.Version
		}

		var buff strings.Builder
		fmt.Fprintf(&buff, "%-*s: %s\n", 15, "Name", p.Name)
		fmt.Fprintf(&buff, "%-*s: %s\n", 15, "Summary", p.Summary)
		fmt.Fprintf(&buff, "%-*s: %s\n", 15, "version", p.Version)
		fmt.Fprintf(&buff, "%-*s: %s\n", 15, "License", p.License)
		fmt.Fprintf(&buff, "%-*s: %s\n", 15, "Repository", p.Repo)
		fmt.Fprintf(&buff, "%-*s: %s\n", 15, "URL", p.URL)

		entry := &pb.QueryResponse_Item{
			Identifier:  p.Name,
			Text:        p.Name,
			Subtext:     subtext,
			Provider:    Name,
			Actions:     actions,
			Preview:     buff.String(),
			PreviewType: util.PreviewTypeText,
		}

		if query != "" {
			score, positions, start := common.FuzzyScore(query, runes, entry.Text, exact)

			entry.Score = score
			entry.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Start:     start,
				Field:     "text",
				Positions: positions,
			}
		}

		if query == "" || entry.Score > config.MinScore {
			entries = append(entries, entry)
		}
	}

	slog.Debug(Name, "query", time.Since(startTime))
	return entries
}

func Available() bool {
	if _, err := os.Stat("/usr/bin/apt-cache"); err != nil {
		slog.Info(Name, "available", "apt-cache command not found, disabling provider.")
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

func Icon() string {
	return config.Icon
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	var actions []string
	if installedOnly {
		actions = []string{ActionRefresh, ActionShowAll}
	} else {
		actions = []string{ActionRefresh, ActionShowInstalled}
	}

	return &pb.ProviderStateResponse{
		Actions: actions,
	}
}
