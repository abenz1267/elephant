package main

import (
	"log/slog"
	"strings"
	"time"

	"github.com/abenz1267/elephant/pkg/common"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

func Query(qid uint32, iid uint32, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	start := time.Now()

	initialCap := len(paths)

	if query != "" {
		initialCap = min(initialCap/10, 1000)
		results.GetData(query, qid, iid, exact)
	}

	entries := make([]*pb.QueryResponse_Item, 0, initialCap)

	if query != "" {
		for k, v := range paths {
			score, positions, s := common.FuzzyScore(query, v.path, exact)
			if score > 0 {
				entries = append(entries, &pb.QueryResponse_Item{
					Identifier: k,
					Text:       v.path,
					Type:       pb.QueryResponse_REGULAR,
					Subtext:    "",
					Provider:   Name,
					Score:      score,
					Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
						Start:     s,
						Field:     "text",
						Positions: positions,
					},
				})
			}
		}
	} else {
		for k, v := range paths {
			if !strings.HasSuffix(k, "/") {
				score := calcScore(v.changed, start)
				entries = append(entries, &pb.QueryResponse_Item{
					Identifier: k,
					Text:       v.path,
					Type:       pb.QueryResponse_REGULAR,
					Subtext:    "",
					Provider:   Name,
					Score:      score,
					Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
						Start:     0,
						Field:     "text",
						Positions: nil,
					},
				})
			}
		}
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
