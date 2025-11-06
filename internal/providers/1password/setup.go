package main

import (
	"fmt"
	"log/slog"
	"net"
	"os/exec"
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
	Vaults        []string `koanf:"vaults" desc:"vaults to index" default:"[\"personal\"]"`
	Notify        bool     `koanf:"notify" desc:"notify after copying" default:"true"`
	ClearAfter    int      `koanf:"clear_after" desc:"clearboard will be cleared after X seconds. 0 to disable." default:"5"`
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
)

func Activate(single bool, identifier, action string, query string, args string, format uint8, conn net.Conn) {
	switch action {
	case ActionCopyPassword:
		toRun := "wl-copy $(op item get %VALUE% --fields password --reveal)"

		if config.Notify {
			toRun = fmt.Sprintf("%s && %s", toRun, "notify-send copied")
		}

		if config.ClearAfter > 0 {
			toRun = fmt.Sprintf("%s && sleep %d && wl-copy --clear", toRun, config.ClearAfter)
		}

		cmd := common.ReplaceResultOrStdinCmd(toRun, identifier)

		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "copy password", err)
			return
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	case ActionCopyUsername:
		res := ""

		for _, v := range cachedItems {
			if v.ID == identifier {
				res = v.AdditionalInformation
			}
		}

		cmd := common.ReplaceResultOrStdinCmd("wl-copy", res)
		err := cmd.Start()
		if err != nil {
			slog.Error(Name, "copy username", err)
			return
		} else {
			go func() {
				cmd.Wait()
			}()
		}
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	for k, v := range cachedItems {
		e := &pb.QueryResponse_Item{
			Identifier: v.ID,
			Text:       v.Title,
			Subtext:    v.AdditionalInformation,
			Icon:       config.Icon,
			Provider:   Name,
			Actions:    []string{ActionCopyUsername, ActionCopyPassword},
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

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))

	return entries
}

func Icon() string {
	return config.Icon
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}
