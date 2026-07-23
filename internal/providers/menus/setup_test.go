package main

import (
	"strings"
	"testing"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/junegunn/fzf/src/algo"
)

func TestQueryCustomMenuUsesUndecoratedSearchRunes(t *testing.T) {
	originalMenus := common.Menus
	originalConfig := common.MenuConfigLoaded
	t.Cleanup(func() {
		common.Menus = originalMenus
		common.MenuConfigLoaded = originalConfig
	})

	common.Menus = map[string]*common.Menu{
		"apps": {
			Name:       "apps",
			NamePretty: "Applications",
			Entries: []common.Entry{
				{
					Identifier: "apps:firefox",
					Menu:       "apps",
					Text:       "Firefox",
				},
			},
		},
	}
	common.MenuConfigLoaded.Config.MinScore = 0

	tests := []struct {
		name  string
		query string
		exact bool
	}{
		{name: "fuzzy", query: "apps:fire"},
		{name: "exact", query: "apps:Fire", exact: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, runeQuery, _ := strings.Cut(tt.query, ":")
			if !tt.exact {
				runeQuery = strings.ToLower(runeQuery)
			}

			runes := algo.NormalizeRunes([]rune(runeQuery))
			results := Query(nil, tt.query, runes, true, tt.exact, 0)

			if len(results) != 1 {
				t.Fatalf("Query() returned %d results, want 1", len(results))
			}
			if results[0].Text != "Firefox" {
				t.Fatalf("Query() returned %q, want Firefox", results[0].Text)
			}
		})
	}
}
