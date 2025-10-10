package main

import (
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

func Query(conn net.Conn, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	start := time.Now()

	entries := []*pb.QueryResponse_Item{}
	actions := []string{ActionOpen, ActionOpenDir, ActionCopyFile, ActionCopyPath}

	if query != "" {
		paths.Range(func(key, val any) bool {
			k := key.(string)
			v := val.(*file)

			score, positions, s := common.FuzzyScore(query, v.path, exact)
			if score > 0 {
				entries = append(entries, &pb.QueryResponse_Item{
					Identifier: k,
					Text:       v.path,
					Type:       pb.QueryResponse_REGULAR,
					Subtext:    "",
					Provider:   Name,
					Actions:    actions,
					Score:      score,
					Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
						Start:     s,
						Field:     "text",
						Positions: positions,
					},
				})
			}

			return true
		})
	} else {
		paths.Range(func(key, val any) bool {
			k := key.(string)
			v := val.(*file)

			if !strings.HasSuffix(k, "/") {
				score := calcScore(v.changed, start)
				entries = append(entries, &pb.QueryResponse_Item{
					Identifier: k,
					Text:       v.path,
					Type:       pb.QueryResponse_REGULAR,
					Subtext:    "",
					Actions:    actions,
					Provider:   Name,
					Score:      score,
					Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
						Start:     0,
						Field:     "text",
						Positions: nil,
					},
				})
			}

			return true
		})
	}

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))
	return entries
}

func calcScore(v time.Time, now time.Time) int32 {
	if v.IsZero() {
		return 0
	}

	diff := now.Sub(v)

	res := 3600 - diff.Seconds()

	if res < 0 {
		res = 0
	}

	return int32(res)
}
