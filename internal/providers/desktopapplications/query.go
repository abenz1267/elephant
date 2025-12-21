package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var desktop = os.Getenv("XDG_CURRENT_DESKTOP")

func Query(conn net.Conn, query string, _ bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := make([]*pb.QueryResponse_Item, 0, len(files)*2) // Estimate for entries + action

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
			actions := []string{ActionStart}

			if config.WindowIntegration {
				actions = append(actions, ActionNewInstance)
			}

			entries = append(entries, &pb.QueryResponse_Item{
				Identifier: k,
				Text:       v.Name,
				Type:       pb.QueryResponse_REGULAR,
				Subtext:    v.GenericName,
				Icon:       v.Icon,
				Actions:    actions,
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

		pinned := false

		hasWindow := false

		if config.WindowIntegration {
			_, hasWindow = appHasWindow(v)
		}

		if !config.IgnorePinWithWindow || config.IgnorePinWithWindow && !hasWindow {
			pinsMu.RLock()
			if query == "" {
				index := slices.Index(pins, k)

				if index != -1 {
					pinned = true
					score = 1000000 - int32(index)
				}
			}
			pinsMu.RUnlock()
		}

		if score != 0 || usageScore != 0 || config.ShowActions && config.ShowGeneric || !config.ShowActions || (config.ShowActions && len(v.Actions) == 0) || query == "" {
			if score >= config.MinScore || query == "" {
				state := []string{}
				a := []string{ActionStart}

				if config.WindowIntegration {
					a = append(a, ActionNewInstance)
				}

				if usageScore != 0 {
					state = append(state, "history")
					a = append(a, history.ActionDelete)
				}

				if pinned {
					state = append(state, "pinned")
				} else {
					state = append(state, "unpinned")
				}

				pinsMu.RLock()
				if slices.Contains(pins, k) {
					a = append(a, ActionUnpin)

					i := slices.Index(pins, k)

					if i != 0 {
						a = append(a, ActionPinUp)
					}

					if i != len(pins)-1 {
						a = append(a, ActionPinDown)
					}
				} else {
					a = append(a, ActionPin)
				}
				pinsMu.RUnlock()

				if query != "" && config.WindowIntegration && config.ScoreOpenWindows && hasWindow {
					score = int32(score / 2)
				}

				entries = append(entries, &pb.QueryResponse_Item{
					Identifier: k,
					Text:       v.Name,
					Type:       pb.QueryResponse_REGULAR,
					Subtext:    subtext,
					Actions:    a,
					Icon:       v.Icon,
					State:      state,
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
		if config.ShowActions {
			for _, a := range v.Actions {
				identifier := fmt.Sprintf("%s:%s", k, a.Action)

				actions := []string{ActionStart}

				if config.WindowIntegration && !config.WindowIntegrationIgnoreActions {
					actions = append(actions, ActionNewInstance)
				}

				if identifier == alias {
					entries = append(entries, &pb.QueryResponse_Item{
						Identifier: identifier,
						Score:      1_000_000,
						Actions:    actions,
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

					if config.ActionMinScore > 0 {
						if score < config.MinScore {
							continue
						}
					}
				}

				var usageScore int32

				if config.History {
					if score > config.MinScore || query == "" && config.HistoryWhenEmpty {
						usageScore = h.CalcUsageScore(query, identifier)
						score = score + usageScore
					}
				}

				pinned := false

				hasWindow := false

				if config.WindowIntegration {
					_, hasWindow = appHasWindow(v)
				}

				if !config.IgnorePinWithWindow || config.IgnorePinWithWindow && !hasWindow {
					pinsMu.RLock()
					if query == "" {
						i := slices.Index(pins, identifier)

						if i != -1 {
							pinned = true
							score = 1000000 - int32(i)
						}
					}
					pinsMu.RUnlock()
				}

				if (query == "" && config.ShowActionsWithoutQuery) || query != "" || usageScore != 0 || score != 0 {
					if score >= config.MinScore || query == "" {
						state := []string{}

						if usageScore != 0 {
							state = append(state, "history")
							actions = append(actions, history.ActionDelete)
						}

						pinsMu.RLock()
						if pinned {
							state = append(state, "pinned")
							actions = append(actions, ActionUnpin)

							i := slices.Index(pins, identifier)

							if i != 0 {
								actions = append(actions, ActionPinUp)
							}

							if i != len(pins)-1 {
								actions = append(actions, ActionPinDown)
							}
						} else {
							state = append(state, "unpinned")
							actions = append(actions, ActionPin)
						}
						pinsMu.RUnlock()

						entries = append(entries, &pb.QueryResponse_Item{
							Identifier: identifier,
							Score:      score,
							Actions:    actions,
							Text:       a.Name,
							Type:       pb.QueryResponse_REGULAR,
							State:      state,
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

	}

	slog.Debug(Name, "query", time.Since(start))

	return entries
}

func calcScore(q string, d *Data, exact bool) (string, int32, []int32, int32, bool) {
	var scoreRes int32
	var posRes []int32
	var startRes int32
	var match string
	var modifier int32

	toSearch := []string{d.Name}
	if !config.OnlySearchTitle {
		toSearch = []string{d.Name, d.Exec, d.Parent, d.GenericName, strings.Join(d.Keywords, ","), d.Comment}
	}

	for k, v := range toSearch {
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
