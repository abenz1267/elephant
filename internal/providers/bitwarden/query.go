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
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

type RbwItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	User   string `json:"user"`
	Folder string `json:"folder"`
}

func initItems() {
	cachedItems = nil
	cmd := exec.Command("rbw", "list", "--raw")
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error(Name, "init", err, "msg", output)
		return
	}

	if err := json.Unmarshal(output, &cachedItems); err != nil {
		slog.Error(Name, "parse", err, "msg", output)
		return
	}
}

func Query(conn net.Conn, query string, single bool, exact bool, format uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}

	for k, v := range cachedItems {
		var subtexts []string
		actions := []string{ActionCopyPassword, ActionCopyTotp, ActionSyncVault}
		if config.AutoTypeSupport {
			actions = append(actions, []string{ActionTypePassword, ActionTypeTotp}...)
		}

		if v.User != "" {
			subtexts = append(subtexts, fmt.Sprintf("User: %s", v.User))
			actions = append(actions, ActionCopyUsername)

			if config.AutoTypeSupport {
				actions = append(actions, ActionTypeUsername)
			}
		}

		if v.Folder != "" {
			subtexts = append(subtexts, fmt.Sprintf("Folder: %s", v.Folder))
		}

		e := &pb.QueryResponse_Item{
			Identifier: v.ID,
			Text:       v.Name,
			Subtext:    strings.Join(subtexts, ", "),
			Icon:       config.Icon,
			Provider:   Name,
			Actions:    actions,
			Score:      int32(100_000 - k),
		}

		if query != "" {
			score, positions, start := common.FuzzyScore(query, v.Name, exact)

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
