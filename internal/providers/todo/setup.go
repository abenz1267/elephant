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
	"sync"
	"time"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
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
	isGit      bool
	creating   bool
)

//go:embed README.md
var readme string

type Config struct {
	common.Config     `koanf:",squash"`
	UrgentTimeFrame   int        `koanf:"urgent_time_frame" desc:"items that have a due time within this period will be marked as urgent" default:"10"`
	DuckPlayerVolumes bool       `koanf:"duck_player_volumes" desc:"lowers volume of players when notifying, slowly raises volumes again" default:"true"`
	ShowCreationTime  bool       `koanf:"show_creation_time" desc:"displays the creatin time if no other time info is available" default:"true"`
	Categories        []Category `koanf:"categories" desc:"categories" default:""`
	Location          string     `koanf:"location" desc:"location of the CSV file" default:"elephant cache dir"`
	TimeFormat        string     `koanf:"time_format" desc:"format of the time. Look at https://go.dev/src/time/format.go for the layout." default:"02-Jan 15:04"`
	Notification      `koanf:",squash"`
	w                 *git.Worktree
	r                 *git.Repository
}

func (config *Config) SetLocation(val string) {
	config.Location = val
}

func (config *Config) URL() string {
	return config.Location
}

func (config *Config) SetWorktree(val *git.Worktree) {
	config.w = val
}

func (config *Config) SetRepository(val *git.Repository) {
	config.r = val
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
	ActionSaveNext       = "save_next"
	ActionChangeCategory = "change_category"
	ActionDelete         = "delete"
	ActionMarkDone       = "done"
	ActionMarkActive     = "active"
	ActionMarkInactive   = "inactive"
	ActionClear          = "clear"
	ActionCreate         = "create"
	ActionSearch         = "search"
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
	Created   time.Time
	Category  string
	State     string
	Urgency   string
	Notified  bool
}

func (i Item) toCSVRow() string {
	sched := i.Scheduled.Format(time.RFC1123Z)
	star := i.Started.Format(time.RFC1123Z)
	fin := i.Finished.Format(time.RFC1123Z)
	created := i.Created.Format(time.RFC1123Z)

	return fmt.Sprintf("%s;%s;%s;%s;%t;%s;%s;%s;%s", i.Category, i.Text, i.State, i.Urgency, i.Notified, sched, star, fin, created)
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

	c := []string{"category;text;state;urgency;notified;scheduled;start;finish;created"}

	for _, v := range items {
		c = append(c, v.toCSVRow())
	}

	content := strings.Join(c, "\n")
	_, err = file.WriteString(content)
	if err != nil {
		slog.Error(Name, "writefile", err)
	}

	if config.w != nil {
		go common.GitPush(Name, "todo.csv", config.w, config.r)
	}
}

func (i *Item) fromQuery(query string) {
	category := ""

	for _, v := range config.Categories {
		if strings.HasPrefix(query, v.Prefix) {
			category = v.Name
			query = strings.TrimPrefix(query, v.Prefix)
		}
	}

	i.Urgency = UrgencyNormal
	i.Category = category

	if after, ok := strings.CutSuffix(query, "!"); ok {
		query = after
		i.Urgency = UrgencyCritical
	}

	splits := strings.SplitN(query, ">", 2)

	if len(splits) == 2 {
		date, err := parser.ParseDate(splits[0], time.Now())

		if date != nil && err == nil {
			now := time.Now()

			if date.Minute() == now.Minute() && date.Day() == now.Day() {
				i.Scheduled = endOfDay(*date)
			} else {
				i.Scheduled = *date
			}
		}

		i.Text = splits[1]
	} else {
		i.Text = splits[0]
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
			MinScore: 20,
		},
		UrgentTimeFrame:   10,
		DuckPlayerVolumes: true,
		Location:          "",
		TimeFormat:        "02-Jan 15:04",
		ShowCreationTime:  true,
		Notification: Notification{
			Title: "Task Due",
			Body:  "%TASK%",
		},
	}

	common.LoadConfig(Name, config)

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	if strings.HasPrefix(config.Location, "https://") {
		isGit = true
	}

	ec := common.GetElephantConfig()

	if !ec.GitOnDemand && isGit {
		common.SetupGit(Name, config)
		loadItems()
	}

	if !isGit {
		if !migrateGOBtoCSV() {
			loadItems()
		}
	}

	go notify()
}

func Available() bool {
	return true
}

func notify() {
	for {
		time.Sleep(time.Second * 1)

		now := time.Now()

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

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	i, _ := strconv.Atoi(identifier)

	switch action {
	case ActionSearch:
		creating = false
		return
	case ActionCreate:
		creating = true
		return
	case ActionChangeCategory:
		currentCategory := items[i].Category
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

		items[i].Category = nextCategory

		updated := itemToEntry(time.Now(), i, items[i])
		handlers.UpdateItem(format, query, conn, updated)
	case ActionDelete:
		items = append(items[:i], items[i+1:]...)
	case ActionMarkActive:
		items[i].State = StateActive
		items[i].Started = time.Now()

		updated := itemToEntry(time.Now(), i, items[i])
		handlers.UpdateItem(format, query, conn, updated)
	case ActionMarkInactive:
		items[i].State = StatePending
		items[i].Started = time.Time{}

		updated := itemToEntry(time.Now(), i, items[i])
		handlers.UpdateItem(format, query, conn, updated)
	case ActionMarkDone:
		if items[i].State == StateDone {
			items[i].State = StatePending
			items[i].Finished = time.Time{}
		} else {
			items[i].State = StateDone
			items[i].Finished = time.Now()
		}

		updated := itemToEntry(time.Now(), i, items[i])
		handlers.UpdateItem(format, query, conn, updated)
	case ActionClear:
		n := 0
		for _, x := range items {
			if x.State != StateDone {
				items[n] = x
				n++
			}
		}
		items = items[:n]
	case ActionSave, ActionSaveNext:
		if action == ActionSave {
			creating = false
		}

		createNew(identifier)
		return
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}

	saveItems()
}

func createNew(identifier string) {
	if after, ok := strings.CutPrefix(identifier, "CREATE:"); ok {
		store(after)

		return
	}
}

func store(query string) {
	i := Item{}
	i.fromQuery(query)
	i.State = StatePending
	i.Created = time.Now()

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

var (
	loadMu sync.Mutex
	loaded bool
)

func loadItems() {
	loadMu.Lock()
	defer loadMu.Unlock()

	if loaded {
		return
	}

	file := common.CacheFile(fmt.Sprintf("%s.csv", Name))
	items = []Item{}

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

				t, err = time.Parse(time.RFC1123Z, d[6])
				if err != nil {
					slog.Error(Name, "timeparse", err, "field", "started")
				} else {
					i.Started = t
				}

				t, err = time.Parse(time.RFC1123Z, d[7])
				if err != nil {
					slog.Error(Name, "timeparse", err, "field", "finished")
				} else {
					i.Finished = t
				}

				if len(d) == 9 {
					t, err = time.Parse(time.RFC1123Z, strings.TrimSpace(d[8]))
					if err != nil {
						slog.Error(Name, "timeparse", err, "field", "created")
					} else {
						i.Created = t
					}
				}

				items = append(items, i)
			}
		}
	}

	loaded = true
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	if isGit && config.r == nil {
		common.SetupGit(Name, config)
		loadItems()
	}

	origQ := query
	entries := []*pb.QueryResponse_Item{}
	urgent := time.Now().Add(time.Duration(config.UrgentTimeFrame) * time.Minute)

	var highestScore int32

	var category Category

	for _, v := range config.Categories {
		if strings.HasPrefix(query, v.Prefix) {
			category = v
			query = strings.TrimPrefix(query, v.Prefix)
		}
	}

	var date *time.Time

	splits := strings.SplitN(query, ">", 2)
	fastCreate := len(splits) == 2 && !creating

	if !creating && !fastCreate {
		date, _ = parser.ParseDate(query, time.Now())
		if date != nil {
			query = ""
		}
	}

	if !creating && !fastCreate {
		slices.SortFunc(items, func(a, b Item) int {
			if date != nil {
				if isSameDay(date, &a.Scheduled) && !isSameDay(date, &b.Scheduled) {
					return -1
				}

				if isSameDay(date, &b.Scheduled) && !isSameDay(date, &a.Scheduled) {
					return 1
				}
			}

			if !a.Scheduled.IsZero() && b.Scheduled.IsZero() {
				return -1
			}

			if a.Scheduled.IsZero() && !b.Scheduled.IsZero() {
				return 1
			}

			if !a.Created.IsZero() && b.Created.IsZero() {
				return -1
			}

			if a.Created.IsZero() && !b.Created.IsZero() {
				return 1
			}

			if !a.Created.IsZero() && !b.Created.IsZero() {
				return a.Created.Compare(b.Created)
			}

			return 0
		})

		for i, v := range items {
			if category.Name != "" && v.Category != category.Name {
				continue
			}

			e := itemToEntry(urgent, i, v)

			if query != "" {
				e.Score, e.Fuzzyinfo.Positions, e.Fuzzyinfo.Start = common.FuzzyScore(query, e.Text, exact)
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

			if date == nil {
				if query == "" || e.Score > config.MinScore {
					entries = append(entries, e)
				}
			} else if isSameDay(date, &v.Scheduled) {
				entries = append(entries, e)
			}
		}
	} else {
		if strings.TrimSpace(strings.TrimPrefix(query, category.Prefix)) != "" {
			i := Item{}
			i.fromQuery(origQ)

			e := &pb.QueryResponse_Item{}
			e.Score = 3_000_000
			e.Provider = Name
			e.Identifier = fmt.Sprintf("CREATE:%s", origQ)
			e.Icon = "list-add"
			e.Text = i.Text
			e.Actions = []string{ActionSave}
			e.State = []string{StateCreating}

			if !fastCreate {
				e.Actions = append(e.Actions, ActionSaveNext)
			}

			if !i.Scheduled.IsZero() {
				e.Subtext = i.Scheduled.Format(config.TimeFormat)
			}

			if category.Name != "" {
				if e.Subtext != "" {
					e.Subtext = fmt.Sprintf("%s, %s", e.Subtext, category.Name)
				} else {
					e.Subtext = category.Name
				}
			}

			entries = append(entries, e)
		}
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
	states := []string{}
	actions := []string{}

	if creating {
		states = append(states, "creating")
		actions = append(actions, ActionSearch)
	} else {
		states = append(states, "searching")
		actions = append(actions, ActionCreate)
	}

	for _, v := range items {
		if v.State == StateDone {
			return &pb.ProviderStateResponse{
				States:  append([]string{"hasfinished"}, states...),
				Actions: append([]string{ActionClear}, actions...),
			}
		}
	}
	return &pb.ProviderStateResponse{
		States:  states,
		Actions: actions,
	}
}

func itemToEntry(urgent time.Time, i int, v Item) *pb.QueryResponse_Item {
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
	} else if config.ShowCreationTime {
		e.Subtext = fmt.Sprintf("Created: %s", v.Created.Format(config.TimeFormat))
	}

	if !v.Scheduled.IsZero() && v.Scheduled.Before(urgent) && v.State != StateDone && v.State != StateActive {
		e.State = []string{StateUrgent}
	}

	e.State = append(e.State, v.Urgency)

	if v.Category != "" {
		if e.Subtext != "" {
			e.Subtext = fmt.Sprintf("%s, %s", e.Subtext, v.Category)
		} else {
			e.Subtext = v.Category
		}
	}

	return e
}

func isSameDay(t1, t2 *time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func endOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 23, 59, 59, 999999999, t.Location())
}
