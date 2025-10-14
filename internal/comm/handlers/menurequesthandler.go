package handlers

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"google.golang.org/protobuf/proto"
)

type MenuRequest struct{}

func (a *MenuRequest) Handle(cid uint32, conn net.Conn, data []byte) {
	req := &pb.MenuRequest{}
	if err := proto.Unmarshal(data, req); err != nil {
		slog.Error("menurequesthandler", "protobuf", err)

		return
	}

	ProviderUpdated <- fmt.Sprintf("%s:%s", "menus", req.Menu)
}
