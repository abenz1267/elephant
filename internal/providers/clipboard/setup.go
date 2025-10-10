// Package clipboard provides access to the clipboard history.
package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	_ "embed"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name             = "clipboard"
	NamePretty       = "Clipboard"
	file             = common.CacheFile("clipboard.gob")
	imgTypes         = make(map[string]string)
	config           *Config
	clipboardhistory = make(map[string]*Item)
	mu               sync.Mutex
	imagesOnly       = false
)

//go:embed README.md
var readme string

const StateEditable = "editable"

type Item struct {
	Content  string
	Img      string
	Mimetype string
	Time     time.Time
	State    string
}

type Config struct {
	common.Config  `koanf:",squash"`
	MaxItems       int    `koanf:"max_items" desc:"max amount of clipboard history items" default:"100"`
	ImageEditorCmd string `koanf:"image_editor_cmd" desc:"editor to use for images. use '%FILE%' as placeholder for file path." default:""`
	TextEditorCmd  string `koanf:"text_editor_cmd" desc:"editor to use for text, otherwise default for mimetype. use '%FILE%' as placeholder for file path." default:""`
	Command        string `koanf:"command" desc:"default command to be executed" default:"wl-copy"`
}

func Setup() {
	start := time.Now()

	config = &Config{
		Config: common.Config{
			Icon:     "user-bookmarks",
			MinScore: 30,
		},
		MaxItems:       100,
		ImageEditorCmd: "",
		TextEditorCmd:  "",
		Command:        "wl-copy",
	}

	common.LoadConfig(Name, config)

	imgTypes["image/png"] = "png"
	imgTypes["image/jpg"] = "jpg"
	imgTypes["image/jpeg"] = "jpeg"
	imgTypes["image/webm"] = "webm"

	loadFromFile()

	go handleChange()

	slog.Info(Name, "history", len(clipboardhistory), "time", time.Since(start))
}

func loadFromFile() {
	if common.FileExists(file) {
		f, err := os.ReadFile(file)
		if err != nil {
			slog.Error("history", "load", err)
		} else {
			decoder := gob.NewDecoder(bytes.NewReader(f))

			err = decoder.Decode(&clipboardhistory)
			if err != nil {
				slog.Error("history", "decoding", err)
			}
		}
	}
}

func saveToFile() {
	var b bytes.Buffer
	encoder := gob.NewEncoder(&b)

	err := encoder.Encode(clipboardhistory)
	if err != nil {
		slog.Error(Name, "encode", err)
		return
	}

	err = os.MkdirAll(filepath.Dir(file), 0o755)
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

var (
	ignoreMimetypes   = []string{"x-kde-passwordManagerHint"}
	firefoxMimetypes  = []string{"text/_moz_htmlcontext"}
	chromiumMimetypes = []string{"chromium/x-source-url"}
)

func update() {
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

	if len(mt) == 0 {
		return
	}

	isImg := false
	isFF, isChrome := false, false

	for _, v := range mt {
		if slices.Contains(ignoreMimetypes, v) {
			return
		}

		if slices.Contains(firefoxMimetypes, v) {
			isFF = true
		}

		if slices.Contains(chromiumMimetypes, v) {
			isChrome = true
		}

		if _, ok := imgTypes[v]; ok {
			isImg = true
		}
	}

	if (isFF || isChrome) && isImg {
		slog.Debug(Name, "error", "can't save images from browsers")
		return
	}

	md5 := md5.Sum(out)
	md5str := hex.EncodeToString(md5[:])

	if val, ok := clipboardhistory[md5str]; ok {
		val.Time = time.Now()
		return
	}

	if !isImg && !utf8.Valid(out) {
		slog.Error(Name, "updating", "string content contains invalid UTF-8")
	}

	if !isImg {
		clipboardhistory[md5str] = &Item{
			Content: string(out),
			Time:    time.Now(),
			State:   StateEditable,
		}
	} else {
		if file := saveImg(out, imgTypes[mt[0]]); file != "" {
			clipboardhistory[md5str] = &Item{
				Img:      file,
				Mimetype: mt[0],
				Time:     time.Now(),
				State:    StateEditable,
			}
		}
	}

	if len(clipboardhistory) > config.MaxItems {
		trim()
		saveToFile()

		return
	}

	saveToFile()
}

func trim() {
	oldest := ""
	oldestTime := time.Now()

	for k, v := range clipboardhistory {
		if v.Time.Before(oldestTime) {
			oldest = k
			oldestTime = v.Time
		}
	}

	if clipboardhistory[oldest].Img != "" {
		_ = os.Remove(clipboardhistory[oldest].Img)
	}

	delete(clipboardhistory, oldest)
}

func saveImg(b []byte, ext string) string {
	d, _ := os.UserCacheDir()
	folder := filepath.Join(d, "elephant", "clipboardimages")

	os.MkdirAll(folder, 0o755)

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
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

const (
	ActionCopy              = "copy"
	ActionEdit              = "edit"
	ActionRemove            = "remove"
	ActionRemoveAll         = "remove_all"
	ActionToggleImages      = "toggle_images"
	ActionDisableImagesOnly = "disable_images_only"
)

func Activate(identifier, action string, query string, args string) {
	if action == "" {
		action = ActionCopy
	}

	switch action {
	case ActionDisableImagesOnly:
		imagesOnly = false
		return
	case ActionToggleImages:
		imagesOnly = !imagesOnly
		return
	case ActionEdit:
		item := clipboardhistory[identifier]
		if item.State != StateEditable {
			return
		}

		if item.Img != "" {
			if config.ImageEditorCmd == "" {
				slog.Info(Name, "edit", "image_editor not set")
				return
			}

			toRun := strings.ReplaceAll(config.ImageEditorCmd, "%FILE%", item.Img)

			cmd := exec.Command("sh", "-c", toRun)

			err := cmd.Start()
			if err != nil {
				slog.Error(Name, "openedit", err)
				return
			} else {
				go func() {
					cmd.Wait()
				}()
			}

			return
		}

		tmpFile, err := os.CreateTemp("", "*.txt")
		if err != nil {
			slog.Error(Name, "edit", err)
			return
		}

		tmpFile.Write([]byte(item.Content))

		var run string

		if config.TextEditorCmd != "" {
			run = strings.ReplaceAll(config.TextEditorCmd, "%FILE%", tmpFile.Name())
		} else {
			run = fmt.Sprintf("xdg-open file://%s", tmpFile.Name())

			if common.ForceTerminalForFile(tmpFile.Name()) {
				run = common.WrapWithTerminal(run)
			}
		}

		cmd := exec.Command("sh", "-c", run)
		err = cmd.Start()
		if err != nil {
			slog.Error(Name, "openedit", err)
			return
		} else {
			cmd.Wait()

			b, _ := os.ReadFile(tmpFile.Name())
			item.Content = string(b)
			saveToFile()
		}
	case ActionRemove:
		mu.Lock()

		if _, ok := clipboardhistory[identifier]; ok {
			if clipboardhistory[identifier].Img != "" {
				_ = os.Remove(clipboardhistory[identifier].Img)
			}

			delete(clipboardhistory, identifier)

			saveToFile()
		}

		mu.Unlock()
	case ActionRemoveAll:
		mu.Lock()
		clipboardhistory = make(map[string]*Item)

		saveToFile()
		mu.Unlock()
	case ActionCopy:
		cmd := exec.Command("sh", "-c", config.Command)

		item := clipboardhistory[identifier]
		if item.Img != "" {
			f, _ := os.ReadFile(item.Img)
			cmd.Stdin = bytes.NewReader(f)
		} else {
			cmd.Stdin = strings.NewReader(item.Content)
		}

		err := cmd.Start()
		if err != nil {
			slog.Error("clipboard", "activate", err)
			return
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}

func Query(conn net.Conn, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	for k, v := range clipboardhistory {
		if imagesOnly && v.Img == "" {
			continue
		}

		e := &pb.QueryResponse_Item{
			Identifier: k,
			Text:       v.Content,
			Icon:       v.Img,
			Subtext:    v.Time.Format(time.RFC1123Z),
			Type:       pb.QueryResponse_REGULAR,
			Actions:    []string{ActionCopy, ActionEdit, ActionRemove},
			Provider:   Name,
		}

		if query != "" {
			score, pos, start := common.FuzzyScore(query, v.Content, exact)

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

	if query == "" {
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
		log.Println(err)
		log.Println(string(out))
		return []string{}
	}

	return strings.Fields(string(out))
}

func Icon() string {
	return config.Icon
}
