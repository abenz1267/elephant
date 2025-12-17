// Package providers provides common provider functions.
package providers

import (
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"plugin"
	"slices"
	"strings"
	"sync"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"github.com/charlievieth/fastwalk"
)

type ProviderStateResponse struct {
	Actions []string
	States  []string
}

type Provider struct {
	Name                 *string
	Available            func() bool
	PrintDoc             func()
	NamePretty           *string
	State                func(string) *pb.ProviderStateResponse
	Setup                func()
	HideFromProviderlist func() bool
	Icon                 func() string
	Activate             func(single bool, identifier, action, query, args string, format uint8, conn net.Conn)
	Query                func(conn net.Conn, query string, single bool, exact bool, format uint8) []*pb.QueryResponse_Item
}

var (
	Providers      map[string]Provider
	QueryProviders map[uint32][]string
	libDirs        = []string{
		"/usr/lib/elephant",
		"/usr/lib64/elephant",
		"/usr/local/lib/elephant",
		"/usr/local/lib64/elephant",
		"/lib/elephant",
		"/lib64/elephant",
	}
)

func Load(setup bool) {
	common.LoadMenus()
	ignored := common.GetElephantConfig().IgnoredProviders

	var mut sync.Mutex
	have := []string{}
	dirs := libDirs
	env := os.Getenv("ELEPHANT_PROVIDER_DIR")

	if env != "" {
		dirs = []string{env}
	} else {
		dirs = append(dirs, common.ConfigDirs()...)
	}

	Providers = make(map[string]Provider)
	QueryProviders = make(map[uint32][]string)

	if os.Getenv("ELEPHANT_DEV") == "true" {
		dirs = []string{"/tmp/elephant/providers"}
	}

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

			fn := strings.TrimSuffix(filepath.Base(path), ".so")

			if slices.Contains(ignored, fn) {
				mut.Lock()
				have = append(have, filepath.Base(path))
				mut.Unlock()

				return nil
			}

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

				hideFromProviderlistFunc, err := p.Lookup("HideFromProviderlist")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				availableFunc, err := p.Lookup("Available")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				queryFunc, err := p.Lookup("Query")
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

				stateFunc, err := p.Lookup("State")
				if err != nil {
					slog.Error("providers", "load", err, "provider", path)
				}

				provider := Provider{
					Icon:                 iconFunc.(func() string),
					Setup:                setupFunc.(func()),
					Name:                 name.(*string),
					Activate:             activateFunc.(func(bool, string, string, string, string, uint8, net.Conn)),
					Query:                queryFunc.(func(net.Conn, string, bool, bool, uint8) []*pb.QueryResponse_Item),
					NamePretty:           namePretty.(*string),
					HideFromProviderlist: hideFromProviderlistFunc.(func() bool),
					PrintDoc:             printDocFunc.(func()),
					Available:            availableFunc.(func() bool),
					State:                stateFunc.(func(string) *pb.ProviderStateResponse),
				}

				available := provider.Available()

				if setup && available {
					go provider.Setup()
				}

				if available {
					mut.Lock()
					Providers[*provider.Name] = provider
					mut.Unlock()
				}

				slog.Info("providers", "loaded", *provider.Name)

				if available {
					mut.Lock()
					have = append(have, filepath.Base(path))
					mut.Unlock()
				}
			}

			return err
		}

		if err := fastwalk.Walk(&conf, v, walkFn); err != nil {
			slog.Error("providers", "load", err)
			os.Exit(1)
		}
	}
}
