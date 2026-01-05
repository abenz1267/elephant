package handlers

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
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

const (
	QueryDone          = 255
	QueryNoResults     = 254
	StatusDone         = 253
	QueryItem          = 0
	QueryAsyncItem     = 1
	ActivationFinished = 2
	ProviderState      = 3
)

var (
	queries                          = make(map[uint32]context.CancelFunc)
	queryMutex                       sync.Mutex
	MaxGlobalItemsToDisplayWebsearch = 0
	WebsearchAlwaysShow              = false
	WebsearchPrefixes                = make(map[string]string)
	qid                              atomic.Uint32
)

type QueryRequest struct{}

func UpdateItem(format uint8, query string, conn net.Conn, item *pb.QueryResponse_Item) {
	req := pb.QueryResponse{
		Query: query,
		Item:  item,
	}

	var b []byte
	var err error

	switch format {
	case 0:
		b, err = proto.Marshal(&req)
	case 1:
		b, err = json.Marshal(&req)
	}

	if err != nil {
		slog.Debug("async update", "marshal", err)
		return
	}

	var buffer bytes.Buffer
	buffer.Write([]byte{QueryAsyncItem})

	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(b)))
	buffer.Write(lengthBuf)
	buffer.Write(b)

	_, err = conn.Write(buffer.Bytes())
	if err != nil {
		slog.Debug("async update", "write", err)
		return
	}
}

func (h *QueryRequest) Handle(format uint8, cid uint32, conn net.Conn, data []byte) {
	qid.Add(1)
	qqid := qid.Load()

	start := time.Now()

	req := &pb.QueryRequest{}

	switch format {
	case 0:
		if err := proto.Unmarshal(data, req); err != nil {
			slog.Error("queryhandler", "protobuf", err)

			return
		}
	case 1:
		if err := json.Unmarshal(data, req); err != nil {
			slog.Error("queryhandler", "protobuf", err)

			return
		}
	}

	wsprefix := ""

	if slices.Contains(req.Providers, "websearch") {
		for k, v := range WebsearchPrefixes {
			if strings.HasPrefix(req.Query, k) {
				wsprefix = v
			}
		}
	}

	queryMutex.Lock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if val, ok := queries[cid]; ok {
		if val != nil {
			val()
		}
		queries[cid] = cancel
	} else {
		queries[cid] = cancel
	}
	queryMutex.Unlock()

	isCncld := func() bool {
		select {
		case <-ctx.Done():
			return true
		default:
			return false
		}
	}

	var mut sync.Mutex

	var wg sync.WaitGroup
	wg.Add(len(req.Providers))

	entries := []*pb.QueryResponse_Item{}

	for _, v := range req.Providers {
		query := req.Query

		if strings.HasPrefix(v, "menus:") {
			split := strings.Split(v, ":")
			v = split[0]
			query = fmt.Sprintf("%s:%s", split[1], query)
		}

		go func(text string, wg *sync.WaitGroup) {
			defer wg.Done()
			if p, ok := providers.Providers[v]; ok {
				res := p.Query(conn, text, len(req.Providers) == 1, req.Exactsearch, format)

				mut.Lock()
				entries = append(entries, res...)
				mut.Unlock()
			}
		}(query, &wg)
	}

	wg.Wait()

	if isCncld() {
		return
	}

	slices.SortFunc(entries, sortEntries)

	if len(entries) == 0 {
		writeStatus(QueryNoResults, conn)
		writeStatus(QueryDone, conn)
		slog.Info("providers", "p", strings.Join(req.Providers, ","), "results", len(entries), "time", time.Since(start))
		return
	}

	if len(entries) > int(req.Maxresults) {
		entries = entries[:req.Maxresults]
	}

	hideWebsearch := (len(req.Providers) > 1 && len(entries) > MaxGlobalItemsToDisplayWebsearch) && !WebsearchAlwaysShow

	for _, v := range entries {
		if isCncld() {
			return
		}

		if v.Provider == "websearch" && hideWebsearch && v.Text != wsprefix {
			continue
		}

		req := pb.QueryResponse{
			Qid:   int32(qqid),
			Query: req.Query,
			Item:  v,
		}

		var b []byte
		var err error

		switch format {
		case 0:
			b, err = proto.Marshal(&req)
		case 1:
			b, err = json.Marshal(&req)
		}

		if err != nil {
			slog.Error("queryrequesthandler", "marshal", err)
			continue
		}

		var buffer bytes.Buffer
		buffer.Write([]byte{QueryItem})

		lengthBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(lengthBuf, uint32(len(b)))
		buffer.Write(lengthBuf)
		buffer.Write(b)

		_, err = conn.Write(buffer.Bytes())
		if err != nil {
			slog.Error("queryrequesthandler", "write", err, "item", v.Text)
			return
		}
	}

	writeStatus(QueryDone, conn)

	slog.Info("providers", "p", strings.Join(req.Providers, ","), "results", len(entries), "time", time.Since(start))
}

func sortEntries(a *pb.QueryResponse_Item, b *pb.QueryResponse_Item) int {
	if a.Score > b.Score {
		return -1
	}

	if b.Score > a.Score {
		return 1
	}

	return strings.Compare(strings.ToLower(a.Text), strings.ToLower(b.Text))
}
