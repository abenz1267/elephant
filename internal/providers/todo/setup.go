package main

import (
	"bytes"
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

	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var (
	Name       = "todo"
	NamePretty = "Todo List"
	config     *Config
	items      = []Item{}
)

type Config struct {
	common.Config   `koanf:",squash"`
	CreatePrefix    string `koanf:"create_prefix" desc:"prefix used in order to create a new item" default:"add:"`
	UrgentTimeFrame int    `koanf:"urgent_time_frame" desc:"items that have a due time within this period will be marked as urgent" default:"10"`
	Notification    `koanf:",squash"`
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

type Item struct {
	Text     string
	Date     time.Time
	State    string
	Notified bool
}

func saveItems() {
	var b bytes.Buffer
	encoder := gob.NewEncoder(&b)

	err := encoder.Encode(items)
	if err != nil {
		slog.Error(Name, "saveencode", err)
		return
	}

	err = os.MkdirAll(filepath.Dir(common.CacheFile(fmt.Sprintf("%s.gob", Name))), 0755)
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
	splits := strings.SplitN(query, ":", 3)

	switch len(splits) {
	case 2:
		i.Text = splits[1]
	case 3:
		i.Text = splits[2]

		now := time.Now()
		fields := strings.Fields(splits[1])

		if len(fields) == 2 {
			switch fields[0] {
			case "in":
				switch {
				case strings.HasSuffix(fields[1], "s"):
					add := strings.TrimSuffix(fields[1], "s")

					addi, _ := strconv.Atoi(add)
					now = now.Add(time.Duration(addi) * time.Second)
				case strings.HasSuffix(fields[1], "m"):
					add := strings.TrimSuffix(fields[1], "m")

					addi, _ := strconv.Atoi(add)
					now = now.Add(time.Duration(addi) * time.Minute)
					now = now.Truncate(time.Minute)
				case strings.HasSuffix(fields[1], "h"):
					add := strings.TrimSuffix(fields[1], "h")

					addi, _ := strconv.Atoi(add)
					now = now.Add(time.Duration(addi) * time.Hour)
					now = now.Truncate(time.Minute)
				}
			case "at":
				hour := fields[1][:2]
				minute := fields[1][2:]
				houri, _ := strconv.Atoi(hour)
				minutei, _ := strconv.Atoi(minute)

				now = time.Date(now.Year(), now.Month(), now.Day(),
					0, 0, 0, 0, now.Location())
				now = now.Add(time.Duration(houri)*time.Hour +
					time.Duration(minutei)*time.Minute)
			}

			i.Date = now
		}
	}
}

func init() {
	config = &Config{
		Config: common.Config{
			Icon: "accessories-calculator",
		},
		CreatePrefix:    "add:",
		UrgentTimeFrame: 10,
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
			if v.Notified || v.Date.IsZero() {
				continue
			}

			if v.Date.Equal(now) || v.Date.Before(now) {
				body := strings.ReplaceAll(config.Body, "%TASK%", v.Text)
				cmd := exec.Command("notify-send", config.Title, body)

				err := cmd.Start()
				if err != nil {
					slog.Error(Name, "notify", err)
				} else {
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

func PrintDoc() {
	fmt.Printf("### %s\n", NamePretty)
	fmt.Println("Simple Todo List with notifications")
	fmt.Println()
}

func Cleanup(qid uint32) {
}

func Activate(qid uint32, identifier, action string, arguments string) {
	if strings.HasPrefix(identifier, config.CreatePrefix) {
		store(identifier)
		return
	}

	i, _ := strconv.Atoi(identifier)

	switch action {
	case ActionDelete:
		items = append(items[:i], items[i+1:]...)
	case ActionMarkActive:
		if items[i].State == StateActive {
			items[i].State = StatePending
		} else {
			items[i].State = StateActive
		}
	case ActionMarkDone:
		if items[i].State == StateDone {
			items[i].State = StatePending
		} else {
			items[i].State = StateDone
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

	if strings.HasPrefix(query, config.CreatePrefix) {
		i := Item{}
		i.fromQuery(query)

		e := &pb.QueryResponse_Item{}
		e.Score = 3_000_000
		e.Provider = Name
		e.Identifier = query
		e.Icon = "list-add"
		e.Text = i.Text
		e.State = []string{StateCreating}

		if !i.Date.IsZero() {
			e.Subtext = i.Date.Format(time.TimeOnly)
		}

		entries = append(entries, e)
	}

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

		if !v.Date.IsZero() {
			e.Subtext = v.Date.Format(time.TimeOnly)
		}

		if query != "" && !strings.HasPrefix(query, config.CreatePrefix) {
			e.Score, e.Fuzzyinfo.Positions, e.Fuzzyinfo.Start = common.FuzzyScore(query, e.Text, exact)
		}

		if !v.Date.IsZero() && v.Date.Before(urgent) && v.State != StateDone && v.State != StateActive {
			e.State = []string{StateUrgent}
		}

		if slices.Contains(e.State, StateActive) && query == "" {
			e.Score = 1_000_001
		}

		if slices.Contains(e.State, StateUrgent) && query == "" {
			diff := time.Since(v.Date).Minutes()
			e.Score = 2_000_000 + int32(diff)
		}

		entries = append(entries, e)
	}

	return entries
}

func Icon() string {
	return config.Icon
}
