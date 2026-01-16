package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
)

const (
	ActionCopyUsername = "copyusername"
	ActionCopyPassword = "copypassword"
	ActionCopyTotp     = "copytotp"
	ActionTypeUsername = "typeusername"
	ActionTypePassword = "typepassword"
	ActionTypeTotp     = "typetotp"
	ActionSyncVault    = "syncvault"
)

type RbwLoginItem struct {
	ID     string       `json:"id"`
	Folder string       `json:"folder"`
	Name   string       `json:"name"`
	Data   RbwLoginData `json:"data"`
	Notes  string       `json:"notes"`
}

type RbwLoginData struct {
	Username string    `json:"username"`
	Password *string   `json:"password"`
	Totp     string    `json:"totp"`
	Uris     []RbwUris `json:"uris"`
}

type RbwUris struct {
	Uri       string `json:"uri"`
	MatchType string `json:"match_type"`
}

func syncLocalRbwVault() {
	cmd := exec.Command("rbw", "sync")
	if err := cmd.Run(); err != nil {
		slog.Error(Name, "sync failed", err)
		return
	}

	initItems()
	exec.Command("notify-send", "Vault synced successfully").Run()
}

func getRbwItem(identifier string, action string) *RbwLoginItem {
	cmd := common.ReplaceResultOrStdinCmd("rbw get %VALUE% --full --raw", identifier)
	stdout, stderr := cmd.CombinedOutput()

	if stderr != nil {
		slog.Error(Name, action, stderr)

		exec.Command("notify-send", "Failed to fetch data").Run()
		return nil
	}

	item := &RbwLoginItem{}
	if err := json.Unmarshal(stdout, &item); err != nil {
		slog.Error(Name, "parse", err)
		return nil
	}

	if item.Data.Password == nil {
		exec.Command("notify-send", "Unsupported Item").Run()
		return nil
	}

	return item
}

func copyToClipboard(value string, logStr string) {
	cmd := common.ReplaceResultOrStdinCmd(config.CopyCommand, value)
	err := cmd.Start()
	if err != nil {
		slog.Error(Name, fmt.Sprintf("%s failed", logStr), err)
		return
	}

	go func() {
		cmd.Wait()
		exec.Command("notify-send", fmt.Sprintf("%s succeeded", logStr)).Run()
		clearClipboard()
	}()
}

func clearClipboard() {
	if config.ClearAfter > 0 {
		time.Sleep(time.Duration(config.ClearAfter) * time.Second)
		exec.Command("wl-copy", "--clear").Run()
	}
}

func typeValue(value string, logStr string) {
	if config.AutoTypeDelay > 0 {
		time.Sleep(time.Duration(config.AutoTypeDelay) * time.Millisecond)
	}

	cmd := common.ReplaceResultOrStdinCmd(config.AutoTypeCommand, value)
	err := cmd.Start()
	if err != nil {
		slog.Error(Name, fmt.Sprintf("%s failed", logStr), err)
		return
	}

	go func() {
		cmd.Wait()
	}()
}

func Activate(single bool, identifier, action, query, args string, format uint8, conn net.Conn) {
	if action == ActionSyncVault {
		syncLocalRbwVault()
		return
	}

	item := getRbwItem(identifier, action)
	if item == nil {
		return
	}

	switch action {
	case ActionCopyUsername:
		copyToClipboard(item.Data.Username, "Username copy")
	case ActionCopyPassword:
		copyToClipboard(*item.Data.Password, "Password copy")
	case ActionCopyTotp:
		cmd := common.ReplaceResultOrStdinCmd("rbw totp %VALUE% --clipboard", identifier)

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "copy totp", err)
			return
		}

		go func() {
			err := cmd.Wait()
			if err != nil {
				exec.Command("notify-send", "Entry does not contain totp").Run()
				return
			}

			exec.Command("notify-send", "Totp copied successfully").Run()
			clearClipboard()
		}()
	case ActionTypeUsername:
		typeValue(item.Data.Username, "Typing username")
	case ActionTypePassword:
		typeValue(*item.Data.Password, "Typing password")
	case ActionTypeTotp:
		cmd := common.ReplaceResultOrStdinCmd("rbw totp %VALUE%", identifier)

		output, err := cmd.Output()
		if err != nil {
			exec.Command("notify-send", "Entry does not contain totp").Run()
			return
		}

		value := strings.TrimSpace(string(output[:]))
		typeValue(value, "Typing totp")
	}
}
