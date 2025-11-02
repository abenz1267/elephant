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
)

var (
	Name       = "bookmarks"
	NamePretty = "Bookmarks"
	config     *Config
	bookmarks  = []Bookmark{}
)

//go:embed README.md
var readme string

type Config struct {
	common.Config `koanf:",squash"`
	CreatePrefix  string     `koanf:"create_prefix" desc:"prefix used in order to create a new bookmark. will otherwise be based on matches (min_score)." default:""`
	Location      string     `koanf:"location" desc:"location of the CSV file" default:"elephant cache dir"`
	Categories    []Category `koanf:"categories" desc:"categories" default:""`
}

type Category struct {
	Name   string `koanf:"name" desc:"name for category" default:""`
	Prefix string `koanf:"prefix" desc:"prefix to store item in category" default:""`
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
)

type Bookmark struct {
	URL         string
	Description string
	Category    string
	CreatedAt   time.Time
}

func (b Bookmark) toCSVRow() string {
	created := b.CreatedAt.Format(time.RFC1123Z)
	return fmt.Sprintf("%s;%s;%s;%s", b.URL, b.Description, b.Category, created)
}

func (b *Bookmark) fromCSVRow(row string) error {
	parts := strings.Split(row, ";")
	if len(parts) < 4 {
		return fmt.Errorf("invalid CSV row format")
	}

	b.URL = parts[0]
	b.Description = parts[1]
	b.Category = parts[2]

	t, err := time.Parse(time.RFC1123Z, parts[3])
	if err != nil {
		slog.Error(Name, "timeparse", err)
		b.CreatedAt = time.Now()
	} else {
		b.CreatedAt = t
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

	os.Remove(f)

	file, err := os.Create(f)
	if err != nil {
		slog.Error(Name, "createfile", err)
		return
	}
	defer file.Close()

	lines := []string{"url;description;category;created_at"}

	for _, b := range bookmarks {
		lines = append(lines, b.toCSVRow())
	}

	content := strings.Join(lines, "\n")
	_, err = file.WriteString(content)
	if err != nil {
		slog.Error(Name, "writefile", err)
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
		CreatePrefix: "",
		Location:     "",
	}

	common.LoadConfig(Name, config)
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
	case ActionDelete:
		bookmarks = append(bookmarks[:i], bookmarks[i+1:]...)
	case ActionOpen, "":
		cmd := exec.Command("xdg-open", bookmarks[i].URL)
		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "xdg-open", err)
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
		e.Actions = append(e.Actions, ActionChangeCategory)
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
	return &pb.ProviderStateResponse{}
}
