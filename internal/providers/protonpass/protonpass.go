package main

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"time"
)

// --- Parse-time structs ---
// These mirror the JSON returned by `pass-cli item list --output json`.
// They are only used inside initItems() and are not kept in memory afterward.

// passItemList is the top-level wrapper object returned by `pass-cli item list`.
type passItemList struct {
	Items []passRawItem `json:"items"`
}

// passRawItem is one entry in the listing.
// We capture share_id so we can address this item in later commands without
// relying on pass-cli's configured default vault.
type passRawItem struct {
	ID      string         `json:"id"`
	ShareID string         `json:"share_id"`
	Content passRawContent `json:"content"`
	State   string         `json:"state"` // "Active", "Trashed", etc.
}

// passRawContent holds the item's title and type-specific payload.
type passRawContent struct {
	Title   string       `json:"title"`
	Content passRawInner `json:"content"`
}

// passRawInner is a tagged union: the JSON object uses item-type strings as
// keys ("Login", "Note", "CreditCard", …). We only care about "Login".
// Using a pointer means json.Unmarshal leaves Login as nil when the key is
// absent, so we can skip non-login items without extra error handling.
type passRawInner struct {
	Login *passRawLogin `json:"Login"`
}

// passRawLogin holds the credential fields for a login item.
// Password is present in the listing JSON but is NOT forwarded to the cached
// struct — see the security note on ProtonPassItem below.
type passRawLogin struct {
	Email    string   `json:"email"`
	Username string   `json:"username"`
	Password string   `json:"password"` // parsed here, then discarded
	URLs     []string `json:"urls"`
	TOTPUri  string   `json:"totp_uri"` // non-empty means item has a TOTP field
}

// --- Cached struct ---
// ProtonPassItem is what we actually keep in memory after parsing.
//
// Security note: pass-cli item list returns all passwords in the listing JSON
// (unlike 1password's `op`, which only returns metadata). We intentionally do
// not store the password here. Passwords are fetched one at a time via
// `pass-cli item view --field password` only when the user requests them,
// limiting how long plaintext credentials live in the process heap.
type ProtonPassItem struct {
	ID      string
	ShareID string // vault identifier; required to address this item explicitly
	Title   string
	User    string // email if non-empty, otherwise username
	URL     string // first URL from the item, shown as subtext hint
	HasTOTP bool   // true if totp_uri was non-empty at parse time
}

// checkAvailable blocks until `pass-cli test` exits 0, meaning the user is
// authenticated. It retries every second so elephant waits gracefully instead
// of crashing if the CLI session hasn't started yet.
func checkAvailable() {
	for {
		cmd := exec.Command("pass-cli", "test")

		if err := cmd.Run(); err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		return
	}
}

// initItems populates cachedItems by calling `pass-cli item list` once per
// configured vault.
//
// If config.Vaults is empty, a single call is made with no vault argument,
// which uses pass-cli's configured default vault. This lets users with a
// single vault skip the vaults config entirely.
func initItems() {
	checkAvailable()

	cachedItems = []ProtonPassItem{}

	// If no vaults are configured, treat a single empty string as "use
	// pass-cli's default" — the loop still runs exactly once.
	vaults := config.Vaults
	if len(vaults) == 0 {
		vaults = []string{""}
	}

	for _, vault := range vaults {
		// The vault name is a positional argument that must appear before any
		// flags: `pass-cli item list [VAULT_NAME] [--output FORMAT]`.
		// We build the args slice conditionally to avoid passing an empty string
		// as the vault name.
		args := []string{"item", "list", "--output", "json"}
		if vault != "" {
			args = []string{"item", "list", vault, "--output", "json"}
		}

		cmd := exec.Command("pass-cli", args...)

		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error(Name, "init", err, "msg", string(output))
			continue
		}

		var list passItemList
		if err := json.Unmarshal(output, &list); err != nil {
			slog.Error(Name, "parse", err, "msg", string(output))
			continue
		}

		for _, raw := range list.Items {
			// Skip trashed/deleted items and any non-login types (notes,
			// credit cards, aliases, SSH keys, …).
			if raw.State != "Active" || raw.Content.Content.Login == nil {
				continue
			}

			login := raw.Content.Content.Login

			// Use email as the display username when present; fall back to
			// the username field. Proton Pass treats them as separate fields.
			user := login.Email
			if user == "" {
				user = login.Username
			}

			// Take only the first URL; items can have many but we only need
			// one for the subtext hint in the search results.
			url := ""
			if len(login.URLs) > 0 {
				url = login.URLs[0]
			}

			cachedItems = append(cachedItems, ProtonPassItem{
				ID:      raw.ID,
				ShareID: raw.ShareID,
				Title:   raw.Content.Title,
				User:    user,
				URL:     url,
				HasTOTP: login.TOTPUri != "",
				// Password is intentionally not stored here.
			})
		}
	}
}
