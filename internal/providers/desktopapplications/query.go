package main

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/abenz1267/elephant/internal/common"
	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/pkg/pb/pb"
)

var results = providers.QueryData{}

func Query(qid uint32, iid uint32, query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	start := time.Now()
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	entries := make([]*pb.QueryResponse_Item, 0, len(files)*2) // Estimate for entries + action

	isSub := qid >= 100_000_000

	if !isSub && query != "" {
		results.GetData(query, qid, iid, exact)
	}

	alias := ""
	if val, ok := config.Aliases[query]; ok {
		alias = val
	}

	for k, v := range files {
		if len(v.NotShowIn) != 0 && slices.Contains(v.NotShowIn, desktop) || len(v.OnlyShowIn) != 0 && !slices.Contains(v.OnlyShowIn, desktop) || v.Hidden || v.NoDisplay {
			continue
		}

		// check generic
		if k == alias {
			entries = append(entries, &pb.QueryResponse_Item{
				Identifier: k,
				Text:       v.Name,
				Type:       pb.QueryResponse_REGULAR,
				Subtext:    v.GenericName,
				Icon:       v.Icon,
				Provider:   Name,
				Score:      1_000_000,
			})
			continue
		}

		var match string
		var ok bool
		var score int32
		var positions []int32
		var fs int32
		field := "text"
		subtext := v.GenericName

		if query != "" {
			match, score, positions, fs, ok = calcScore(query, &v.Data, exact)

			if ok && match != v.Name {
				subtext = match
				field = "subtext"
			}
		}

		var usageScore int32
		if config.History && score > config.MinScore || (query == "" && config.HistoryWhenEmpty) {
			usageScore = h.CalcUsageScore(query, k)
			score = score + usageScore
		}

		if query == "" {
			i := slices.Index(config.Pinned, k)

			if i != -1 {
				score = 1000000 - int32(i)
				usageScore = score
			}
		}

		if usageScore != 0 || config.ShowActions && config.ShowGeneric || !config.ShowActions || (config.ShowActions && len(v.Actions) == 0) || query == "" {
			if score >= config.MinScore || query == "" {
				entries = append(entries, &pb.QueryResponse_Item{
					Identifier: k,
					Text:       v.Name,
					Type:       pb.QueryResponse_REGULAR,
					Subtext:    subtext,
					Icon:       v.Icon,
					Provider:   Name,
					Score:      score,
					Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
						Start:     fs,
						Field:     field,
						Positions: positions,
					},
				})
			}
		}

		// check actions
		for _, a := range v.Actions {
			identifier := fmt.Sprintf("%s:%s", k, a.Action)

			if identifier == alias {
				entries = append(entries, &pb.QueryResponse_Item{
					Identifier: identifier,
					Score:      1_000_000,
					Text:       a.Name,
					Type:       pb.QueryResponse_REGULAR,
					Subtext:    v.Name,
					Icon:       a.Icon,
					Provider:   Name,
				})
				continue
			}

			var match string
			var ok bool
			var score int32
			var positions []int32
			var fs int32
			field := "text"
			subtext := v.Name

			if query != "" {
				match, score, positions, fs, ok = calcScore(query, &a, exact)

				if ok && match != a.Name {
					subtext = match
					field = "subtext"
				}
			}

			var usageScore int32
			if config.History {
				if score > config.MinScore || query == "" && config.HistoryWhenEmpty {
					usageScore = h.CalcUsageScore(query, identifier)
					score = score + usageScore
				}
			}

			if query == "" {
				i := slices.Index(config.Pinned, identifier)

				if i != -1 {
					score = 1000000 - int32(i)
					usageScore = score
				}
			}

			if (query == "" && config.ShowActionsWithoutQuery) || (query != "" && config.ShowActions) || usageScore != 0 {
				if score >= config.MinScore || query == "" {
					entries = append(entries, &pb.QueryResponse_Item{
						Identifier: identifier,
						Score:      score,
						Text:       a.Name,
						Type:       pb.QueryResponse_REGULAR,
						Subtext:    subtext,
						Icon:       a.Icon,
						Provider:   Name,
						Fuzzyinfo: &pb.QueryResponse_Item_FuzzyInfo{
							Start:     fs,
							Field:     field,
							Positions: positions,
						},
					})
				}
			}
		}
	}

	if !isSub {
		slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))
	}

	return entries
}

func calcScore(q string, d *Data, exact bool) (string, int32, []int32, int32, bool) {
	var scoreRes int32
	var posRes []int32
	var startRes int32
	var match string
	var modifier int32

	for k, v := range []string{d.Name, d.Parent, d.GenericName, strings.Join(d.Keywords, ","), d.Comment} {
		score, pos, start := common.FuzzyScore(q, v, exact)

		if score > scoreRes {
			scoreRes = score
			posRes = pos
			startRes = start
			match = v
			modifier = int32(k)
		}
	}

	if scoreRes == 0 {
		return "", 0, nil, 0, false
	}

	scoreRes = max(scoreRes-min(modifier*5, 50)-startRes, 10)

	return match, scoreRes, posRes, startRes, true
}
