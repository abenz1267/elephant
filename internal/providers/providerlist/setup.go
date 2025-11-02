package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/abenz1267/elephant/v2/internal/providers"
	"github.com/abenz1267/elephant/v2/internal/util"
	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var (
	Name       = "providerlist"
	NamePretty = "Providerlist"
	config     *Config
)

//go:embed README.md
var readme string

type Config struct {
	common.Config `koanf:",squash"`
	Hidden        []string `koanf:"hidden" desc:"hidden providers" default:"<empty>"`
}

func Setup() {
	config = &Config{
		Config: common.Config{
			Icon:     "applications-other",
			MinScore: 10,
		},
		Hidden: []string{},
	}

	common.LoadConfig(Name, config)
}

func Available() bool {
	return true
}

func PrintDoc() {
	fmt.Println(readme)
	fmt.Println()
	util.PrintConfig(Config{}, Name)
}

func Activate(identifier, action string, query string, args string) {
}

func Query(conn net.Conn, query string, single bool, exact bool, _ uint8) []*pb.QueryResponse_Item {
	start := time.Now()
	entries := []*pb.QueryResponse_Item{}

	for _, v := range providers.Providers {
		if *v.Name == Name {
			continue
		}

		if *v.Name == "menus" {
			for _, v := range common.Menus {
				identifier := fmt.Sprintf("%s:%s", "menus", v.Name)

				if slices.Contains(config.Hidden, identifier) || v.HideFromProviderlist {
					continue
				}

				e := &pb.QueryResponse_Item{
					Identifier: identifier,
					Text:       v.NamePretty,
					Subtext:    v.Description,
					Provider:   Name,
					Actions:    []string{"activate"},
					Type:       pb.QueryResponse_REGULAR,
					Icon:       v.Icon,
				}

				if query != "" {
					e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
						Field: "text",
					}

					e.Score, e.Fuzzyinfo.Positions, e.Fuzzyinfo.Start = common.FuzzyScore(query, e.Text, exact)

					for _, v := range v.Keywords {
						score, positions, start := common.FuzzyScore(query, v, exact)

						if score > e.Score {
							e.Score = score
							e.Fuzzyinfo.Positions = positions
							e.Fuzzyinfo.Start = start
						}
					}
				}

				if e.Score > config.MinScore || query == "" {
					entries = append(entries, e)
				}
			}
		} else {
			if slices.Contains(config.Hidden, *v.Name) {
				continue
			}

			e := &pb.QueryResponse_Item{
				Identifier: *v.Name,
				Text:       *v.NamePretty,
				Icon:       v.Icon(),
				Provider:   Name,
				Actions:    []string{"activate"},
				Type:       pb.QueryResponse_REGULAR,
			}

			if query != "" {
				e.Fuzzyinfo = &pb.QueryResponse_Item_FuzzyInfo{
					Field: "text",
				}

				e.Score, e.Fuzzyinfo.Positions, e.Fuzzyinfo.Start = common.FuzzyScore(query, e.Text, exact)
			}

			if e.Score > config.MinScore || query == "" {
				entries = append(entries, e)
			}
		}
	}

	slices.SortFunc(entries, func(a, b *pb.QueryResponse_Item) int {
		if a.Score > b.Score {
			return 1
		}

		if a.Score < b.Score {
			return -1
		}

		return strings.Compare(a.Text, b.Text)
	})

	slog.Info(Name, "queryresult", len(entries), "time", time.Since(start))

	return entries
}

func Icon() string {
	return ""
}

func State(provider string) *pb.ProviderStateResponse {
	return &pb.ProviderStateResponse{}
}
