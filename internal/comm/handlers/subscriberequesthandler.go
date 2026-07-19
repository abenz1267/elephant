// Package handlers providers all the communication handlers
package handlers

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abenz1267/elephant/v2/internal/providers"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"google.golang.org/protobuf/proto"
)

type SubscribeRequest struct{}

func (a *SubscribeRequest) Handle(format uint8, cid uint32, conn net.Conn, data []byte) {
	req := &pb.SubscribeRequest{}

	switch format {
	case 0:
		if err := proto.Unmarshal(data, req); err != nil {
			slog.Error("activationrequesthandler", "protobuf", err)

			return
		}
	case 1:
		if err := json.Unmarshal(data, req); err != nil {
			slog.Error("activationrequesthandler", "protobuf", err)

			return
		}
	}

	subscribe(format, int(req.Interval), req.Provider, req.Query, conn)
}

var (
	sid             atomic.Uint32
	subs            map[uint32]*sub
	ProviderUpdated chan string
	mut             sync.Mutex
)

const (
	SubscriptionDataChanged = 0
	SubscriptionHealthCheck = 230
)

type sub struct {
	format   uint8
	sid      uint32
	interval int
	provider string
	query    string
	results  []*pb.QueryResponse_Item
	conn     net.Conn
}

func init() {
	sid.Store(100_000_000)
	subs = make(map[uint32]*sub)
	ProviderUpdated = make(chan string)

	// go checkHealth()

	// handle general realtime subs
	go func() {
		for p := range ProviderUpdated {
			value := p

			if strings.HasPrefix(p, "menus:") {
				p = "menus"
			}

			if strings.HasPrefix(p, "bluetooth:") {
				p = "bluetooth"
			}

			if strings.HasPrefix(p, "wifi:") {
				p = "wifi"
			}

			toDelete := []uint32{}

			for k, v := range subs {
				if v.provider == p && v.interval == 0 && v.query == "" {
					if ok := updated(v.format, v.conn, value); !ok {
						toDelete = append(toDelete, k)
					}
				}
			}

			for _, v := range toDelete {
				delete(subs, v)
			}
		}
	}()
}

func subscribe(format uint8, interval int, provider, query string, conn net.Conn) {
	sid.Add(1)

	sub := &sub{
		format:   format,
		sid:      sid.Load(),
		interval: interval,
		provider: provider,
		query:    query,
		conn:     conn,
		results:  []*pb.QueryResponse_Item{},
	}

	mut.Lock()
	subs[sub.sid] = sub
	mut.Unlock()

	if interval != 0 {
		go watch(format, sub, conn)
	}

	slog.Info("subscription", "new", sub.provider)
}

func watch(format uint8, s *sub, conn net.Conn) {
	p := providers.Providers[s.provider]

	for {
		time.Sleep(time.Duration(s.interval) * time.Millisecond)

		if _, ok := subs[s.sid]; !ok {
			return
		}

		res := p.Query(conn, s.query, true, false, format)

		slices.SortFunc(res, sortEntries)

		if len(s.results) != 0 {
			// check if result is different in length
			if len(res) != len(s.results) {
				s.results = res

				if ok := updated(format, conn, ""); !ok {
					delete(subs, s.sid)
				}

				continue
			}

			// check if result is different in content
			for k, v := range res {
				if !equals(v, s.results[k]) {
					s.results = res

					if ok := updated(format, conn, ""); !ok {
						delete(subs, s.sid)
					}

					break
				}
			}
		} else {
			s.results = res
		}
	}
}

func updated(format uint8, conn net.Conn, value string) bool {
	resp := pb.SubscribeResponse{
		Value: value,
	}

	var b []byte
	var err error

	switch format {
	case 0:
		b, err = proto.Marshal(&resp)
	case 1:
		b, err = json.Marshal(&resp)
	}

	if err != nil {
		panic(err)
	}

	var buffer bytes.Buffer
	buffer.Write([]byte{SubscriptionDataChanged})

	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(b)))
	buffer.Write(lengthBuf)
	buffer.Write(b)

	_, err = conn.Write(buffer.Bytes())
	if err != nil {
		slog.Debug("subscriptionrequesthandler", "write", err, "value", value)
		return false
	}

	return true
}

func equals(a *pb.QueryResponse_Item, b *pb.QueryResponse_Item) bool {
	if a.Icon != b.Icon || a.Text != b.Text || a.Subtext != b.Subtext || a.Score != b.Score {
		return false
	}

	return true
}
