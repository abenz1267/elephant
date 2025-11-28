package main

import (
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
	Name        = "1password"
	NamePretty  = "1Password"
	config      *Config
	cachedItems []OpItem
)

//go:embed README.md
var readme string

type Config struct {
	common.Config `koanf:",squash"`
	Vaults        []string          `koanf:"vaults" desc:"vaults to index" default:"[\"personal\"]"`
	Notify        bool              `koanf:"notify" desc:"notify after copying" default:"true"`
	ClearAfter    int               `koanf:"clear_after" desc:"clearboard will be cleared after X seconds. 0 to disable." default:"5"`
	CategoryIcons map[string]string `koanf:"category_icons" desc:"icon mapping by category"`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "1password",
			MinScore: 20,
		},
		Notify:     true,
		ClearAfter: 5,
	}

	common.LoadConfig(Name, config)

	if config.NamePretty != "" {
		NamePretty = config.NamePretty
	}

	if len(config.Vaults) == 0 {
		slog.Error(Name, "config", "no vaults")
		return
	}

	initItems()
}

func Available() bool {
	p, err := exec.LookPath("op")
	if p == "" || err != nil {
		slog.Info(Name, "available", "1password cli not found.")
		return false
	}

	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

const (
	ActionCopyPassword = "copy_password"
	ActionCopyUsername = "copy_username"
	ActionCopy2FA      = "copy_2fa"
)

func notifyAndClear() {
	if config.Notify {
		exec.Command("notify-send", "copied").Run()
	}

	if config.ClearAfter > 0 {
		time.Sleep(time.Duration(config.ClearAfter))
		exec.Command("wl-copy", "--clear")
	}
}

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionCopyPassword:
		toRun := "wl-copy $(op item get %VALUE% --fields password --reveal)"

		cmd := common.ReplaceResultOrStdinCmd(toRun, identifier)

		err := cmd.Run()
		if err != nil {
			slog.Error(Name, "copy password", err)
			exec.Command("notify-send", "error copying password.").Run()
		} else {
			notifyAndClear()
		}
	case ActionCopyUsername:
		res := ""

		for _, v := range cachedItems {
			if v.ID == identifier {
				res = v.AdditionalInformation
			}
		}

		cmd := common.ReplaceResultOrStdinCmd("wl-copy", res)

		err := cmd.Run()
		if err != nil {
			slog.Error(Name, "copy username", err)
			exec.Command("notify-send", "error copying username.").Run()
		} else {
			notifyAndClear()
		}
	case ActionCopy2FA:
		toRun := "wl-copy $(op item get %VALUE% --otp)"

		cmd := common.ReplaceResultOrStdinCmd(toRun, identifier)

		err := cmd.Run()
		if err != nil {
			slog.Error(Name, "copy 2fa", err)
			exec.Command("notify-send", "error copying OTP.").Run()
		} else {
			notifyAndClear()
		}
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	for k, v := range cachedItems {
		icon := config.Icon
		if customIcon, ok := config.CategoryIcons[strings.ToLower(v.Category)]; ok {
			icon = customIcon
		}

		e := &pb.QueryResponse_Item{
			Identifier: v.ID,
			Text:       v.Title,
			Subtext:    v.AdditionalInformation,
			Icon:       icon,
			Provider:   Name,
			Actions:    []string{ActionCopyUsername, ActionCopyPassword, ActionCopy2FA},
			Score:      int32(100_000 - k),
		}

		if query != "" {
			score, positions, start := common.FuzzyScore(query, v.Title, exact)

			e.Score = score
			e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Start:     start,
				Field:     "text",
				Positions: positions,
			}
		}

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
