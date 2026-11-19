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

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

func Setup() {
	start := time.Now()

	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	imgTypes["image/png"] = "png"
	imgTypes["image/jpg"] = "jpg"
	imgTypes["image/jpeg"] = "jpeg"
	imgTypes["image/webp"] = "webp"

	ls, err := exec.LookPath("localsend")
	if ls != "" && err == nil {
		hasLocalsend = true
	}

	if config.OCR {
		if _, err := exec.LookPath("tesseract"); err != nil {
			slog.Warn(Name, "ocr", "tesseract not found. install with: sudo pacman -S tesseract tesseract-data-eng")
		}
	}

	initUnicodeSymbols()
	loadFromFile()

	go handleChange()
	go handleSaveToFile()

	if config.AutoCleanup != 0 {
		go cleanup()
	}

	setupModes()

	slog.Info(Name, "history", len(clipboardhistory), "time", time.Since(start))
}

func setupModes() {
	availableModes = []string{}
	availableModes = append(availableModes, ActionCombined)

	for _, v := range clipboardhistory {
		if v.Pinned && !slices.Contains(availableModes, ActionPinnedOnly) {
			availableModes = append(availableModes, ActionPinnedOnly)
		}

		if v.Img != "" && !slices.Contains(availableModes, ActionImagesOnly) {
			availableModes = append(availableModes, ActionImagesOnly)
		} else if !slices.Contains(availableModes, ActionTextOnly) {
			availableModes = append(availableModes, ActionTextOnly)
		}

		if len(availableModes) == 4 {
			break
		}
	}

	slices.Sort(availableModes)
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon:     "user-bookmarks",
			MinScore: 30,
		},
		MaxItems:       100,
		ImageEditorCmd: "",
		OCR:            false,
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

		i := 0

		now := time.Now()

		mu.Lock()
		for k, v := range clipboardhistory {
			if now.Sub(v.Time).Minutes() >= float64(config.AutoCleanup) {
				delete(clipboardhistory, k)
				i++
			}
		}

		if i != 0 {
			saveToFileLocked()
			slog.Info(Name, "cleanup", i)
		}
		mu.Unlock()
	}
}

func initUnicodeSymbols() {
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

	// TODO: xml.Unmarshal causes a crash with GOEXPERIMENT=nodwarf5 (Go stdlib race in encoding/xml)
	// These ~3000 symbol annotations are not critical; the unicode data above covers the main cases.
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
	mu.Lock()
	defer mu.Unlock()
	saveToFileLocked()
}

func saveToFileLocked() {
	if len(clipboardhistory) > config.MaxItems {
		trim()
	}

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

func handleSaveToFile() {
	timer := time.NewTimer(time.Second * 5)
	do := false

	for {
		select {
		case <-saveFileChan:
			timer.Reset(time.Second * 5)
			do = true
		case <-timer.C:
			if do {
				saveToFile()
				do = false
			}
		}
	}
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
		slog.Error(Name, "create image file", err)
		return ""
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

func getMimetypes() []string {
	cmd := exec.Command("wl-paste", "--list-types")

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "getMimetypes", err, "output", string(out))
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

	if len(availableModes) > 2 {
		i := slices.Index(availableModes, currentMode)
		i = i + 1

		if i == len(availableModes) {
			i = 0
		}

		actions = append(actions, availableModes[i])
	}

	if len(clipboardhistory) == 0 {
		actions = []string{}
	} else {
		actions = append(actions, ActionRemoveAll)
	}

	if paused.Load() {
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
