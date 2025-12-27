package main

import (
	"bufio"
	"fmt"
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
	Name string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Repo string `json:"repo,omitempty"`
	Installed bool  `json:"installed,omitempty"`
	URL string `json:"url,omitempty"`
	Summary string `json:"summary,omitempty"`
	License string `json:"license,omitempty"`
}

var (
	Name       = "dnfpackages"
	NamePretty = "DNF Packages"
	config     *Config
	installedPackages = map[string]PackageDetail{}
	allPackages = map[string]PackageDetail{}
	installedOnly = false
)

const (
	ActionInstall  = "install"
	ActionRemove   = "remove"
	ActionRefresh  = "refresh"
	ActionShowInstalled = "show_installed"
	ActionShowAll       = "show_all"
	ActionVisitURL = "visit_url"
)

var readme string

type Config struct {
	common.Config `koanf:",squash"`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon: "system-software-install",
			MinScore: 20,
		},
	}

	common.LoadConfig(Name, config)

	if (config.NamePretty != "") { 
		NamePretty = config.NamePretty
	}

	refresh()
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	var pkgcmd string

	switch action {
	case ActionVisitURL:
		p := allPackages[identifier]
		run := strings.TrimSpace(fmt.Sprintf("%s xdg-open '%s'", common.LaunchPrefix(""), p.URL))
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

	toRun := common.WrapWithTerminal(fmt.Sprintf("sudo /usr/bin/dnf %s %s", pkgcmd, identifier))
	cmd := exec.Command("sh", "-c", toRun)
	err := cmd.Start();
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

func refreshPackages(refreshInstalledOnly bool) (map[string]PackageDetail,error) {
	startTime := time.Now()
	packages := map[string]PackageDetail{} 

	cmd := exec.Command("/usr/bin/dnf", "repoquery", "--queryformat", "%{NAME}|%{FULL_NEVRA}|%{REPOID}|%{URL}|%{SUMMARY}|%{LICENSE}\n")
	if refreshInstalledOnly {
		cmd = exec.Command("/usr/bin/dnf", "repoquery", "--installed", "--queryformat", "%{NAME}|%{FULL_NEVRA}|%{FROM_REPO}|%{URL}|%{SUMMARY}|%{LICENSE}\n")
	}

	output, err := cmd.StdoutPipe();

	if err != nil {
		slog.Error(Name, "could not fetch packages", err.Error())
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		slog.Error(Name, "could not fetch packages", err.Error())
		return nil, err
	}

	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "|")
		if len(fields) == 6 {

			pkg_installed := refreshInstalledOnly

			if !refreshInstalledOnly {
				if _, ok := installedPackages[fields[0]]; ok {
					pkg_installed = true
				}
			}

			entry := PackageDetail {
				Name: fields[0],
				Version: fields[1],
				Repo: fields[2],
				Installed: pkg_installed,
				URL: fields[3],
				Summary: fields[4],
				License: fields[5],
			}

			packages[fields[0]] = entry
		}
	}

	slog.Debug(Name, "query", time.Since(startTime))
	return packages, nil
}

func refreshInstalledPackages() {
	installedPackages, _ = refreshPackages(true)
}

func refreshAllPackages() {
	allPackages, _ = refreshPackages(false)
}


func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
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
			Identifier: p.Name,
			Text: p.Name,
			Subtext: subtext,
			Provider: Name,
			Actions: actions,
			Preview: buff.String(),
			PreviewType: util.PreviewTypeText,
		}

		if query != "" {
			score, positions, start := common.FuzzyScore(query, entry.Text, exact)

			entry.Score = score
			entry.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Start: start,
				Field: "text",
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

	if _, err := os.Stat("/usr/bin/dnf"); err != nil {
		slog.Info(Name, "available", "dnf command not found, disabling provider.")
		return false
	}

	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
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
