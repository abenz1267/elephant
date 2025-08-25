// Package handlers providers all the communication handlers
package handlers

import (
	"bytes"
	"encoding/binary"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/abenz1267/elephant/internal/providers"
	"github.com/abenz1267/elephant/pkg/pb/pb"
	"google.golang.org/protobuf/proto"
)

type SubscribeRequest struct{}

func (a *SubscribeRequest) Handle(cid uint32, conn net.Conn, data []byte) {
	req := &pb.SubscribeRequest{}
	if err := proto.Unmarshal(data, req); err != nil {
		slog.Error("activationrequesthandler", "protobuf", err)

		return
	}

	subscribe(int(req.Interval), req.Provider, req.Query, conn)
}

var (
	sid             atomic.Uint32
	subs            map[uint32]*sub
	ProviderUpdated chan string
)

const SubscriptionDataChanged = 0

type sub struct {
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

	// handle general realtime subs
	go func() {
		for p := range ProviderUpdated {
			value := p

			if strings.HasPrefix(p, "menus:") {
				p = "menus"
			}

			for k, v := range subs {
				if v.provider == p && v.interval == 0 && v.query == "" {
					if ok := updated(v.conn, value); !ok {
						delete(subs, k)
					}
				}
			}
		}
	}()
}

func subscribe(interval int, provider, query string, conn net.Conn) {
	sid.Add(1)

	sub := &sub{
		sid:      sid.Load(),
		interval: interval,
		provider: provider,
		query:    query,
		conn:     conn,
		results:  []*pb.QueryResponse_Item{},
	}

	subs[sub.sid] = sub

	if interval != 0 {
		go watch(sub, conn)
	}

	slog.Info("subscription", "new", sub.provider)
}

func watch(s *sub, conn net.Conn) {
	p := providers.Providers[s.provider]

	for {
		time.Sleep(time.Duration(s.interval) * time.Millisecond)

		if _, ok := subs[s.sid]; !ok {
			return
		}

		res := p.Query(s.sid, s.sid, s.query, true, false)

		slices.SortFunc(res, sortEntries)

		if len(s.results) != 0 {
			// check if result is different in length
			if len(res) != len(s.results) {
				s.results = res

				if ok := updated(conn, ""); !ok {
					delete(subs, s.sid)
				}

				continue
			}

			// check if result is different in content
			for k, v := range res {
				if !equals(v, s.results[k]) {
					s.results = res

					if ok := updated(conn, ""); !ok {
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

func updated(conn net.Conn, value string) bool {
	resp := pb.SubscribeResponse{
		Value: value,
	}

	b, err := proto.Marshal(&resp)
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
		slog.Error("queryrequesthandler", "write", err)
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
