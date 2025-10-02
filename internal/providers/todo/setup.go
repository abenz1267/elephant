package main

import (
	"bytes"
	_ "embed"
	"encoding/gob"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/common"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var (
	Name       = "todo"
	NamePretty = "Todo List"
	config     *Config
	items      = []Item{}
)

//go:embed README.md
var readme string

type Config struct {
	common.Config     `koanf:",squash"`
	CreatePrefix      string `koanf:"create_prefix" desc:"prefix used in order to create a new item. will otherwise be based on matches (min_score)." default:""`
	UrgentTimeFrame   int    `koanf:"urgent_time_frame" desc:"items that have a due time within this period will be marked as urgent" default:"10"`
	DuckPlayerVolumes bool   `koanf:"duck_player_volumes" desc:"lowers volume of players when notifying, slowly raises volumes again" default:"true"`
	Notification      `koanf:",squash"`
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
	ActionSave       = "save"
	ActionDelete     = "delete"
	ActionMarkDone   = "done"
	ActionMarkActive = "active"
	ActionClear      = "clear"
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
	State     string
	Urgency   string
	Notified  bool
}

func saveItems() {
	var b bytes.Buffer
	encoder := gob.NewEncoder(&b)

	err := encoder.Encode(items)
	if err != nil {
		slog.Error(Name, "saveencode", err)
		return
	}

	err = os.MkdirAll(filepath.Dir(common.CacheFile(fmt.Sprintf("%s.gob", Name))), 0o755)
	if err != nil {
		slog.Error(Name, "savedir", err)
		return
	}

	err = os.WriteFile(common.CacheFile(fmt.Sprintf("%s.gob", Name)), b.Bytes(), 0o600)
	if err != nil {
		slog.Error(Name, "savewrite", err)
	}
}

func (i *Item) fromQuery(query string) {
	query = strings.TrimSpace(strings.TrimPrefix(query, config.CreatePrefix))

	i.Urgency = UrgencyNormal

	if strings.HasSuffix(query, "!") {
		query = strings.TrimSuffix(query, "!")
		i.Urgency = UrgencyCritical
	}

	if strings.HasPrefix(query, "in ") || strings.HasPrefix(query, "at ") {
		splits := strings.SplitN(query, " ", 3)

		switch len(splits) {
		case 3:
			i.Text = splits[2]

			now := time.Now()

			switch splits[0] {
			case "in":
				switch {
				case strings.HasSuffix(splits[1], "s"):
					add := strings.TrimSuffix(splits[1], "s")

					addi, _ := strconv.Atoi(add)
					now = now.Add(time.Duration(addi) * time.Second)
				case strings.HasSuffix(splits[1], "m"):
					add := strings.TrimSuffix(splits[1], "m")

					addi, _ := strconv.Atoi(add)
					now = now.Add(time.Duration(addi) * time.Minute)
					now = now.Truncate(time.Minute)
				case strings.HasSuffix(splits[1], "h"):
					add := strings.TrimSuffix(splits[1], "h")

					addi, _ := strconv.Atoi(add)
					now = now.Add(time.Duration(addi) * time.Hour)
					now = now.Truncate(time.Minute)
				}
			case "at":
				hour := splits[1][:2]
				minute := splits[1][2:]
				houri, _ := strconv.Atoi(hour)
				minutei, _ := strconv.Atoi(minute)

				now = time.Date(now.Year(), now.Month(), now.Day(),
					0, 0, 0, 0, now.Location())
				now = now.Add(time.Duration(houri)*time.Hour +
					time.Duration(minutei)*time.Minute)
			}

			i.Scheduled = now
		}
	} else {
		i.Text = query
	}
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "checkbox-checked",
			MinScore: 50,
		},
		CreatePrefix:      "",
		UrgentTimeFrame:   10,
		DuckPlayerVolumes: true,
		Notification: Notification{
			Title: "Task Due",
			Body:  "%TASK%",
		},
	}

	loadItems()

	common.LoadConfig(Name, config)
	go notify()
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

func Cleanup(qid uint32) {
}

func Activate(qid uint32, identifier, action string, arguments string) {
	if after, ok := strings.CutPrefix(identifier, "CREATE:"); ok {
		if after != "" {
			store(after)
		}

		return
	}

	i, _ := strconv.Atoi(identifier)

	switch action {
	case ActionDelete:
		items = append(items[:i], items[i+1:]...)
	case ActionMarkActive:
		if items[i].State == StateActive {
			items[i].State = StatePending
			items[i].Started = time.Time{}
		} else {
			items[i].State = StateActive
			items[i].Started = time.Now()
		}
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

func loadItems() {
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
	}
}

func Query(qid uint32, iid uint32, query string, single bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}
	urgent := time.Now().Add(time.Duration(config.UrgentTimeFrame) * time.Minute)

	var highestScore int32

	for i, v := range items {
		e := &pb.QueryResponse_Item{}

		if v.State == StateDone {
			e.Score = 100_000 - int32(i)
		} else {
			e.Score = 999_999 - int32(i)
		}

		e.Provider = Name
		e.Identifier = fmt.Sprintf("%d", i)
		e.Text = v.Text
		e.State = []string{v.State}
		e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{}

		if !v.Finished.IsZero() {
			if !v.Started.IsZero() {
				duration := v.Finished.Sub(v.Started)
				hours := int(duration.Hours())
				minutes := int(duration.Minutes()) % 60

				e.Subtext = fmt.Sprintf("Started: %s, Finished: %s, Duration: %s", v.Started.Format("15:04"), v.Finished.Format("15:04"), fmt.Sprintf("%02d:%02d", hours, minutes))
			} else {
				e.Subtext = fmt.Sprintf("Finished: %s", v.Finished.Format("15:04"))
			}
		} else if !v.Started.IsZero() {
			duration := time.Since(v.Started)
			hours := int(duration.Hours())
			minutes := int(duration.Minutes()) % 60

			e.Subtext = fmt.Sprintf("Started: %s, Ongoing: %s", v.Started.Format("15:04"), fmt.Sprintf("%02d:%02d", hours, minutes))
		} else if !v.Scheduled.IsZero() {
			e.Subtext = fmt.Sprintf("At: %s", v.Scheduled.Format("15:04"))
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

		entries = append(entries, e)
	}

	if (config.CreatePrefix != "" && strings.HasPrefix(query, config.CreatePrefix)) || highestScore < config.MinScore {
		i := Item{}
		i.fromQuery(query)

		e := &pb.QueryResponse_Item{}
		e.Score = 3_000_000
		e.Provider = Name
		e.Identifier = fmt.Sprintf("CREATE:%s", query)
		e.Icon = "list-add"
		e.Text = i.Text
		e.State = []string{StateCreating}

		if !i.Scheduled.IsZero() {
			e.Subtext = i.Scheduled.Format(time.TimeOnly)
		}

		entries = append(entries, e)
	}

	return entries
}

func Icon() string {
	return config.Icon
}
