// Package clipboard provides access to the clipboard history.
package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	_ "embed"
	"encoding/gob"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"golang.org/x/net/html"
)

var (
	Name             = "clipboard"
	NamePretty       = "Clipboard"
	file             = common.CacheFile("clipboard.gob")
	imgTypes         = make(map[string]string)
	config           *Config
	clipboardhistory = make(map[string]*Item)
	mu               sync.RWMutex
	imagesOnly       = false
)

//go:embed README.md
var readme string

//go:embed data/UnicodeData.txt
var unicodedata string

//go:embed data/symbols.xml
var symbolsdata string

const StateEditable = "editable"

type Item struct {
	Content string
	Img     string
	Time    time.Time
	State   string
}

type Config struct {
	common.Config  `koanf:",squash"`
	MaxItems       int    `koanf:"max_items" desc:"max amount of clipboard history items" default:"100"`
	ImageEditorCmd string `koanf:"image_editor_cmd" desc:"editor to use for images. use '%FILE%' as placeholder for file path." default:""`
	TextEditorCmd  string `koanf:"text_editor_cmd" desc:"editor to use for text, otherwise default for mimetype. use '%FILE%' as placeholder for file path." default:""`
	Command        string `koanf:"command" desc:"default command to be executed" default:"wl-copy"`
	Recopy         bool   `koanf:"recopy" desc:"recopy content to make it persistent after closing a window" default:"true"`
	IgnoreSymbols  bool   `koanf:"ignore_symbols" desc:"ignores symbols/unicode" default:"true"`
	AutoCleanup    int    `koanf:"auto_cleanup" desc:"will automatically cleanup entries every X minutes" default:"0"`
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
		Recopy:         true,
		IgnoreSymbols:  true,
		AutoCleanup:    0,
	}

	common.LoadConfig(Name, config)

	imgTypes["image/png"] = "png"
	imgTypes["image/jpg"] = "jpg"
	imgTypes["image/jpeg"] = "jpeg"
	imgTypes["image/webm"] = "webm"

	loadFromFile()

	go handleChangeImage()
	go handleChangeText()

	if config.IgnoreSymbols {
		setupUnicodeSymbols()
	}

	if config.AutoCleanup != 0 {
		go cleanup()
	}

	mu.RLock()
	slog.Info(Name, "history", len(clipboardhistory), "time", time.Since(start))
	mu.RUnlock()
}

func cleanup() {
	for {
		time.Sleep(time.Duration(config.AutoCleanup) * time.Minute)

		i := 0

		mu.Lock()
		for k := range clipboardhistory {
			delete(clipboardhistory, k)
			i++
		}
		mu.Unlock()

		if i != 0 {
			saveToFile()
			slog.Info(Name, "cleanup", i)
		}
	}
}

var symbols = make(map[string]struct{})

type LDML struct {
	XMLName     xml.Name    `xml:"ldml"`
	Identity    Identity    `xml:"identity"`
	Annotations Annotations `xml:"annotations"`
}

type Identity struct {
	Version  Version  `xml:"version"`
	Language Language `xml:"language"`
}

type Version struct {
	Number string `xml:"number,attr"`
}

type Language struct {
	Type string `xml:"type,attr"`
}

type Annotations struct {
	Annotation []Annotation `xml:"annotation"`
}

type Annotation struct {
	CP   string `xml:"cp,attr"`
	Type string `xml:"type,attr,omitempty"`
	Text string `xml:",chardata"`
}

type Symbol struct {
	CP         string
	Searchable []string
}

func setupUnicodeSymbols() {
	mu.Lock()
	defer mu.Unlock()
	// unicode
	for v := range strings.Lines(unicodedata) {
		if v == "" {
			continue
		}

		fields := strings.SplitN(v, ";", 3)

		codePoint, err := strconv.ParseInt(fields[0], 16, 32)
		if err != nil {
			slog.Error(Name, "activate parse unicode", err)
			return
		}

		toUse := string(rune(codePoint))
		symbols[toUse] = struct{}{}
	}

	// symbols
	var ldml LDML

	err := xml.Unmarshal([]byte(symbolsdata), &ldml)
	if err != nil {
		panic(err)
	}

	for _, v := range ldml.Annotations.Annotation {
		if _, ok := symbols[v.CP]; !ok {
			symbols[v.CP] = struct{}{}
		}
	}
}

func loadFromFile() {
	if common.FileExists(file) {
		f, err := os.ReadFile(file)
		if err != nil {
			slog.Error("history", "load", err)
		} else {
			decoder := gob.NewDecoder(bytes.NewReader(f))

			mu.Lock()
			err = decoder.Decode(&clipboardhistory)
			if err != nil {
				slog.Error("history", "decoding", err)
			}
			mu.Unlock()
		}
	}
}

func cleanupImages() {
	d, _ := os.UserCacheDir()
	folder := filepath.Join(d, "elephant", "clipboardimages")

	filepath.Walk(folder, func(path string, info fs.FileInfo, err error) error {
		if !info.IsDir() {
			os.Remove(path)
		}

		return nil
	})
}

func saveToFile() {
	mu.Lock()
	if len(clipboardhistory) > config.MaxItems {
		trimLocked()
	}

	var b bytes.Buffer
	encoder := gob.NewEncoder(&b)

	err := encoder.Encode(clipboardhistory)
	if err != nil {
		slog.Error(Name, "encode", err)
		return
	}
	mu.Unlock()

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

func handleChangeText() {
	cmd := exec.Command("wl-paste", "--type", "text", "--watch", "echo", "")

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
		updateText()
	}
}

func handleChangeImage() {
	cmd := exec.Command("wl-paste", "--type", "image", "--watch", "echo", "")

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
		updateImage()
	}
}

var ignoreMimetypes = []string{"x-kde-passwordManagerHint", "text/uri-list"}

func recopy(b []byte) {
	if !config.Recopy {
		return
	}

	cmd := exec.Command("wl-copy")
	cmd.Stdin = bytes.NewReader(b)

	err := cmd.Start()
	if err != nil {
		slog.Error(Name, "recopy", err)
	} else {
		go func() {
			cmd.Wait()
		}()
	}
}

func updateImage() {
	cmd := exec.Command("wl-paste", "-t", "image", "-n")

	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Nothing is copied") {
			return
		}

		slog.Error(Name, "error", err)

		return
	}

	mt := getMimetypes()

	// special treatment for gimp
	if slices.Contains(mt, "image/x-xcf") {
		buf := bytes.NewBuffer([]byte{})
		cmd := exec.Command("wl-paste", "-t", "image/png")
		cmd.Stdout = buf

		cmd.Run()
		out = buf.Bytes()
	}

	md5 := md5.Sum(out)
	md5str := hex.EncodeToString(md5[:])

	if val, ok := clipboardhistory[md5str]; ok {
		val.Time = time.Now()
		return
	}

	if file := saveImg(out, "raw"); file != "" {
		clipboardhistory[md5str] = &Item{
			Img:   file,
			Time:  time.Now(),
			State: StateEditable,
		}
	}

	recopy(out)

	saveToFile()
}

func updateText() {
	cmd := exec.Command("wl-paste", "-t", "text", "-n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Nothing is copied") {
			return
		}

		slog.Error("clipboard", "error", err)

		return
	}

	if strings.TrimSpace(string(out)) == "" {
		return
	}

	if config.IgnoreSymbols {
		mu.RLock()
		if _, ok := symbols[string(out)]; ok {
			return
		}
		mu.RUnlock()
	}

	mt := getMimetypes()

	for _, v := range mt {
		if slices.Contains(ignoreMimetypes, v) {
			return
		}
	}

	md5 := md5.Sum(out)
	md5str := hex.EncodeToString(md5[:])

	mu.Lock()
	if val, ok := clipboardhistory[md5str]; ok {
		val.Time = time.Now()
		return
	}

	if !utf8.Valid(out) {
		slog.Error(Name, "updating", "string content contains invalid UTF-8")
	}

	clipboardhistory[md5str] = &Item{
		Content: string(out),
		Time:    time.Now(),
		State:   StateEditable,
	}
	mu.Unlock()

	recopy(out)

	saveToFile()
}

func trimLocked() {
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
		mu.RLock()
		item := clipboardhistory[identifier]
		if item.State != StateEditable {
			return
		}
		mu.RUnlock()

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
		mu.RLock()

		if _, ok := clipboardhistory[identifier]; ok {
			if clipboardhistory[identifier].Img != "" {
				_ = os.Remove(clipboardhistory[identifier].Img)
			}

			delete(clipboardhistory, identifier)

			saveToFile()
		}

		mu.RUnlock()
	case ActionRemoveAll:
		mu.Lock()
		clipboardhistory = make(map[string]*Item)

		saveToFile()
		cleanupImages()
		mu.Unlock()
	case ActionCopy:
		mu.RLock()
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
		mu.RUnlock()
	default:
		slog.Error(Name, "activate", fmt.Sprintf("unknown action: %s", action))
		return
	}
}

func Query(conn net.Conn, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	mu.RLock()
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
	mu.RUnlock()

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

func getImgSrc(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "img" {
		for _, attr := range n.Attr {
			if attr.Key == "src" {
				return attr.Val
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if src := getImgSrc(c); src != "" {
			return src
		}
	}

	return ""
}

func downloadImage(url string) ([]byte, string) {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println(url)
		slog.Error(Name, "download", err)
		return nil, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error(Name, "download status", err)
		return nil, ""
	}

	contentType := resp.Header.Get("Content-Type")

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error(Name, "download read", err)
		return nil, ""
	}

	return data, contentType
}
