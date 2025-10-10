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

	"github.com/abenz1267/elephant/internal/util"
	"github.com/abenz1267/elephant/pkg/common"
	"github.com/abenz1267/elephant/pkg/pb/pb"
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
	clipboardTool    = determineClipboardTool()
)

// Aggiungi queste costanti
const (
	ClipboardToolWayland = "wl-paste"
	ClipboardToolGPaste  = "gpaste-client"
)

//go:embed README.md
var readme string

const StateEditable = "editable"

type Item struct {
	ID       string
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

	// Determina quale tool usare
	clipboardTool = determineClipboardTool()

	loadFromFile()

	go handleChange()

	slog.Info(Name, "history", len(clipboardhistory), "time", time.Since(start))
}

func determineClipboardTool() string {

	// Thest first gpaste
	if checkToolAvailable(ClipboardToolGPaste) {
		slog.Info(Name, "using", ClipboardToolGPaste)
		return ClipboardToolGPaste
	}

	// fallback to wl-paste
	if checkToolAvailable(ClipboardToolWayland) {
		slog.Info(Name, "using", ClipboardToolWayland)
		return ClipboardToolWayland
	}
	// Nessun tool disponibile
	slog.Error(Name, "error", "no clipboard tool available (wl-paste or gpaste)")
	os.Exit(1)
	return ""
}

func checkToolAvailable(tool string) bool {
	cmd := exec.Command("which", tool)
	err := cmd.Run()
	return err == nil
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
	switch clipboardTool {
	case ClipboardToolWayland:
		handleChangeWayland()
	case ClipboardToolGPaste:
		handleChangeGPaste()
	}
}

func handleChangeGPaste() {
	// Per gpaste, dobbiamo polling poiché non ha --watch
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	//fmt.Printf("🎯 handleChangeGPaste \n")
	var lastContent string

	for range ticker.C {
		current := getGPasteContent()
		if current != "" && current != lastContent {
			lastContent = current
			update()
		}
	}
}

func handleChangeWayland() {
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
	var content []byte
	var mimetypes []string
	var err error

	switch clipboardTool {
	case ClipboardToolWayland:
		content, mimetypes, err = getWaylandContent()
	case ClipboardToolGPaste:
		content, mimetypes, err = getGPasteContentWithTypes()
	}

	if err != nil {
		slog.Error("clipboard", "error", err)
		return
	}

	if len(mimetypes) == 0 {
		return
	}

	isImg := false
	isFF, isChrome := false, false

	for _, v := range mimetypes {
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

	md5 := md5.Sum(content)
	//Check tool type
	md5str := ""
	parts := []string{}
	switch clipboardTool {
	case ClipboardToolWayland:
		md5str = hex.EncodeToString(md5[:])
	case ClipboardToolGPaste:
		parts = strings.SplitN(string(content), ":", 2)
		md5str = parts[0]
	}

	if val, ok := clipboardhistory[md5str]; ok {
		val.Time = time.Now()
		return
	}

	if !isImg && !utf8.Valid(content) {
		slog.Error(Name, "updating", "string content contains invalid UTF-8")
	}

	if !isImg {
		//Check tool type
		switch clipboardTool {
		case ClipboardToolWayland:
			clipboardhistory[md5str] = &Item{
				ID:      md5str,
				Content: string(content),
				Time:    time.Now(),
				State:   StateEditable,
			}
		case ClipboardToolGPaste:
			clipboardhistory[md5str] = &Item{
				ID:      md5str,
				Content: string(parts[1]),
				Time:    time.Now(),
				State:   StateEditable,
			}
		}

	} else {
		if file := saveImg(content, imgTypes[mimetypes[0]]); file != "" {
			clipboardhistory[md5str] = &Item{
				Img:      file,
				Mimetype: mimetypes[0],
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

func getGPasteHistory() []string {
	cmd := exec.Command("gpaste-client", "history")

	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ Error gpaste history: %v\n", err)
		return nil
	}

	output := string(out)
	//fmt.Printf("📜 Cronologia RAW:\n--- INIZIO ---\n%s\n--- FINE ---\n", output)
	//fmt.Printf("📏 Lunghezza totale: %d caratteri\n", len(output))

	// Dividi per newline - considera diversi tipi di newline
	var items []string

	// Prova prima con \n (Linux/Unix)
	if strings.Contains(output, "\n") {
		items = strings.Split(output, "\n")
	} else {
		items = []string{output}
	}

	var cleanItems []string
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			cleanItems = append(cleanItems, trimmed)
		}
	}
	return cleanItems
}

func getLastGPasteItem() string {
	items := getGPasteHistory()
	if len(items) == 0 {
		return ""
	}
	lastItem := items[0]

	return lastItem
}

func getGPasteContent() string {
	return getLastGPasteItem()
}

func getGPasteContentWithTypes() ([]byte, []string, error) {
	content := getGPasteContent()
	if content == "" {
		return nil, nil, fmt.Errorf("no content")
	}
	return []byte(content), []string{"text/plain"}, nil
}

func getWaylandContent() ([]byte, []string, error) {
	cmd := exec.Command("wl-paste", "-n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Nothing is copied") {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	mimetypes := getMimetypes()
	return out, mimetypes, nil
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
		var cmd *exec.Cmd

		item := clipboardhistory[identifier]
		if item.Img != "" {
			if checkToolAvailable("wl-copy") {
				f, _ := os.ReadFile(item.Img)
				cmd = exec.Command("wl-copy")
				cmd.Stdin = bytes.NewReader(f)
			} else {
				slog.Error(Name, "copy image", "wl-copy required for images")
				return
			}
		} else {
			if clipboardTool == ClipboardToolWayland {
				cmd = exec.Command("wl-copy")
				cmd.Stdin = strings.NewReader(item.Content)
			} else {
				cmd = exec.Command("gpaste-client", "select", item.ID)
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
	if clipboardTool == ClipboardToolGPaste {
		return []string{"text/plain"}
	}

	// Codice originale per wl-paste
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
