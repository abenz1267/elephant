package main

import (
	"encoding/json"
	"log/slog"
	"net"
	"os/exec"

	"github.com/abenz1267/elephant/v2/pkg/common"
)

const (
	ActionCopyUsername = "copyusername"
	ActionCopyPassword = "copypassword"
	ActionCopyTotp = "copytotp"
	ActionTypeUsername = "typeusername"
	ActionTypePassword = "typepassword"
	ActionTypeTotp = "typetotp"
	ActionSyncVault = "syncvault"
)

type RbwLoginItem struct {
	ID string `json:"id"`
	Folder string `json:"folder"`
	Name string `json:"name"`
	Data RbwLoginData `json:"data"`
	Notes string `json:"notes"`
}

type RbwLoginData struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Totp string `json:"totp"`
	Uris []RbwUris `json:"uris"`
}

type RbwUris struct {
	Uri string `json:"uri"`
	MatchType string `json:"match_type"`
}

func Activate(single bool, identifier, action, query, args string, format uint8, conn net.Conn) {
	cmd := common.ReplaceResultOrStdinCmd("rbw get %VALUE% --full --raw", identifier)
	stdout, stderr := cmd.CombinedOutput()

	if stderr != nil {
		slog.Error(Name, action, stderr)

		exec.Command("notify-send", "Failed to fetch data").Run()
		return
	}

	var item RbwLoginItem
	if err := json.Unmarshal(stdout, &item); err != nil {
		slog.Error(Name, "parse", err)
		return
	}

	switch action {
	case ActionCopyUsername:
		cmd := common.ReplaceResultOrStdinCmd("wl-copy", item.Data.Username)
		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "copy username", err)
			return
		}

		go func() {
			cmd.Wait()
		}()
		exec.Command("notify-send", "Username copied successfully").Run()
	case ActionCopyPassword:
		cmd := common.ReplaceResultOrStdinCmd("wl-copy", item.Data.Password)
		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "copy password", err)
			return
		}

		go func() {
			cmd.Wait()
		}()
		exec.Command("notify-send", "Password copied successfully").Run()
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
			} else {
				exec.Command("notify-send", "Totp copied successfully").Run()
			}
		}()
	}
}

