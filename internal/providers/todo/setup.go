package main

import (
	"bytes"
	_ "embed"
	"encoding/gob"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"github.com/go-git/go-git/v6"
	"github.com/sho0pi/naturaltime"
)

var (
	Name       = "todo"
	NamePretty = "Todo List"
	config     *Config
	items      = []Item{}
	parser     *naturaltime.Parser
	r          *git.Repository
	w          *git.Worktree
)

//go:embed README.md
var readme string

type Config struct {
	common.Config     `koanf:",squash"`
	CreatePrefix      string     `koanf:"create_prefix" desc:"prefix used in order to create a new item. will otherwise be based on matches (min_score)." default:""`
	UrgentTimeFrame   int        `koanf:"urgent_time_frame" desc:"items that have a due time within this period will be marked as urgent" default:"10"`
	DuckPlayerVolumes bool       `koanf:"duck_player_volumes" desc:"lowers volume of players when notifying, slowly raises volumes again" default:"true"`
	Categories        []Category `koanf:"categories" desc:"categories" default:""`
	Location          string     `koanf:"location" desc:"location of the CSV file" default:"elephant cache dir"`
	TimeFormat        string     `koanf:"time_format" desc:"format of the time. Look at https://go.dev/src/time/format.go for the layout." default:"02-Jan 15:04"`
	Notification      `koanf:",squash"`
}

type Category struct {
	Name   string `koanf:"name" desc:"name for category" default:""`
	Prefix string `koanf:"prefix" desc:"prefix to store item in category" default:""`
}

type Notification struct {
	Title string `koanf:"title" desc:"title of the notification" default:"Task Due"`
	Body  string `koanf:"body" desc:"body of the notification" default:"%TASK%"`
}

const (
	StatePending  = "pending"
	StateActive   = "active"
	StateDone     = "done"
	StateCreating = "creating"
	StateUrgent   = "urgent"
)

const (
	ActionSave           = "save"
	ActionChangeCategory = "change_category"
	ActionDelete         = "delete"
	ActionMarkDone       = "done"
	ActionMarkActive     = "active"
	ActionMarkInactive   = "inactive"
	ActionClear          = "clear"
)

const (
	UrgencyNormal   = "normal"
	UrgencyCritical = "critical"
)

type Item struct {
	Text      string
	Scheduled time.Time
	Started   time.Time
	Finished  time.Time
	Category  string
	State     string
	Urgency   string
	Notified  bool
}

func (i Item) toCSVRow() string {
	sched := i.Scheduled.Format(time.RFC1123Z)
	star := i.Scheduled.Format(time.RFC1123Z)
	fin := i.Scheduled.Format(time.RFC1123Z)

	return fmt.Sprintf("%s;%s;%s;%s;%t;%s;%s;%s", i.Category, i.Text, i.State, i.Urgency, i.Notified, sched, star, fin)
}

func saveItems() {
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
	}
	defer file.Close()

	c := []string{"category;text;state;urgency;notified;scheduled;start;finish"}

	for _, v := range items {
		c = append(c, v.toCSVRow())
	}

	content := strings.Join(c, "\n")
	_, err = file.WriteString(content)
	if err != nil {
		slog.Error(Name, "writefile", err)
	}

	if w != nil {
		go common.GitPush(Name, "todo.csv", w, r)
	}
}

func (i *Item) fromQuery(query string) {
	query = strings.TrimSpace(strings.TrimPrefix(query, config.CreatePrefix))

	category := ""

	for _, v := range config.Categories {
		if strings.HasPrefix(query, v.Prefix) {
			category = v.Name
			query = strings.TrimPrefix(query, v.Prefix)
		}
	}

	i.Urgency = UrgencyNormal
	i.Category = category

	if strings.HasSuffix(query, "!") {
		query = strings.TrimSuffix(query, "!")
		i.Urgency = UrgencyCritical
	}

	splits := strings.Fields(query)

	for k := range splits {
		date, err := parser.ParseDate(strings.Join(splits[:k], " "), time.Now())
		if date != nil && err == nil {
			i.Scheduled = *date
			i.Text = strings.Join(splits[k:], " ")
			break
		}

		i.Text = strings.Join(splits, " ")
	}

	i.Text = strings.TrimSpace(i.Text)
}

func Setup() {
	var err error
	parser, err = naturaltime.New()
	if err != nil {
		panic(err)
	}

	config = &Config{
		Config: common.Config{
			Icon:     "checkbox-checked",
			MinScore: 100,
		},
		CreatePrefix:      "",
		UrgentTimeFrame:   10,
		DuckPlayerVolumes: true,
		Location:          "",
		TimeFormat:        "02-Jan 15:04",
		Notification: Notification{
			Title: "Task Due",
			Body:  "%TASK%",
		},
	}

	common.LoadConfig(Name, config)

	if strings.HasPrefix(config.Location, "https://") {
		loc, wt, re := common.SetupGit(Name, config.Location)
		if loc != "" {
			config.Location = loc
		}

		if wt == nil || re == nil {
			config.Location = ""
			slog.Error(Name, "error", "couldn't setup git.")
			return
		}

		w = wt
		r = re
	}

	if !migrateGOBtoCSV() {
		loadItems()
	}

	go notify()
}

func Available() bool {
	return true
}

func notify() {
	for {
		now := time.Now().Truncate(time.Minute)
		nextMinute := now.Add(time.Minute)
		time.Sleep(time.Until(nextMinute))

		now = time.Now().Truncate(time.Minute)

		hasNotification := false

		for i, v := range items {
			if v.Notified || v.Scheduled.IsZero() || v.State != StatePending {
				continue
			}

			if v.Scheduled.Equal(now) || v.Scheduled.Before(now) {

				body := strings.ReplaceAll(config.Body, "%TASK%", v.Text)
				cmd := exec.Command("notify-send", "-a", "elephant", "-u", v.Urgency, config.Title, body)

				err := cmd.Start()
				if err != nil {
					slog.Error(Name, "notify", err)
				} else {
					if config.DuckPlayerVolumes {
						duckPlayers()
					}

					items[i].Notified = true
					hasNotification = true

					go func() {
						cmd.Wait()
					}()
				}
			}
		}

		if hasNotification {
			saveItems()
		}
	}
}

func duckPlayers() {
	reduce := exec.Command("playerctl", "--all-players", "volume", "0.1")
	reduce.Run()

	initial := 0.1

	for initial < 1.0 {
		time.Sleep(time.Millisecond * 200)
		initial += 0.1
		raise := exec.Command("playerctl", "--all-players", "volume", fmt.Sprintf("%f", initial))
		raise.Run()
	}
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Activate(identifier, action string, query string, args string) {
	if after, ok := strings.CutPrefix(identifier, "CREATE:"); ok {
		if after != "" {
			store(after)
		}

		return
	}

	i, _ := strconv.Atoi(identifier)

	switch action {
	case ActionChangeCategory:
		var category Category

		for _, v := range config.Categories {
			if args == v.Prefix {
				category = v
			}
		}

		items[i].Category = category.Name
	case ActionDelete:
		items = append(items[:i], items[i+1:]...)
	case ActionMarkActive:
		items[i].State = StateActive
		items[i].Started = time.Now()
	case ActionMarkInactive:
		items[i].State = StatePending
		items[i].Started = time.Time{}
	case ActionMarkDone:
		if items[i].State == StateDone {
			items[i].State = StatePending
			items[i].Finished = time.Time{}
		} else {
			items[i].State = StateDone
			items[i].Finished = time.Now()
		}
	case ActionClear:
		n := 0
		for _, x := range items {
			if x.State != StateDone {
				items[n] = x
				n++
			}
		}
		items = items[:n]
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}

	saveItems()
}

func store(query string) {
	i := Item{}
	i.fromQuery(query)
	i.State = StatePending

	items = append(items, i)

	saveItems()
}

func migrateGOBtoCSV() bool {
	file := common.CacheFile(fmt.Sprintf("%s.gob", Name))

	if common.FileExists(file) {
		f, err := os.ReadFile(file)
		if err != nil {
			slog.Error(Name, "itemsread", err)
		} else {
			decoder := gob.NewDecoder(bytes.NewReader(f))

			err = decoder.Decode(&items)
			if err != nil {
				slog.Error(Name, "decoding", err)
			}
		}

		saveItems()

		os.Remove(file)
	}

	return false
}

func loadItems() {
	file := common.CacheFile(fmt.Sprintf("%s.csv", Name))

	if config.Location != "" {
		file = filepath.Join(config.Location, fmt.Sprintf("%s.csv", Name))
	}

	if common.FileExists(file) {
		f, err := os.ReadFile(file)
		if err != nil {
			slog.Error(Name, "itemsread", err)
		} else {
			first := false

			for l := range strings.Lines(string(f)) {
				if !first {
					first = true
					continue
				}

				d := strings.Split(l, ";")

				i := Item{}
				i.Category = d[0]
				i.Text = d[1]
				i.State = d[2]
				i.Urgency = d[3]
				i.Notified = d[4] == "true"

				t, err := time.Parse(time.RFC1123Z, d[5])
				if err != nil {
					slog.Error(Name, "timeparse", err, "field", "scheduled")
				} else {
					i.Scheduled = t
				}

				t, _ = time.Parse(time.RFC1123Z, d[6])
				if err != nil {
					slog.Error(Name, "timeparse", err, "field", "started")
				} else {
					i.Started = t
				}

				t, _ = time.Parse(time.RFC1123Z, d[7])
				if err != nil {
					slog.Error(Name, "timeparse", err, "field", "finished")
				} else {
					i.Finished = t
				}

				items = append(items, i)
			}
		}
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}
	urgent := time.Now().Add(time.Duration(config.UrgentTimeFrame) * time.Minute)

	var highestScore int32

	var category Category

	for _, v := range config.Categories {
		if strings.HasPrefix(query, v.Prefix) {
			category = v
		}
	}

	for i, v := range items {
		if category.Name != "" && v.Category != category.Name {
			continue
		}

		e := &pb.QueryResponse_Item{}

		if v.State == StateDone {
			e.Score = 100_000 - int32(i)
		} else {
			e.Score = 999_999 - int32(i)
		}

		actions := []string{ActionDelete}

		switch v.State {
		case StateActive:
			actions = []string{ActionDelete, ActionMarkDone, ActionMarkInactive}
		case StateDone:
			actions = []string{ActionDelete, ActionMarkInactive}
		case StatePending, StateUrgent:
			actions = []string{ActionDelete, ActionMarkDone, ActionMarkActive}
		case StateCreating:
			actions = []string{ActionSave}
		}

		actions = append(actions, ActionChangeCategory)

		e.Provider = Name
		e.Identifier = fmt.Sprintf("%d", i)
		e.Text = v.Text
		e.Actions = actions
		e.State = []string{v.State}
		e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{}

		if !v.Finished.IsZero() {
			if !v.Started.IsZero() {
				duration := v.Finished.Sub(v.Started)
				hours := int(duration.Hours())
				minutes := int(duration.Minutes()) % 60

				e.Subtext = fmt.Sprintf("Started: %s, Finished: %s, Duration: %s", v.Started.Format(config.TimeFormat), v.Finished.Format(config.TimeFormat), fmt.Sprintf("%02d:%02d", hours, minutes))
			} else {
				e.Subtext = fmt.Sprintf("Finished: %s", v.Finished.Format(config.TimeFormat))
			}
		} else if !v.Started.IsZero() {
			duration := time.Since(v.Started)
			hours := int(duration.Hours())
			minutes := int(duration.Minutes()) % 60

			e.Subtext = fmt.Sprintf("Started: %s, Ongoing: %s", v.Started.Format(config.TimeFormat), fmt.Sprintf("%02d:%02d", hours, minutes))
		} else if !v.Scheduled.IsZero() {
			e.Subtext = fmt.Sprintf("At: %s", v.Scheduled.Format(config.TimeFormat))
		}

		if query != "" {
			e.Score, e.Fuzzyinfo.Positions, e.Fuzzyinfo.Start = common.FuzzyScore(query, e.Text, exact)
		}

		if !v.Scheduled.IsZero() && v.Scheduled.Before(urgent) && v.State != StateDone && v.State != StateActive {
			e.State = []string{StateUrgent}
		}

		if slices.Contains(e.State, StateActive) && query == "" {
			e.Score = 1_000_001
		}

		if slices.Contains(e.State, StateUrgent) && query == "" {
			diff := time.Since(v.Scheduled).Minutes()
			e.Score = 2_000_000 + int32(diff)
		}

		if e.Score > highestScore {
			highestScore = e.Score
		}

		e.State = append(e.State, v.Urgency)

		if v.Category != "" {
			if e.Subtext != "" {
				e.Subtext = fmt.Sprintf("%s, %s", e.Subtext, v.Category)
			} else {
				e.Subtext = v.Category
			}
		}

		entries = append(entries, e)
	}

	if strings.TrimSpace(strings.TrimPrefix(query, category.Prefix)) != "" {
		if (config.CreatePrefix != "" && strings.HasPrefix(query, config.CreatePrefix)) || highestScore < config.MinScore {
			i := Item{}
			i.fromQuery(query)

			e := &pb.QueryResponse_Item{}
			e.Score = 3_000_000
			e.Provider = Name
			e.Identifier = fmt.Sprintf("CREATE:%s", query)
			e.Icon = "list-add"
			e.Text = i.Text
			e.Actions = []string{ActionSave}
			e.State = []string{StateCreating}

			if !i.Scheduled.IsZero() {
				e.Subtext = i.Scheduled.Format(config.TimeFormat)
			}

			entries = append(entries, e)
		}
	}

	return entries
}

func Icon() string {
	return config.Icon
}

func State(provider string) *pb.ProviderStateResponse {
	for _, v := range items {
		if v.State == StateDone {
			return &pb.ProviderStateResponse{
				States:  []string{"hasfinished"},
				Actions: []string{ActionClear},
			}
		}
	}
	return &pb.ProviderStateResponse{}
}
