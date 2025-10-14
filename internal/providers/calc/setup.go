package main

import (
	"bytes"
	"crypto/md5"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/comm/handlers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "calc"
	NamePretty = "Calculator/Unit-Conversion"
	config     *Config
)

//go:embed README.md
var readme string

const (
	ActionCopy   = "copy"
	ActionSave   = "save"
	ActionDelete = "delete"
)

type Config struct {
	common.Config `koanf:",squash"`
	MaxItems      int    `koanf:"max_items" desc:"max amount of calculation history items" default:"100"`
	Placeholder   string `koanf:"placeholder" desc:"placeholder to display for async update" default:"calculating..."`
	RequireNumber bool   `koanf:"require_number" desc:"don't perform if query does not contain a number" default:"true"`
	MinChars      int    `koanf:"min_chars" desc:"don't perform if query is shorter than min_chars" default:"3"`
	Command       string `koanf:"command" desc:"default command to be executed. supports %VALUE%." default:"wl-copy"`
}

type HistoryItem struct {
	Identifier string
	Input      string
	Result     string
}

var history = []HistoryItem{}

func Setup() {
	p, err := exec.LookPath("qalc")
	if err != nil {
		slog.Error(Name, "setup", err)
		return
	}

	if p == "" {
		slog.Error(Name, "setup", "qalc not installed")
		return
	}

	config = &Config{
		Config: common.Config{
			Icon: "accessories-calculator",
		},
		MaxItems:      100,
		Placeholder:   "calculating...",
		RequireNumber: true,
		MinChars:      3,
		Command:       "wl-copy",
	}

	common.LoadConfig(Name, config)

	loadHist()

	// this is to update exchange rate data
	cmd := exec.Command("qalc", "-e", "1+1")
	err = cmd.Start()
	if err != nil {
		slog.Error(Name, "init", err)
	} else {
		go func() {
			cmd.Wait()
		}()
	}
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Activate(identifier, action string, query string, args string) {
	i := slices.IndexFunc(history, func(item HistoryItem) bool {
		return item.Identifier == identifier
	})

	var result string
	createHistoryItem := false

	if i != -1 {
		result = history[i].Result
	} else {
		cmd := exec.Command("qalc", "-t", query)
		out, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "result", err)
			return
		}

		result = strings.TrimSpace(string(out))
		createHistoryItem = true
	}

	switch action {
	case ActionCopy:
		cmd := common.ReplaceResultOrStdinCmd(config.Command, result)

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "copy", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}

		if createHistoryItem {
			saveToHistory(query, result)
		}
	case ActionSave:
		saveToHistory(query, result)
	case ActionDelete:
		i := 0

		for k, v := range history {
			if v.Identifier == identifier {
				i = k
				break
			}
		}

		if len(history) > 0 {
			history = append(history[:i], history[i+1:]...)
		} else {
			history = []HistoryItem{}
		}

		saveHist()
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}

func saveToHistory(query, result string) {
	md5 := md5.Sum([]byte(query))
	md5str := hex.EncodeToString(md5[:])

	h := HistoryItem{
		Identifier: md5str,
		Input:      query,
		Result:     result,
	}

	history = append([]HistoryItem{h}, history...)

	saveHist()
}

func Query(conn net.Conn, query string, single bool, _ bool) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	hasNumber := true

	if config.RequireNumber {
		hasNumber = false

		for _, c := range query {
			if unicode.IsDigit(c) {
				hasNumber = true
			}
		}
	}

	if query != "" && len(query) >= config.MinChars && hasNumber {
		md5 := md5.Sum([]byte(query))
		md5str := hex.EncodeToString(md5[:])

		e := &pb.QueryResponse_Item{
			Identifier: md5str,
			Text:       config.Placeholder,
			Icon:       config.Icon,
			Subtext:    query,
			Provider:   Name,
			Score:      int32(config.MaxItems) + 1,
			Type:       pb.QueryResponse_REGULAR,
			State:      []string{"current"},
			Actions:    []string{ActionSave, ActionCopy},
		}

		go func() {
			cmd := exec.Command("qalc", "-t", query)
			out, err := cmd.CombinedOutput()

			if err == nil {
				e.Text = strings.TrimSpace(string(out))
			} else {
				slog.Error(Name, "qalc", err, "out", out)
				e.Text = "%DELETE%"
			}

			handlers.UpdateItem(query, conn, e)
		}()

		entries = append(entries, e)
	}

	if single {
		for k, v := range history {
			e := &pb.QueryResponse_Item{
				Identifier: v.Identifier,
				Text:       v.Result,
				Score:      int32(config.MaxItems - k),
				Icon:       config.Icon,
				Subtext:    v.Input,
				Provider:   Name,
				State:      []string{"saved"},
				Actions:    []string{ActionDelete, ActionCopy},
				Type:       pb.QueryResponse_REGULAR,
			}

			entries = append(entries, e)
		}
	}

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))

	return entries
}

func loadHist() {
	file := common.CacheFile(fmt.Sprintf("%s.gob", Name))

	if common.FileExists(file) {
		f, err := os.ReadFile(file)
		if err != nil {
			slog.Error(Name, "history", err)
		} else {
			decoder := gob.NewDecoder(bytes.NewReader(f))

			err = decoder.Decode(&history)
			if err != nil {
				slog.Error(Name, "decoding", err)
			}
		}
	}
}

func saveHist() {
	if len(history) > config.MaxItems {
		history = history[:config.MaxItems]
	}

	var b bytes.Buffer
	encoder := gob.NewEncoder(&b)

	err := encoder.Encode(history)
	if err != nil {
		slog.Error("history", "encode", err)
		return
	}

	err = os.MkdirAll(filepath.Dir(common.CacheFile(fmt.Sprintf("%s.gob", Name))), 0o755)
	if err != nil {
		slog.Error("history", "createdirs", err)
		return
	}

	err = os.WriteFile(common.CacheFile(fmt.Sprintf("%s.gob", Name)), b.Bytes(), 0o600)
	if err != nil {
		slog.Error("history", "writefile", err)
	}
}

func Icon() string {
	return config.Icon
}
