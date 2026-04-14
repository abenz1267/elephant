// Package clipboard provides access to the clipboard history.
package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	_ "embed"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name         = "clipboard"
	NamePretty   = "Clipboard"
	imgTypes     = make(map[string]string)
	config       *Config
	mu           sync.Mutex
	currentMode  = Combined
	nextMode     = ActionImagesOnly
	hasImg       = false
	hasText      = false
	hasLocalsend bool
)

//go:embed README.md
var readme string

//go:embed data/UnicodeData.txt
var unicodedata string

//go:embed data/symbols.xml
var symbolsdata string

var paused bool

const StateEditable = "editable"

type Item struct {
	Content string
	Img     string
	URIList []string
	Time    time.Time
	State   string
	Pinned  bool
}

type Config struct {
	common.Config  `koanf:",squash"`
	MaxItems       int    `koanf:"max_items" desc:"max amount of clipboard history items" default:"100"`
	ImageEditorCmd string `koanf:"image_editor_cmd" desc:"editor to use for images. use '%FILE%' as placeholder for file path." default:""`
	TextEditorCmd  string `koanf:"text_editor_cmd" desc:"editor to use for text, otherwise default for mimetype. use '%FILE%' as placeholder for file path." default:""`
	Command        string `koanf:"command" desc:"default command to be executed" default:"wl-copy"`
	IgnoreSymbols  bool   `koanf:"ignore_symbols" desc:"ignores symbols/unicode" default:"true"`
	PinnedOnTop    bool   `koanf:"pinned_on_top" desc:"put pinned items on top" default:"false"`
	AutoCleanup    int    `koanf:"auto_cleanup" desc:"will automatically cleanup entries entries older than X minutes" default:"0"`
}

func Setup() {
	start := time.Now()

	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	imgTypes["image/png"] = "png"
	imgTypes["image/jpg"] = "jpg"
	imgTypes["image/jpeg"] = "jpeg"
	imgTypes["image/webm"] = "webm"

	ls, err := exec.LookPath("localsend")
	if ls != "" && err == nil {
		hasLocalsend = true
	}

	if err := openDB(); err != nil {
		slog.Error(Name, "setup", err)
		return
	}

	migrateGobToSQLite()

	go handleChange()

	if config.IgnoreSymbols {
		setupUnicodeSymbols()
	}

	if config.AutoCleanup != 0 {
		go cleanup()
	}

	hasText, hasImg = countByType()

	slog.Info(Name, "history", itemCount(), "time", time.Since(start))
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon:     "user-bookmarks",
			MinScore: 30,
		},
		MaxItems:       100,
		ImageEditorCmd: "",
		TextEditorCmd:  "",
		Command:        "wl-copy",
		IgnoreSymbols:  true,
		AutoCleanup:    0,
		PinnedOnTop:    false,
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	p, err := exec.LookPath("wl-paste")
	if p == "" || err != nil {
		slog.Info(Name, "available", "wl-clipboard not found. disabling")
		return false
	}

	p, err = exec.LookPath("identify")
	if p == "" || err != nil {
		slog.Info(Name, "available", "imagemagick not found. disabling")
		return false
	}

	return true
}

func cleanup() {
	for {
		time.Sleep(time.Duration(config.AutoCleanup) * time.Minute)

		i := cleanupOldEntries(config.AutoCleanup)

		if i != 0 {
			mu.Lock()
			hasText, hasImg = countByType()
			mu.Unlock()
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
		mu.Lock()
		symbols[toUse] = struct{}{}
		mu.Unlock()
	}

	// symbols
	var ldml LDML

	err := xml.Unmarshal([]byte(symbolsdata), &ldml)
	if err != nil {
		panic(err)
	}

	for _, v := range ldml.Annotations.Annotation {
		mu.Lock()
		if _, ok := symbols[v.CP]; !ok {
			symbols[v.CP] = struct{}{}
		}
		mu.Unlock()
	}
}


func handleChange() {
	cmd := exec.Command("wl-paste", "--watch", "echo", "clipboard-changed")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal("Error creating stdout pipe:", err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatal("Error starting wl-paste watch:", err)
	}

	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		if paused {
			continue
		}

		text, texterr := getClipboardText()
		if texterr == nil {
			ok := updateText(text)
			if ok {
				mu.Lock()
				hasText = true
				mu.Unlock()
				continue
			}
		}

		img, imgerr := getClipboardImage()
		if imgerr == nil {
			updateImage(img)
			mu.Lock()
			hasImg = true
			mu.Unlock()
			continue
		}
	}
}

func getClipboardImage() ([]byte, error) {
	cmd := exec.Command("wl-paste", "-t", "image", "-n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Debug(Name, "get clipboard img", string(out))
	}

	return out, err
}

func getClipboardText() (string, error) {
	cmd := exec.Command("wl-paste", "-t", "text", "-n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Debug(Name, "get clipboard text", string(out))
	}

	return string(out), err
}

var ignoreMimetypes = []string{"x-kde-passwordManagerHint"}

func updateImage(out []byte) {
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

	if existing := getItem(md5str); existing != nil {
		updateItemTime(md5str, time.Now())
	} else {
		cmd := exec.Command("identify", "-format", "%m", "-")
		cmd.Stdin = bytes.NewReader(out)

		res, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "update image", err, "msg", res)
			return
		}

		ext := strings.ToLower(string(res))
		ext = strings.TrimSpace(ext)

		if file := saveImg(out, ext); file != "" {
			putItem(md5str, &Item{
				Img:   file,
				Time:  time.Now(),
				State: StateEditable,
			})
			trimToMax(config.MaxItems)
		}
	}
}

// ... returns false if its an image from browser
func updateText(text string) bool {
	if strings.TrimSpace(text) == "" {
		return true
	}

	if config.IgnoreSymbols {
		if _, ok := symbols[text]; ok {
			return true
		}
	}

	mt := getMimetypes()

	if slices.Contains(mt, "text/_moz_htmlcontext") || slices.Contains(mt, "chromium/x-source-url") {
		for k := range imgTypes {
			if slices.Contains(mt, k) {
				return false
			}
		}
	}

	isURIList := false

	for _, v := range mt {
		if slices.Contains(ignoreMimetypes, v) {
			return true
		}

		if v == "text/uri-list" {
			isURIList = true
		}
	}

	uris := []string{}

	if isURIList {
		for v := range strings.FieldsSeq(text) {
			if strings.HasPrefix(v, "file://") {
				uris = append(uris, v)
			} else {
				uris = append(uris, fmt.Sprintf("file://%s", v))
			}
		}

		text = strings.Join(uris, "\n")
	}

	b := []byte(text)
	md5 := md5.Sum(b)
	md5str := hex.EncodeToString(md5[:])

	if existing := getItem(md5str); existing != nil {
		updateItemTime(md5str, time.Now())
	} else {
		if !utf8.Valid(b) {
			slog.Error(Name, "updating", "string content contains invalid UTF-8")
		}

		if isURIList {
			putItem(md5str, &Item{
				URIList: uris,
				Time:    time.Now(),
			})
		} else {
			putItem(md5str, &Item{
				Content: text,
				Time:    time.Now(),
				State:   StateEditable,
			})
		}

		trimToMax(config.MaxItems)
	}

	return true
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

func PrintDoc(write bool) {
	if !write {
		fmt.Println(readme)
		fmt.Println()
	}

	util.PrintConfig(config, Name, write)
}

const (
	ActionPause      = "pause"
	ActionPin        = "pin"
	ActionUnpin      = "unpin"
	ActionLocalsend  = "localsend"
	ActionUnpause    = "unpause"
	ActionCopy       = "copy"
	ActionEdit       = "edit"
	ActionRemove     = "remove"
	ActionRemoveAll  = "remove_all"
	ActionImagesOnly = "show_images_only"
	ActionTextOnly   = "show_text_only"
	ActionCombined   = "show_combined"

	ImagesOnly = "images_only"
	TextOnly   = "text_only"
	Combined   = "combined"
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	if action == "" {
		action = ActionCopy
	}

	switch action {
	case ActionLocalsend:
		item := getItem(identifier)
		if item == nil {
			return
		}

		var path string

		if item.Img != "" {
			path = item.Img
		} else {
			f, err := os.CreateTemp(os.TempDir(), "clipboard_*.txt")
			if err != nil {
				slog.Error(Name, "actionlocalsend", err)
			}

			_, err = f.WriteString(item.Content)
			if err != nil {
				slog.Error(Name, "actionlocalsend", err)
			}

			path = f.Name()
		}

		cmd := exec.Command("sh", "-c", strings.TrimSpace(fmt.Sprintf("%s %s %s", common.LaunchPrefix(), "localsend", path)))

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "actionlocalsend", err)
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	case ActionPause:
		paused = true
	case ActionUnpause:
		paused = false
	case ActionImagesOnly:
		currentMode = ImagesOnly
		nextMode = ActionTextOnly
	case ActionTextOnly:
		currentMode = TextOnly
		nextMode = ActionCombined
	case ActionCombined:
		currentMode = Combined
		nextMode = ActionImagesOnly
	case ActionEdit:
		item := getItem(identifier)
		if item == nil || item.State != StateEditable {
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
			updateItemContent(identifier, string(b))
		}
	case ActionRemove:
		item := getItem(identifier)
		if item == nil {
			return
		}

		if item.Img != "" {
			_ = os.Remove(item.Img)
		}

		deleteItem(identifier)

		mu.Lock()
		hasText, hasImg = countByType()

		if currentMode == ImagesOnly && !hasImg {
			currentMode = Combined
		}

		if currentMode == TextOnly && !hasText {
			currentMode = Combined
		}
		mu.Unlock()
	case ActionUnpin:
		updateItemPinned(identifier, false)
	case ActionPin:
		updateItemPinned(identifier, true)
	case ActionRemoveAll:
		imgs := deleteAllUnpinned()

		for _, img := range imgs {
			_ = os.Remove(img)
		}

		mu.Lock()
		hasImg = false
		hasText = false
		hasText, hasImg = countByType()
		currentMode = Combined
		mu.Unlock()
	case ActionCopy:
		item := getItem(identifier)
		if item == nil {
			return
		}

		cmd := exec.Command("sh", "-c", config.Command)

		if item.Img != "" {
			f, _ := os.ReadFile(item.Img)
			cmd.Stdin = bytes.NewReader(f)
		} else {
			if len(item.URIList) > 0 {
				withMimetype := fmt.Sprintf("%s -t 'text/uri-list'", config.Command)
				cmd = exec.Command("sh", "-c", withMimetype)

				uriList := strings.Join(item.URIList, "\n")
				cmd.Stdin = strings.NewReader(uriList)
			} else {
				cmd.Stdin = strings.NewReader(item.Content)
			}
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

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	rows := getItemsByQuery(currentMode, config.MaxItems)
	entries := make([]*pb.QueryResponse_Item, 0, len(rows))

	for k, row := range rows {
		v := row.Item

		actions := []string{ActionCopy, ActionEdit, ActionRemove}

		if v.Pinned {
			actions = append(actions, ActionUnpin)
		} else {
			actions = append(actions, ActionPin)
		}

		if hasLocalsend {
			actions = append(actions, ActionLocalsend)
		}

		state := []string{}

		if v.Pinned {
			state = append(state, "pinned")
		}

		content := v.Content

		isURIList := false

		if len(v.URIList) > 0 {
			isURIList = true
			files := []string{}

			for _, u := range v.URIList {
				files = append(files, filepath.Base(u))
			}

			content = strings.Join(files, ",")
		}

		if len([]rune(content)) > 1000 {
			content = string([]rune(content)[:1000])
		}

		e := &pb.QueryResponse_Item{
			Identifier: row.Hash,
			Text:       content,
			Subtext:    v.Time.Format(time.RFC1123Z),
			Type:       pb.QueryResponse_REGULAR,
			State:      state,
			Actions:    actions,
			Provider:   Name,
		}

		if v.Img != "" {
			e.Preview = v.Img
			e.PreviewType = util.PreviewTypeFile
		} else {
			if isURIList {
				if len(v.URIList) == 1 {
					e.Preview = v.URIList[0]
					e.PreviewType = util.PreviewTypeFile
				} else {
					e.Preview = strings.Join(v.URIList, "\n")
					e.PreviewType = util.PreviewTypeText
				}
			} else {
				e.Preview = v.Content
				e.PreviewType = util.PreviewTypeText
			}
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
			// Results are already sorted by time DESC from the database
			if slices.Contains(state, "pinned") && config.PinnedOnTop {
				e.Score = int32(1_000_000_000 - k)
			} else {
				e.Score = int32(1_000_000 - k)
			}

			entries = append(entries, e)
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

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	states := []string{currentMode}
	actions := []string{}

	if hasImg && hasText {
		actions = append(actions, nextMode)
	}

	if itemCount() == 0 {
		actions = []string{}
	} else {
		actions = append(actions, ActionRemoveAll)
	}

	if paused {
		states = append(states, "paused")
		actions = append(actions, ActionUnpause)
	} else {
		states = append(states, "unpaused")
		actions = append(actions, ActionPause)
	}

	return &pb.ProviderStateResponse{
		States:  states,
		Actions: actions,
	}
}
