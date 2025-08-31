package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var (
	Name       = "archlinuxpkgs"
	NamePretty = "Arch Linux Packages"
	config     *Config
	isSetup    = false
	entryMap   = map[string]Entry{}
	installed  = []string{}
	command    = "yay -S"
)

const (
	ActionInstall = "install"
	ActionRemove  = "remove"
)

type Config struct {
	common.Config   `koanf:",squash"`
	RefreshInterval int    `koanf:"refresh_interval" desc:"refresh database every X minutes" default:"10"`
	InstalledPrefix string `koanf:"installed_prefix" desc:"prefix to use to show only explicitly installed packages" default:"i:"`
}

type Entry struct {
	Name        string
	Description string
	Repository  string
	Version     string
	Installed   bool
}

func init() {
	config = &Config{
		Config: common.Config{
			Icon:     "applications-internet",
			MinScore: 20,
		},
		RefreshInterval: 10,
		InstalledPrefix: "i:",
	}

	common.LoadConfig(Name, config)

	path, _ := exec.LookPath("paru")
	if path != "" {
		command = "paru -S"
	}

	go refresh()
}

func PrintDoc() {
	fmt.Printf("### %s\n", NamePretty)
	fmt.Println("Arch Linux Packages: find packages and install them.")
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Cleanup(qid uint32) {
}

func Activate(qid uint32, identifier, action string, query string) {
	name := entryMap[identifier].Name
	var pkgcmd string

	switch action {
	case ActionInstall:
		pkgcmd = "sudo pacman -S"

		if entryMap[identifier].Repository == "AUR" {
			pkgcmd = command
		}
	case ActionRemove:
		pkgcmd = "sudo pacman -R"
	}

	toRun := common.WrapWithTerminal(fmt.Sprintf("%s %s", pkgcmd, name))

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

func Query(qid uint32, iid uint32, query string, single bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	oi := false

	if strings.HasPrefix(query, config.InstalledPrefix) {
		oi = true
		query = strings.TrimPrefix(query, config.InstalledPrefix)
	}

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

		if (score > config.MinScore || query == "") && (!oi || (oi && v.Installed)) {
			entries = append(entries, &pb.QueryResponse_Item{
				Identifier: k,
				Text:       v.Name,
				Type:       pb.QueryResponse_REGULAR,
				Subtext:    fmt.Sprintf("%s (%s) (%s)", v.Description, v.Version, v.Repository),
				Provider:   Name,
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

	isSetup = true
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
		isSetup = false
		entryMap = make(map[string]Entry)
		getInstalled()
		queryPacman()
		setupAUR()
		time.Sleep(time.Duration(config.RefreshInterval) * time.Minute)
	}
}
