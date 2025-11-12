package main

import (
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}
	actions := []string{ActionOpen, ActionOpenDir, ActionCopyFile, ActionCopyPath}

	results := getFilesByQuery(query, exact)

	for k, v := range results {
		p := v.Path
		pt := util.PreviewTypeFile

		for _, i := range config.IgnorePreviews {
			if strings.HasPrefix(v.Path, i.Path) {
				p = i.Placeholder
				pt = util.PreviewTypeText
				break
			}
		}

		entry := &pb.QueryResponse_Item{
			Identifier:  v.Identifier,
			Text:        v.Path,
			Preview:     p,
			PreviewType: pt,
			Type:        pb.QueryResponse_REGULAR,
			Subtext:     "",
			Score:       int32(1000000000 - k),
			Provider:    Name,
			Actions:     actions,
		}

		if hasLocalsend && !strings.HasSuffix(p, "/") {
			entry.Actions = append(entry.Actions, ActionLocalsend)
		}

		if query != "" {
			score, pos, start := common.FuzzyScore(query, v.Path, exact)
			entry.Score = score
			entry.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Start:     start,
				Field:     "text",
				Positions: pos,
			}
		}

		entries = append(entries, entry)
	}

	slog.Debug(Name, "query", time.Since(start))

	return entries
}
