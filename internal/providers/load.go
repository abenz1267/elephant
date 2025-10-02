// Package providers provides common provider functions.
package providers

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"plugin"
	"slices"
	"sync"

	"github.com/abenz1267/elephant/pkg/common"
	"github.com/abenz1267/elephant/pkg/pb/pb"
	"github.com/charlievieth/fastwalk"
)

type Provider struct {
	Name       *string
	PrintDoc   func()
	NamePretty *string
	Setup      func()
	Icon       func() string
	Cleanup    func(qid uint32)
	Activate   func(qid uint32, identifier, action string, arguments string)
	Query      func(qid uint32, iid uint32, query string, single bool, exact bool) []*pb.QueryResponse_Item
}

var (
	Providers      map[string]Provider
	QueryProviders map[uint32][]string
	AsyncChannels  = make(map[uint32]map[uint32]chan *pb.QueryResponse_Item)
)

func Load(setup bool) {
	common.LoadMenus()

	var mut sync.Mutex
	have := []string{}

	dirs := []string{filepath.Join(common.ConfigDir(), "providers"), "/etc/xdg/elephant/providers", os.Getenv("ELEPHANT_PROVIDER_DIR")}

	Providers = make(map[string]Provider)
	QueryProviders = make(map[uint32][]string)

	for _, v := range dirs {
		if !common.FileExists(v) {
			continue
		}

		conf := fastwalk.Config{
			Follow: true,
		}

		walkFn := func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				slog.Error("providers", "load", err)
				os.Exit(1)
			}

			mut.Lock()
			done := slices.Contains(have, filepath.Base(path))
			mut.Unlock()

			if !done && filepath.Ext(path) == ".so" {
				p, err := plugin.Open(path)
				if err != nil {
					slog.Error("providers", "load", path, "err", err)
					return nil
				}

				name, err := p.Lookup("Name")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				namePretty, err := p.Lookup("NamePretty")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				activateFunc, err := p.Lookup("Activate")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				queryFunc, err := p.Lookup("Query")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				cleanupFunc, err := p.Lookup("Cleanup")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				iconFunc, err := p.Lookup("Icon")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				printDocFunc, err := p.Lookup("PrintDoc")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				setupFunc, err := p.Lookup("Setup")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				provider := Provider{
					Icon:       iconFunc.(func() string),
					Setup:      setupFunc.(func()),
					Name:       name.(*string),
					Cleanup:    cleanupFunc.(func(uint32)),
					Activate:   activateFunc.(func(uint32, string, string, string)),
					Query:      queryFunc.(func(uint32, uint32, string, bool, bool) []*pb.QueryResponse_Item),
					NamePretty: namePretty.(*string),
					PrintDoc:   printDocFunc.(func()),
				}

				go func() {
					if setup {
						provider.Setup()
					}

					mut.Lock()
					Providers[*provider.Name] = provider
					mut.Unlock()

					slog.Info("providers", "loaded", *provider.Name)
				}()

				mut.Lock()
				have = append(have, filepath.Base(path))
				mut.Unlock()
			}

			return err
		}

		if err := fastwalk.Walk(&conf, v, walkFn); err != nil {
			slog.Error("providers", "load", err)
			os.Exit(1)
		}
	}
}
