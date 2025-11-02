package handlers

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net"
	"strings"

	"github.com/abenz1267/elephant/v2/internal/providers"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"google.golang.org/protobuf/proto"
)

type StateRequest struct{}

func (a *StateRequest) Handle(format uint8, cid uint32, conn net.Conn, data []byte) {
	req := &pb.ProviderStateRequest{}

	switch format {
	case 0:
		if err := proto.Unmarshal(data, req); err != nil {
			slog.Error("staterequesthandler", "protobuf", err)

			return
		}
	case 1:
		if err := json.Unmarshal(data, req); err != nil {
			slog.Error("staterequesthandler", "protobuf", err)

			return
		}
	}

	p := req.Provider

	if strings.HasPrefix(req.Provider, "menus:") {
		p = "menus"
	}

	res := providers.Providers[p].State(req.Provider)
	res.Provider = req.Provider

	var b []byte
	var err error

	switch format {
	case 0:
		b, err = proto.Marshal(res)
	case 1:
		b, err = json.Marshal(res)
	}

	if err != nil {
		slog.Error("staterequesthandler", "marshal", err)
		return
	}

	var buffer bytes.Buffer
	buffer.Write([]byte{3})

	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(b)))
	buffer.Write(lengthBuf)
	buffer.Write(b)

	_, err = conn.Write(buffer.Bytes())
	if err != nil {
		slog.Error("staterequesthandler", "write", err, "provider", req.Provider)
		return
	}

	writeStatus(QueryDone, conn)
}
