package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/common/history"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"github.com/tidwall/gjson"
)

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}
	prefix, query := splitEnginePrefix(query)

	if likelyAddress(query) && prefix == "" {
		address := query
		if !strings.Contains(address, "://") {
			address = fmt.Sprintf("https://%s", query)
		}

		addressEntry := &pb.QueryResponse_Item{
			Identifier: "websearch",
			Text:       fmt.Sprintf("visit: %s", address),
			Actions:    []string{"open_url"},
			Icon:       Icon(),
			Provider:   Name,
			Score:      1_000_000,
		}
		entries = append(entries, addressEntry)
	}

	// TODO: re-add support for engines as actions

	if query == "" && prefix == "" {
		entries = append(entries, queryEmpty(single, exact)...)
	} else {
		entries = append(entries, queryEngines(prefix, query, single, exact)...)
	}

	// force search to be first when queried with prefix
	if prefix != "" {
		for _, entry := range entries {
			entry.Score += 10_000
		}
	}

	return entries
}

func likelyAddress(query string) bool {
	if !strings.Contains(query, ".") &&
		!strings.Contains(query, "://") {
		return false
	}

	if strings.Contains(query, " ") ||
		strings.HasSuffix(query, ".") ||
		strings.HasPrefix(query, ".") {
		return false
	}

	if !strings.Contains(query, "://") {
		query = fmt.Sprintf("https://%s", query)
	}

	_, err := url.Parse(query)

	return err == nil
}

func queryEmpty(single bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	entries = append(entries, listEngines("", single, exact)...)

	// TODO Add configurable empty behavior
	// TODO List Recent Browser History
	// TODO List History Browser by frecency
	// TODO List Bookmarks/Pins

	return entries
}

func queryEngines(prefix string, query string, single bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	queriedEngines := []Engine{}
	if prefix == "" {
		for _, engine := range config.Engines {
			if (!single && engine.Default) || (single && engine.DefaultSingle) {
				queriedEngines = append(queriedEngines, engine)
			}
		}
	} else {
		for _, engine := range config.Engines {
			if engine.Prefix == prefix {
				queriedEngines = append(queriedEngines, engine)
			}
		}
	}

	// Add direct search entry
	for i, engine := range queriedEngines {
		text := engine.Name
		subtext := ""
		if query != "" {
			text = config.TextPrefix + query
			subtext = engine.Name
		}

		// Force search above finder when single
		score := int32(len(queriedEngines) - i)
		if single && engine.DefaultSingle {
			score += 1_000
		}

		entries = append(entries, &pb.QueryResponse_Item{
			Identifier: engine.Name,
			Text:       text,
			Subtext:    subtext,
			Actions:    []string{"search"},
			Icon:       engine.Icon,
			Provider:   Name,
			Score:      score,
			Type:       0,
		})
	}

	// Add Suggestion Entries
	if single || prefix != "" {
		entries = append(entries, getAPISuggestions(queriedEngines, prefix, query, single)...)
	}

	// TODO: Add local browser history based suggestions

	// Engines finder
	isPrefix := prefix == config.EngineFinderPrefix && prefix != ""
	isDefault := prefix == "" && !single && config.EngineFinderDefault
	isDefaultSingle := prefix == "" && single && config.EngineFinderDefault
	if isPrefix || isDefault || isDefaultSingle {
		entries = append(entries, listEngines(query, single, exact)...)
	}

	return entries
}

func getAPISuggestions(queriedEngines []Engine, prefix string, query string, single bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	// Perform requests
	allSuggestions := []Suggestion{}
	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	for engineIndex, engine := range queriedEngines {
		if query == "" || engine.SuggestionsURL == "" {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			suggestions, err := fetchApiSuggestions(
				engine.SuggestionsURL,
				query,
				engine.SuggestionsPath,
			)
			if err != nil {
				slog.Warn("failed to fetch suggestions", "engine", engine.Name, "error", err)
				return
			}

			local := make([]Suggestion, 0, len(suggestions))
			for i, content := range suggestions {
				identifier := engine.Name + ":" + content

				local = append(local, Suggestion{
					Identifier: identifier,
					Content:    content,
					Engine:     engine,
					Score:      int32(-1000 - i - engineIndex),
				})
			}

			mu.Lock()
			allSuggestions = append(allSuggestions, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Deduplicate and add entries
	currentSuggestionsMutex.Lock()
	currentSuggestions = make(map[string]Suggestion) // remove old suggestions

	sort.Slice(allSuggestions, func(i, j int) bool {
		return allSuggestions[i].Score > allSuggestions[j].Score
	})
	seenSuggestions := make(map[string]bool)
	seenSuggestions[strings.ToLower(strings.TrimSpace(query))] = true
	count := 0
	for _, s := range allSuggestions {
		if count >= config.MaxApiItems {
			break
		}

		normalized := strings.ToLower(strings.TrimSpace(s.Content))
		if !seenSuggestions[normalized] {
			newEntry := &pb.QueryResponse_Item{
				Identifier: s.Identifier,
				Text:       s.Content,
				Icon:       s.Engine.Icon,
				Subtext:    s.Engine.Name,
				Actions:    []string{"search_suggestion"},
				Provider:   Name,
				Score:      s.Score,
				Type:       0,
			}

			entries = append(entries, newEntry)
			currentSuggestions[s.Identifier] = s
			seenSuggestions[normalized] = true
			count += 1
		}
	}
	currentSuggestionsMutex.Unlock()

	return entries
}

func listEngines(query string, _ bool, exact bool) []*pb.QueryResponse_Item {
	entries := []*pb.QueryResponse_Item{}

	for k, v := range config.Engines {
		text := v.Name
		if v.Prefix != "" {
			text = fmt.Sprintf("%s ( %s )", v.Name, v.Prefix)
		}

		e := &pb.QueryResponse_Item{
			Identifier: v.Name,
			Text:       text,
			Subtext:    "",
			Actions:    []string{"search"},
			Icon:       v.Icon,
			Provider:   Name,
			Score:      int32(len(config.Engines) - k),
			Type:       0,
		}

		if query != "" {
			score, pos, start := common.FuzzyScore(query, v.Name, exact)

			e.Score = score
			e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
				Field:     "text",
				Positions: pos,
				Start:     start,
			}
		}

		var usageScore int32
		if config.History {
			if e.Score > config.MinScore || query == "" && config.HistoryWhenEmpty {
				usageScore = h.CalcUsageScore(query, e.Identifier)

				if usageScore != 0 {
					e.State = append(e.State, "history")
					e.Actions = append(e.Actions, history.ActionDelete)
				}

				e.Score = e.Score + usageScore
			}
		}

		if e.Score > config.MinScore || query == "" {
			entries = append(entries, e)
		}
	}

	return entries
}

func fetchApiSuggestions(address string, query string, jsonPath string) ([]string, error) {
	request := expandSubstitutions(address, query)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.SuggestionsTimeout)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", request, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result := gjson.Get(string(body), jsonPath)

	var suggestions []string
	if result.IsArray() {
		for _, item := range result.Array() {
			suggestions = append(suggestions, item.String())
		}
	} else {
		suggestions = append(suggestions, result.String())
	}

	return suggestions, nil
}
