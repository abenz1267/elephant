package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

func handleChange() {
	cmd := exec.Command("wl-paste", "--watch", "echo", "clipboard-changed")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error(Name, "handleChange", "failed to create stdout pipe", "error", err)
		return
	}

	if err := cmd.Start(); err != nil {
		slog.Error(Name, "handleChange", "failed to start wl-paste watch", "error", err)
		return
	}

	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		if paused.Load() {
			continue
		}

		text, texterr := getClipboardText()
		if texterr == nil {
			mu.Lock()
			ok := updateText(text)
			if ok {
				if !slices.Contains(availableModes, ActionTextOnly) {
					availableModes = append(availableModes, ActionTextOnly)
				}
				mu.Unlock()
				continue
			} else {
				mu.Unlock()
			}
		}

		img, imgerr := getClipboardImage()
		if imgerr == nil {
			mu.Lock()
			updateImage(img)
			if !slices.Contains(availableModes, ActionImagesOnly) {
				availableModes = append(availableModes, ActionImagesOnly)
			}
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

func hasImageType(mt []string) bool {
	for _, m := range mt {
		if _, ok := imgTypes[m]; ok {
			return true
		}
	}
	return false
}

func updateImage(out []byte) {
	mt := getMimetypes()

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
			ocrText := ""
			if config.OCR {
				ocrText = runOCR(file)
			}

			clipboardhistory[md5str] = &Item{
				Content: ocrText,
				Img:     file,
				Time:    time.Now(),
				State:   StateEditable,
			}
		}
	}

	saveFileChan <- struct{}{}
}

func updateText(text string) bool {
	mt := getMimetypes()

	if hasImageType(mt) {
		return false
	}

	if strings.TrimSpace(text) == "" {
		return true
	}

	if config.IgnoreSymbols {
		if _, ok := symbols[text]; ok {
			return true
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

	if val, ok := clipboardhistory[md5str]; ok {
		val.Time = time.Now()
	} else {
		if !utf8.Valid(b) {
			slog.Error(Name, "updating", "string content contains invalid UTF-8")
		}

		if isURIList {
			clipboardhistory[md5str] = &Item{
				URIList: uris,
				Time:    time.Now(),
			}
		} else {
			clipboardhistory[md5str] = &Item{
				Content: text,
				Time:    time.Now(),
				State:   StateEditable,
			}
		}
	}

	saveFileChan <- struct{}{}
	return true
}

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	mu.Lock()
	defer mu.Unlock()

	entries := []*pb.QueryResponse_Item{}

	for k, v := range clipboardhistory {
		switch currentMode {
		case ActionPinnedOnly:
			if !v.Pinned {
				continue
			}
		case ActionImagesOnly:
			if v.Img == "" {
				continue
			}
		case ActionTextOnly:
			if v.Img != "" {
				continue
			}
		}

		actions := []string{ActionCopy, ActionEdit}

		if v.Pinned {
			actions = append(actions, ActionUnpin)
		} else {
			actions = append(actions, ActionPin, ActionRemove)
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

			for _, v := range v.URIList {
				files = append(files, filepath.Base(v))
			}

			content = strings.Join(files, ",")
		}

		if len([]rune(content)) > 1000 {
			content = string([]rune(content)[:1000])
		}

		e := &pb.QueryResponse_Item{
			Identifier: k,
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
			entries = append(entries, e)
		}
	}

	if query == "" {
		slices.SortStableFunc(entries, func(a, b *pb.QueryResponse_Item) int {
			ta, _ := time.Parse(time.RFC1123Z, a.Subtext)
			tb, _ := time.Parse(time.RFC1123Z, b.Subtext)

			return ta.Compare(tb) * -1
		})

		for k, v := range entries {
			if slices.Contains(v.State, "pinned") && config.PinnedOnTop {
				entries[k].Score = int32(1_000_000_000 - k)
			} else {
				entries[k].Score = int32(1_000_000 - k)
			}
		}
	}

	return entries
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	if action == "" {
		action = ActionCopy
	}

	switch action {
	case ActionLocalsend:
		item := clipboardhistory[identifier]

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
		paused.Store(true)
	case ActionUnpause:
		paused.Store(false)
	case ActionImagesOnly, ActionTextOnly, ActionPinnedOnly, ActionCombined:
		currentMode = action
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

			if len(clipboardhistory) != 0 {
				setupModes()

				if !slices.Contains(availableModes, currentMode) {
					currentMode = ActionCombined
				}
			}

			saveToFile()
		}

		mu.Unlock()
	case ActionUnpin:
		mu.Lock()

		if val, ok := clipboardhistory[identifier]; ok {
			val.Pinned = false

			saveToFile()
		}

		setupModes()

		mu.Unlock()
	case ActionPin:
		mu.Lock()

		if val, ok := clipboardhistory[identifier]; ok {
			val.Pinned = true

			saveToFile()
		}

		if !slices.Contains(availableModes, ActionPinnedOnly) {
			availableModes = append(availableModes, ActionPinnedOnly)
		}

		mu.Unlock()
	case ActionRemoveAll:
		mu.Lock()

		for k, v := range clipboardhistory {
			if v.Pinned {
				continue
			}

			delete(clipboardhistory, k)

			if v.Img != "" {
				_ = os.Remove(v.Img)
			}
		}

		saveToFile()
		currentMode = ActionCombined
		setupModes()
		mu.Unlock()
	case ActionCopy:
		cmd := exec.Command("sh", "-c", config.Command)

		item := clipboardhistory[identifier]
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
