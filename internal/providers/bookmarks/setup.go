package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"github.com/go-git/go-git/v6"
)

var (
	Name       = "bookmarks"
	NamePretty = "Bookmarks"
	config     *Config
	bookmarks  = []Bookmark{}
	r          *git.Repository
	w          *git.Worktree
)

//go:embed README.md
var readme string

type Config struct {
	common.Config      `koanf:",squash"`
	CreatePrefix       string     `koanf:"create_prefix" desc:"prefix used in order to create a new bookmark. will otherwise be based on matches (min_score)." default:""`
	Location           string     `koanf:"location" desc:"location of the CSV file" default:"elephant cache dir"`
	Categories         []Category `koanf:"categories" desc:"categories" default:""`
	Browsers           []Browser  `koanf:"browsers" desc:"browsers for opening bookmarks" default:""`
	SetBrowserOnImport bool       `koanf:"set_browser_on_import" desc:"set browser name on imported bookmarks" default:"false"`
}

type Category struct {
	Name   string `koanf:"name" desc:"name for category" default:""`
	Prefix string `koanf:"prefix" desc:"prefix to store item in category" default:""`
}

type Browser struct {
	Name    string `koanf:"name" desc:"name of the browser" default:""`
	Command string `koanf:"command" desc:"command to launch the browser" default:""`
}

const (
	StateCreating = "creating"
	StateNormal   = "normal"
)

const (
	ActionSave           = "save"
	ActionOpen           = "open"
	ActionDelete         = "delete"
	ActionChangeCategory = "change_category"
	ActionChangeBrowser  = "change_browser"
	ActionImport         = "import"
)

type Bookmark struct {
	URL         string
	Description string
	Category    string
	Browser     string
	CreatedAt   time.Time
	Imported    bool
}

func (b Bookmark) toCSVRow() string {
	created := b.CreatedAt.Format(time.RFC1123Z)
	return fmt.Sprintf("%s;%s;%s;%s;%s;%t", b.URL, b.Description, b.Category, b.Browser, created, b.Imported)
}

func (b *Bookmark) fromCSVRow(row string) error {
	parts := strings.Split(row, ";")
	if len(parts) < 4 {
		return fmt.Errorf("invalid CSV row format")
	}

	b.URL = parts[0]
	b.Description = parts[1]
	b.Category = parts[2]

	if len(parts) >= 5 {
		b.Browser = parts[3]
		t, err := time.Parse(time.RFC1123Z, parts[4])
		if err != nil {
			slog.Error(Name, "timeparse", err)
			b.CreatedAt = time.Now()
		} else {
			b.CreatedAt = t
		}
	} else {
		t, err := time.Parse(time.RFC1123Z, parts[3])
		if err != nil {
			slog.Error(Name, "timeparse", err)
			b.CreatedAt = time.Now()
		} else {
			b.CreatedAt = t
		}
	}

	if len(parts) >= 6 {
		b.Imported, _ = strconv.ParseBool(parts[5])
	}

	return nil
}

func (b *Bookmark) fromQuery(query string) {
	query = strings.TrimSpace(strings.TrimPrefix(query, config.CreatePrefix))
	query = strings.TrimSpace(strings.TrimPrefix(query, ":"))

	category := ""

	for _, v := range config.Categories {
		if strings.HasPrefix(query, v.Prefix) {
			category = v.Name
			query = strings.TrimPrefix(query, v.Prefix)
		}
	}

	b.Category = category

	parts := strings.Fields(query)
	if len(parts) == 0 {
		return
	}

	b.URL = parts[0]
	if !strings.HasPrefix(b.URL, "http://") && !strings.HasPrefix(b.URL, "https://") {
		b.URL = "https://" + b.URL
	}

	if len(parts) > 1 {
		b.Description = strings.Join(parts[1:], " ")
	} else {
		b.Description = b.URL
	}
	b.CreatedAt = time.Now()
}

func saveBookmarks() {
	f := common.CacheFile(fmt.Sprintf("%s.csv", Name))

	if config.Location != "" {
		f = filepath.Join(config.Location, fmt.Sprintf("%s.csv", Name))
	}

	err := os.MkdirAll(filepath.Dir(f), 0o755)
	if err != nil {
		slog.Error(Name, "mkdirall", err)
		return
	}

	file, err := os.Create(f)
	if err != nil {
		slog.Error(Name, "createfile", err)
		return
	}
	defer file.Close()

	lines := []string{"url;description;category;browser;created_at;imported"}

	for _, b := range bookmarks {
		lines = append(lines, b.toCSVRow())
	}

	content := strings.Join(lines, "\n")
	_, err = file.WriteString(content)
	if err != nil {
		slog.Error(Name, "writefile", err)
	}

	if w != nil {
		go common.GitPush(Name, "bookmarks.csv", w, r)
	}
}

func loadBookmarks() {
	file := common.CacheFile(fmt.Sprintf("%s.csv", Name))

	if config.Location != "" {
		file = filepath.Join(config.Location, fmt.Sprintf("%s.csv", Name))
	}

	if !common.FileExists(file) {
		return
	}

	data, err := os.ReadFile(file)
	if err != nil {
		slog.Error(Name, "readfile", err)
		return
	}

	first := false
	for line := range strings.Lines(string(data)) {
		if !first {
			first = true
			continue
		}

		if strings.TrimSpace(line) == "" {
			continue
		}

		b := Bookmark{}
		if err := b.fromCSVRow(line); err != nil {
			slog.Error(Name, "parserow", err)
			continue
		}

		bookmarks = append(bookmarks, b)
	}
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "user-bookmarks",
			MinScore: 100,
		},
		CreatePrefix:       "",
		Location:           "",
		SetBrowserOnImport: false,
	}

	common.LoadConfig(Name, config)

	if strings.HasPrefix(config.Location, "https://") {
		loc, wt, re := common.SetupGit(Name, config.Location)
		if loc != "" {
			config.Location = loc
		}

		if wt == nil || re == nil {
			config.Location = ""
			slog.Error(Name, "error", "couldn't setup git, falling back to default")
		}

		w = wt
		r = re
	}

	loadBookmarks()
}

func Available() bool {
	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Activate(identifier, action string, _ string, args string) {
	if action == ActionImport {
		importBrowserBookmarks()
		return
	}

	if after, ok := strings.CutPrefix(identifier, "CREATE:"); ok {
		if after != "" {
			store(after)
		}

		return
	}

	i, err := strconv.Atoi(identifier)
	if err != nil {
		slog.Error(Name, "activate", fmt.Sprintf("invalid identifier: %s", identifier))
		return
	}

	switch action {
	case ActionSave:
		return
	case ActionChangeCategory:
		bookmarks[i].Imported = false
		currentCategory := bookmarks[i].Category
		nextCategory := ""

		if len(config.Categories) > 0 {
			if currentCategory == "" {
				nextCategory = config.Categories[0].Name
			} else {
				for idx, cat := range config.Categories {
					if cat.Name == currentCategory {
						if idx+1 < len(config.Categories) {
							nextCategory = config.Categories[idx+1].Name
						}
						break
					}
				}
			}
		}

		bookmarks[i].Category = nextCategory
	case ActionChangeBrowser:
		bookmarks[i].Imported = false
		currentBrowser := bookmarks[i].Browser
		nextBrowser := ""

		if len(config.Browsers) > 0 {
			if currentBrowser == "" {
				nextBrowser = config.Browsers[0].Name
			} else {
				for idx, browser := range config.Browsers {
					if browser.Name == currentBrowser {
						if idx+1 < len(config.Browsers) {
							nextBrowser = config.Browsers[idx+1].Name
						}
						break
					}
				}
			}
		}

		bookmarks[i].Browser = nextBrowser
	case ActionDelete:
		bookmarks = append(bookmarks[:i], bookmarks[i+1:]...)
	case ActionOpen, "":
		command := "xdg-open %VALUE%"

		if bookmarks[i].Browser != "" {
			for _, browser := range config.Browsers {
				if browser.Name == bookmarks[i].Browser {
					command = browser.Command
					break
				}
			}
		}

		if strings.Contains(command, "%VALUE%") {
			command = strings.ReplaceAll(command, "%VALUE%", bookmarks[i].URL)
		} else {
			command = fmt.Sprintf("%s %s", command, bookmarks[i].URL)
		}

		cmd := exec.Command("sh", "-c", command)
		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "open", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}
		return
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}

	saveBookmarks()
}

func store(query string) {
	b := Bookmark{}
	b.fromQuery(query)
	bookmarks = append([]Bookmark{b}, bookmarks...)

	saveBookmarks()
}

type browserInfo struct {
	name        string
	browserType string
	path        string
}

func normalizeURL(url string) string {
	url = strings.TrimSpace(url)
	if after, found := strings.CutPrefix(url, "http://"); found {
		url = "https://" + after
	}
	url = strings.TrimSuffix(url, "/")
	return url
}

func discoverBrowsers() []browserInfo {
	browsers := []browserInfo{}

	cmd := exec.Command("sh", "-c", "find ~/.config ~/.mozilla ~/.zen ~/.librewolf ~/.waterfox ~/.floorp -name 'Bookmarks' -o -name 'places.sqlite' 2>/dev/null")
	out, _ := cmd.Output()

	chromiumBrowserNames := map[string]string{
		"google-chrome":               "Chrome",
		"chromium":                    "Chromium",
		"BraveSoftware/Brave-Browser": "Brave",
		"brave-browser":               "Brave",
		"microsoft-edge":              "Edge",
		"opera":                       "Opera",
		"vivaldi":                     "Vivaldi",
		"net.imput.helium":            "Helium",
	}

	firefoxVariants := map[string]string{
		".zen/":       "Zen",
		".librewolf/": "LibreWolf",
		".waterfox/":  "Waterfox",
		".floorp/":    "Floorp",
	}

	for line := range strings.Lines(string(out)) {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}

		if strings.HasSuffix(path, "/Bookmarks") {
			for baseName, displayName := range chromiumBrowserNames {
				if strings.Contains(path, ".config/"+baseName+"/") {
					browsers = append(browsers, browserInfo{
						name:        displayName,
						browserType: "chromium",
						path:        path,
					})
					break
				}
			}
		} else if strings.HasSuffix(path, "/places.sqlite") {
			if strings.Contains(path, ".mozilla/firefox/") {
				browserName := "Firefox"
				if strings.Contains(path, "dev-edition-default") {
					browserName = "Firefox Developer"
				}
				browsers = append(browsers, browserInfo{
					name:        browserName,
					browserType: "firefox",
					path:        path,
				})
			} else {
				for pattern, name := range firefoxVariants {
					if strings.Contains(path, pattern) {
						browsers = append(browsers, browserInfo{
							name:        name,
							browserType: "firefox",
							path:        path,
						})
						break
					}
				}
			}
		}
	}

	return browsers
}

func readChromiumBookmarks(path string) map[string]Bookmark {
	bookmarkMap := make(map[string]Bookmark)

	cmd := exec.Command("sh", "-c", fmt.Sprintf(`jq -r '.roots | .. | objects | select(.type == "url") | "\(.name)|||\(.url)"' "%s" 2>/dev/null`, path))
	out, err := cmd.Output()
	if err != nil {
		slog.Error(Name, "jq", err)
		return bookmarkMap
	}

	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|||", 2)
		if len(parts) == 2 {
			title := strings.TrimSpace(parts[0])
			url := strings.TrimSpace(parts[1])
			normalizedURL := normalizeURL(url)
			if normalizedURL != "" && title != "" {
				bookmarkMap[normalizedURL] = Bookmark{
					URL:         url,
					Description: title,
					CreatedAt:   time.Now(),
					Imported:    true,
				}
			}
		}
	}

	return bookmarkMap
}

func readFirefoxBookmarks(path string) map[string]Bookmark {
	bookmarkMap := make(map[string]Bookmark)

	escapedPath := strings.ReplaceAll(path, " ", "%20")
	cmd := exec.Command("sh", "-c", fmt.Sprintf(`sqlite3 -separator "|||" "file:%s?immutable=1" "SELECT mb.title, mp.url FROM moz_bookmarks mb JOIN moz_places mp ON mb.fk = mp.id WHERE mb.type = 1 AND LENGTH(mb.title) > 0" 2>/dev/null`, escapedPath))
	out, err := cmd.Output()
	if err != nil {
		slog.Error(Name, "sqlite3", err)
		return bookmarkMap
	}

	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|||", 2)
		if len(parts) == 2 {
			title := strings.TrimSpace(parts[0])
			url := strings.TrimSpace(parts[1])
			normalizedURL := normalizeURL(url)
			if normalizedURL != "" && title != "" {
				bookmarkMap[normalizedURL] = Bookmark{
					URL:         url,
					Description: title,
					CreatedAt:   time.Now(),
					Imported:    true,
				}
			}
		}
	}

	return bookmarkMap
}

func importBrowserBookmarks() {
	existingURLs := make(map[string]bool)
	for _, b := range bookmarks {
		existingURLs[normalizeURL(b.URL)] = true
	}

	browsers := discoverBrowsers()
	imported := 0

	for _, browser := range browsers {
		var browserBookmarks map[string]Bookmark

		switch browser.browserType {
		case "chromium":
			browserBookmarks = readChromiumBookmarks(browser.path)
		case "firefox":
			browserBookmarks = readFirefoxBookmarks(browser.path)
		}

		for normalizedURL, bookmark := range browserBookmarks {
			if !existingURLs[normalizedURL] {
				if config.SetBrowserOnImport {
					bookmark.Browser = browser.name
				}
				bookmarks = append(bookmarks, bookmark)
				existingURLs[normalizedURL] = true
				imported++
			}
		}
	}

	if imported > 0 {
		saveBookmarks()
		slog.Info(Name, "imported", fmt.Sprintf("%d bookmarks", imported))
	} else {
		slog.Info(Name, "imported", "no new bookmarks found")
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}
	var highestScore int32

	var category Category

	for _, v := range config.Categories {
		if strings.HasPrefix(query, v.Prefix) {
			category = v
		}
	}

	for i, b := range bookmarks {
		if category.Name != "" && b.Category != category.Name {
			continue
		}

		e := &pb.QueryResponse_Item{}

		e.Score = 999_999 - int32(i)

		searchText := b.URL + " " + b.Description

		e.Provider = Name
		e.Identifier = fmt.Sprintf("%d", i)
		e.Text = b.Description
		e.Subtext = b.URL
		e.Actions = []string{ActionOpen, ActionDelete}

		if len(config.Browsers) > 1 {
			e.Actions = append(e.Actions, ActionChangeBrowser)
		}

		if len(config.Categories) > 1 {
			e.Actions = append(e.Actions, ActionChangeCategory)
		}

		e.State = []string{StateNormal}
		e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{}

		if e.Text == e.Subtext {
			e.Subtext = ""
		}

		if query != "" {
			e.Score, e.Fuzzyinfo.Positions, e.Fuzzyinfo.Start = common.FuzzyScore(query, searchText, exact)
		}

		if e.Score > highestScore {
			highestScore = e.Score
		}

		if b.Browser != "" {
			if e.Subtext != "" {
				e.Subtext = fmt.Sprintf("%s, %s", e.Subtext, b.Browser)
			} else {
				e.Subtext = b.Browser
			}
		}

		if b.Category != "" {
			if e.Subtext != "" {
				e.Subtext = fmt.Sprintf("%s, %s", e.Subtext, b.Category)
			} else {
				e.Subtext = b.Category
			}
		}

		entries = append(entries, e)
	}

	if strings.TrimSpace(strings.TrimPrefix(query, category.Prefix)) != "" {
		if (config.CreatePrefix != "" && strings.HasPrefix(query, config.CreatePrefix)) || highestScore < config.MinScore {
			b := Bookmark{}
			b.fromQuery(query)

			e := &pb.QueryResponse_Item{}
			e.Score = 3_000_000
			e.Provider = Name
			e.Identifier = fmt.Sprintf("CREATE:%s", query)
			e.Icon = "list-add"
			e.Text = b.Description
			e.Subtext = b.URL
			e.Actions = []string{ActionSave}
			e.State = []string{StateCreating}

			entries = append(entries, e)
		}
	}

	return entries
}

func Icon() string {
	return config.Icon
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{
		Actions: []string{ActionImport},
	}
}
