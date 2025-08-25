// Package clipboard provides access to the clipboard history.
package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var (
	Name       = "clipboard"
	NamePretty = "Clipboard"
	file       = common.CacheFile("clipboard.gob")
	imgTypes   = make(map[string]string)
	config     *Config
	history    map[string]Item
)

type Item struct {
	Content  string
	Img      string
	Mimetype string
	Time     time.Time
}

type Config struct {
	common.Config `koanf:",squash"`
	MaxItems      int `koanf:"max_items" desc:"max amount of clipboard history items" default:"100"`
}

func init() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "user-bookmarks",
			MinScore: 30,
		},
		MaxItems: 100,
	}

	common.LoadConfig(Name, config)

	imgTypes["image/png"] = "png"
	imgTypes["image/jpg"] = "jpg"
	imgTypes["image/jpeg"] = "jpeg"

	loadFromFile()

	go handleChange()

	slog.Info(Name, "history", len(history), "time", time.Since(start))
}

func loadFromFile() {
	if common.FileExists(file) {
		f, err := os.ReadFile(file)
		if err != nil {
			slog.Error("history", "load", err)
		} else {
			decoder := gob.NewDecoder(bytes.NewReader(f))

			err = decoder.Decode(&history)
			if err != nil {
				slog.Error("history", "decoding", err)
			}
		}
	} else {
		history = map[string]Item{}
	}
}

func saveToFile() {
	var b bytes.Buffer
	encoder := gob.NewEncoder(&b)

	err := encoder.Encode(history)
	if err != nil {
		slog.Error(Name, "encode", err)
		return
	}

	err = os.MkdirAll(filepath.Dir(file), 0755)
	if err != nil {
		slog.Error(Name, "createdirs", err)
		return
	}

	err = os.WriteFile(file, b.Bytes(), 0o600)
	if err != nil {
		slog.Error(Name, "writefile", err)
	}
}

func handleChange() {
	cmd := exec.Command("wl-paste", "--watch", "echo", "")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error(Name, "load", err)
		os.Exit(1)
	}

	err = cmd.Start()
	if err != nil {
		slog.Error(Name, "load", err)
		os.Exit(1)
	} else {
		go func() {
			cmd.Wait()
		}()
	}

	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		update()
	}
}

func update() {
	if os.Getenv("CLIPBOARD_STATE") == "sensitive" {
		return
	}

	cmd := exec.Command("wl-paste", "-n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Nothing is copied") {
			return
		}

		slog.Error("clipboard", "error", err)

		return
	}

	mt := getMimetypes()
	isImg := false

	for _, v := range mt {
		if _, ok := imgTypes[v]; ok {
			isImg = true
			break
		}
	}

	md5 := md5.Sum(out)
	md5str := hex.EncodeToString(md5[:])

	if _, ok := history[md5str]; ok {
		return
	}

	if !isImg {
		history[md5str] = Item{
			Content: string(out),
			Time:    time.Now(),
		}
	} else {
		if file := saveImg(out, imgTypes[mt[0]]); file != "" {
			history[md5str] = Item{
				Img:      file,
				Mimetype: mt[0],
				Time:     time.Now(),
			}
		}
	}

	if len(history) > config.MaxItems {
		trim()
		saveToFile()

		return
	}

	saveToFile()
}

func trim() {
	oldest := ""
	oldestTime := time.Now()

	for k, v := range history {
		if v.Time.Before(oldestTime) {
			oldest = k
			oldestTime = v.Time
		}
	}

	if history[oldest].Img != "" {
		_ = os.Remove(history[oldest].Img)
	}

	delete(history, oldest)
}

func saveImg(b []byte, ext string) string {
	d, _ := os.UserCacheDir()
	folder := filepath.Join(d, "elephant", "clipboardimages")

	os.MkdirAll(folder, 0755)

	file := filepath.Join(folder, fmt.Sprintf("%d.%s", time.Now().Unix(), ext))

	outfile, err := os.Create(file)
	if err != nil {
		panic(err)
	}
	defer outfile.Close()

	_, err = outfile.Write(b)
	if err != nil {
		slog.Error("clipboard", "writeimage", err)
		return ""
	}

	return file
}

func PrintDoc() {
	fmt.Printf("### %s\n", NamePretty)
	fmt.Println("Provides access to your clipboard history.")
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Cleanup(qid uint32) {}

const (
	ActionCopy   = "copy"
	ActionRemove = "remove"
)

func Activate(qid uint32, identifier, action string, arguments string) {
	if action == "" {
		action = ActionCopy
	}

	switch action {
	case ActionRemove:
		if history[identifier].Img != "" {
			_ = os.Remove(history[identifier].Img)
		}

		delete(history, identifier)

		saveToFile()
	case ActionCopy:
		cmd := exec.Command("wl-copy")

		item := history[identifier]
		if item.Img != "" {
			f, _ := os.ReadFile(item.Img)
			cmd.Stdin = bytes.NewReader(f)
		} else {
			cmd.Stdin = strings.NewReader(item.Content)
		}

		err := cmd.Start()
		if err != nil {
			slog.Error("clipboard", "activate", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	default:
		slog.Error(Name, "activate", fmt.Sprintf("no such action '%s'", action))
	}
}

func Query(qid uint32, iid uint32, text string, _ bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	for k, v := range history {
		e := &pb.QueryResponse_Item{
			Identifier: k,
			Text:       v.Content,
			Icon:       v.Img,
			Subtext:    v.Time.Format(time.RFC1123Z),
			Type:       pb.QueryResponse_REGULAR,
			Provider:   Name,
		}

		if text != "" {
			score, pos, start := common.FuzzyScore(text, v.Content, exact)

			e.Score = score
			e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Field:     "text",
				Positions: pos,
				Start:     start,
			}

			if e.Score > config.MinScore {
				entries = append(entries, e)
			}
		} else {
			entries = append(entries, e)
		}
	}

	if text == "" {
		slices.SortStableFunc(entries, func(a, b *pb.QueryResponse_Item) int {
			ta, _ := time.Parse(time.RFC1123Z, a.Subtext)
			tb, _ := time.Parse(time.RFC1123Z, b.Subtext)

			return ta.Compare(tb) * -1
		})

		for k := range entries {
			entries[k].Score = int32(10000 - k)
		}
	}

	return entries
}

func getMimetypes() []string {
	cmd := exec.Command("wl-paste", "--list-types")

	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(string(out))
		log.Panic(err)
	}

	return strings.Fields(string(out))
}

func Icon() string {
	return config.Icon
}
