package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"time"

	_ "embed"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name        = "protonpass"
	NamePretty  = "Proton Pass"
	config      *Config
	cachedItems []ProtonPassItem // populated by initItems(), read by Query() and Activate()
)

var readme string

type Config struct {
	common.Config `koanf:",squash"`
	// Vaults lists the vault names to index. Matches the 1password convention.
	// Leave empty to use whichever vault pass-cli has set as default.
	Vaults     []string `koanf:"vaults" desc:"vault names to index" default:"[\"Personal\"]"`
	Notify     bool     `koanf:"notify" desc:"send a desktop notification after copying" default:"true"`
	ClearAfter int      `koanf:"clear_after" desc:"clear the clipboard after X seconds (0 to disable)" default:"5"`
}

func LoadConfig() {
	config = &Config{
		Config: common.Config{
			Icon:     "proton-pass",
			MinScore: 20,
		},
		Vaults:     []string{"Personal"},
		Notify:     true,
		ClearAfter: 5,
	}

	common.LoadConfig(Name, config)
}

func Setup() {
	LoadConfig()

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	initItems()
}

// Available reports whether pass-cli is installed. Elephant calls this before
// loading the plugin; returning false disables the provider entirely.
func Available() bool {
	p, err := exec.LookPath("pass-cli")
	if p == "" || err != nil {
		slog.Info(Name, "available", "pass-cli not found.")
		return false
	}

	return true
}

func PrintDoc(write bool) {
	if !write {
		fmt.Println(readme)
		fmt.Println()
	}

	util.PrintConfig(config, Name, write)
}

const (
	ActionCopyPassword = "copy_password"
	ActionCopyUsername = "copy_username"
	ActionCopy2FA      = "copy_2fa"
)

// notifyAndClear optionally sends a desktop notification and then waits
// ClearAfter seconds before wiping the clipboard. It is always called as
// `go notifyAndClear(msg)` so it runs in the background without blocking Activate.
func notifyAndClear(msg string) {
	if config.Notify {
		exec.Command("notify-send", "-i", "proton-pass", "Proton Pass", msg).Run()
	}

	if config.ClearAfter > 0 {
		// time.Duration is an int64 of nanoseconds, so we must multiply by
		// time.Second to get the right unit.
		time.Sleep(time.Duration(config.ClearAfter) * time.Second)
		exec.Command("wl-copy", "--clear").Run()
	}
}

// findItem looks up a cached item by its ID. Returns a zero-value and false
// if not found. Using a named return for the bool makes the call sites read
// naturally: `if item, ok := findItem(id); ok { … }`.
func findItem(id string) (ProtonPassItem, bool) {
	for _, v := range cachedItems {
		if v.ID == id {
			return v, true
		}
	}

	return ProtonPassItem{}, false
}

// Activate is called when the user selects an action on a result.
// identifier is the item ID (set as Identifier in Query's response).
// action is one of the ActionCopy* constants above.
func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionCopyPassword:
		item, ok := findItem(identifier)
		if !ok {
			slog.Error(Name, "copy password", "item not found in cache", "id", identifier)
			return
		}

		// Fetch the password on-demand rather than reading it from cache.
		// We pass --share-id so the command is unambiguous across vaults.
		cmd := exec.Command(
			"pass-cli", "item", "view",
			"--share-id", item.ShareID,
			"--item-id", identifier,
			"--field", "password",
		)

		// cmd.Output() captures stdout only; stderr goes to /dev/null.
		// If the command fails, the error describes why.
		output, err := cmd.Output()
		if err != nil {
			slog.Error(Name, "get password", err)
			exec.Command("notify-send", "error fetching password.").Run()
			return
		}

		// The field output includes a trailing newline
		password := strings.TrimSpace(string(output))

		// "--" signals end of flags; prevents passwords starting with "-"
		// from being misinterpreted as wl-copy flags.
		if err := exec.Command("wl-copy", "--sensitive", "--", password).Run(); err != nil {
			slog.Error(Name, "copy password", err)
			exec.Command("notify-send", "-i", "proton-pass", "Proton Pass", "error copying password.").Run()
			return
		}

		go notifyAndClear("Password copied — " + item.Title)

	case ActionCopyUsername:
		item, ok := findItem(identifier)
		if !ok {
			slog.Error(Name, "copy username", "item not found in cache", "id", identifier)
			return
		}

		if err := exec.Command("wl-copy", "--", item.User).Run(); err != nil {
			slog.Error(Name, "copy username", err)
			exec.Command("notify-send", "-i", "proton-pass", "Proton Pass", "error copying username.").Run()
			return
		}

		go notifyAndClear("Username copied — " + item.Title)

	case ActionCopy2FA:
		item, ok := findItem(identifier)
		if !ok {
			slog.Error(Name, "copy 2fa", "item not found in cache", "id", identifier)
			return
		}

		cmd := exec.Command(
			"pass-cli", "item", "totp",
			"--share-id", item.ShareID,
			"--item-id", identifier,
			"--output", "json",
		)

		output, err := cmd.Output()
		if err != nil {
			slog.Error(Name, "get totp", err)
			exec.Command("notify-send", "error fetching TOTP.").Run()
			return
		}

		// The TOTP response is a map of field-name → code. Items can have
		// multiple TOTP fields; the standard login one is always keyed "totp".
		// Example: {"totp": "235775", "totp_uri": "235775"}
		var totpData map[string]string
		if err := json.Unmarshal(output, &totpData); err != nil {
			slog.Error(Name, "parse totp", err)
			return
		}

		code, ok := totpData["totp"]
		if !ok || code == "" {
			exec.Command("notify-send", "no TOTP field found on this item.").Run()
			return
		}

		if err := exec.Command("wl-copy", "--sensitive", "--", code).Run(); err != nil {
			slog.Error(Name, "copy totp", err)
			exec.Command("notify-send", "-i", "proton-pass", "Proton Pass", "error copying TOTP.").Run()
			return
		}

		go notifyAndClear("TOTP copied — " + item.Title)
	}
}

func Query(conn net.Conn, query string, runes []rune, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	slab := common.AcquireFuzzySlab()
	defer common.ReleaseFuzzySlab(slab)

	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	// k is the index, v is a copy of the ProtonPassItem at that index.
	for k, v := range cachedItems {
		// First action is the default (Return). Username and 2FA are bound
		// via walker's [providers.actions] config — see README.
		actions := []string{ActionCopyPassword, ActionCopyUsername}
		title := v.Title
		if v.HasTOTP {
			actions = append(actions, ActionCopy2FA)
			title = "[2FA] " + title
		}

		subtext := v.User
		if v.URL != "" {
			if subtext != "" {
				subtext += " — " + v.URL
			} else {
				subtext = v.URL
			}
		}

		e := &pb.QueryResponse_Item{
			Identifier: v.ID,
			Text:       title,
			Subtext:    subtext,
			Icon:       config.Icon,
			Provider:   Name,
			Actions:    actions,
			// Score when no query is set: items appear in list order.
			// 100_000 - k gives earlier items a higher score, preserving
			// the order returned by pass-cli (usually alphabetical).
			Score: int32(100_000 - k),
		}

		if query != "" {
			score, positions, startPos := common.FuzzyScore(runes, v.Title, exact, slab)

			e.Score = score
			e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Start:     startPos,
				Field:     "text",
				Positions: common.FuzzyPositionsToInt32(positions),
			}
		}

		// Drop items that scored below the configured threshold when a query
		// is active. When there's no query, include everything.
		if query == "" || e.Score > config.MinScore {
			entries = append(entries, e)
		}
	}

	slog.Debug(Name, "query", time.Since(start))

	return entries
}

func Icon() string {
	return config.Icon
}

func HideFromProviderlist() bool {
	return config.HideFromProviderlist
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}
