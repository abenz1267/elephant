package main

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/abenz1267/elephant/internal/common"
)

func Query(query string) []common.Entry {
	start := time.Now()
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	entries := []common.Entry{}

	filesMu.RLock()
	filesCopy := files
	filesMu.RUnlock()

	for k, v := range filesCopy {
		if len(v.NotShowIn) != 0 && slices.Contains(v.NotShowIn, desktop) || len(v.OnlyShowIn) != 0 && !slices.Contains(v.OnlyShowIn, desktop) || v.Hidden || v.NoDisplay {
			continue
		}

		// add generic entry
		if config.ShowActions && config.ShowGeneric || !config.ShowActions {

			e := common.Entry{
				Identifier: k,
				Text:       v.Name,
				SubText:    v.GenericName,
				Icon:       v.Icon,
				Provider:   Name,
				Fuzzy: common.FuzzyMatchInfo{
					Field: "text",
				},
			}

			var match string
			match, e.Score, e.Fuzzy.Pos, e.Fuzzy.Start = calcScore(query, &v.Data)

			if match != e.Text {
				e.SubText = match
				e.Fuzzy.Field = "subtext"
			}

			if e.Score > 0 {
				entries = append(entries, e)
			}
		}

		// add actions
		if config.ShowActions {
			for _, a := range v.Actions {

				e := common.Entry{
					Identifier: fmt.Sprintf("%s:%s", k, a.Action),
					Text:       a.Name,
					SubText:    v.Name,
					Icon:       a.Icon,
					Provider:   Name,
					Fuzzy: common.FuzzyMatchInfo{
						Field: "text",
					},
				}

				var match string
				match, e.Score, e.Fuzzy.Pos, e.Fuzzy.Start = calcScore(query, &a)

				if match != e.Text {
					e.SubText = match
					e.Fuzzy.Field = "subtext"
				}

				if e.Score > 0 {
					entries = append(entries, e)
				}
			}
		}
	}

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))

	return entries
}

func calcScore(q string, d *Data) (string, int, *[]int, int) {
	var scoreRes int
	var posRes *[]int
	var startRes int
	var match string
	var modifier int

	for k, v := range []string{d.Name, d.Parent, d.GenericName, strings.Join(d.Keywords, ","), d.Comment} {
		score, pos, start := common.FuzzyScore(q, v)

		if score > scoreRes {
			scoreRes = score
			posRes = pos
			startRes = start
			match = v
			modifier = k
		}
	}

	scoreRes = scoreRes - max((modifier*10), 50)

	return match, scoreRes, posRes, startRes
}
