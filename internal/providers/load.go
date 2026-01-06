// Package providers provides common provider functions.
package providers

import (
	"log/slog"
	"net"
	"slices"
	"sync"

	"github.com/abenz1267/elephant/v2/pkg/common"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
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
	registry       []Provider
)

func Register(p Provider) {
	registry = append(registry, p)
}

func Load(setup bool) {
	common.LoadMenus()
	ignored := common.GetElephantConfig().IgnoredProviders

	Providers = make(map[string]Provider)
	QueryProviders = make(map[uint32][]string)

	var mut sync.Mutex

	for _, provider := range registry {
		if slices.Contains(ignored, *provider.Name) {
			continue
		}

		available := provider.Available()

		if setup && available {
			go provider.Setup()
		}

		if available {
			mut.Lock()
			Providers[*provider.Name] = provider
			mut.Unlock()
			slog.Info("providers", "loaded", *provider.Name)
		}
	}
}
