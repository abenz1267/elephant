package handlers

import (
	"bytes"
	"encoding/binary"
	"log/slog"
	"net"
	"strings"

	"github.com/abenz1267/elephant/v2/internal/providers"
	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"google.golang.org/protobuf/proto"
)

type ActivateRequest struct{}

func (a *ActivateRequest) Handle(cid uint32, conn net.Conn, data []byte) {
	req := &pb.ActivateRequest{}
	if err := proto.Unmarshal(data, req); err != nil {
		slog.Error("activationrequesthandler", "protobuf", err)

		return
	}

	provider := req.Provider

	if strings.HasPrefix(provider, "menus:") {
		provider = strings.Split(provider, ":")[0]
	}

	if p, ok := providers.Providers[provider]; ok {
		p.Activate(req.Identifier, req.Action, req.Query, req.Arguments)

		var buffer bytes.Buffer
		buffer.Write([]byte{ActivationFinished})

		lengthBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(lengthBuf, uint32(0))
		buffer.Write(lengthBuf)

		_, err := conn.Write(buffer.Bytes())
		if err != nil {
			slog.Debug("activation done", "write", err)
		}
	}
}
