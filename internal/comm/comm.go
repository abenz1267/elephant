// Package comm provides functionallity to communitate with elephant
package comm

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"github.com/abenz1267/elephant/internal/comm/handlers"
)

// connection id
var (
	cid    uint32
	Socket = filepath.Join(os.TempDir(), "elephant.sock")
)

var registry []MessageHandler

type MessageHandler interface {
	Handle(cid uint32, conn net.Conn, data []byte)
}

const (
	QueryRequestHandlerPos     = 0
	ActivateRequestHandlerPos  = 1
	SubscribeRequestHandlerPos = 2
	MenuRequestHandlerPos      = 3
)

func init() {
	registry = make([]MessageHandler, 255)

	registry[QueryRequestHandlerPos] = &handlers.QueryRequest{}
	registry[ActivateRequestHandlerPos] = &handlers.ActivateRequest{}
	registry[SubscribeRequestHandlerPos] = &handlers.SubscribeRequest{}
	registry[MenuRequestHandlerPos] = &handlers.MenuRequest{}
}

func StartListen() {
	os.Remove(Socket)

	l, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: Socket,
	})
	if err != nil {
		slog.Error("comm", "socket", err)
	}
	defer l.Close()

	slog.Info("comm", "listen", "starting")

	for {
		conn, err := l.AcceptUnix()
		if err != nil {
			slog.Error("comm", "accept", err)
		}

		slog.Info("comm", "connection", "new")

		cid++

		go handle(conn, cid)
	}
}

func handle(conn net.Conn, cid uint32) {
	defer conn.Close()

	for {
		tb := make([]byte, 1)
		if _, err := io.ReadFull(conn, tb); err != nil {
			if err == io.EOF {
				break
			}

			slog.Error("conn", "readtype", err)
			continue
		}

		mType := int(tb[0])

		lb := make([]byte, 4)
		if _, err := io.ReadFull(conn, lb); err != nil {
			slog.Error("conn", "readlength", err)
			continue
		}

		l := binary.BigEndian.Uint32(lb)

		p := make([]byte, l)
		if _, err := io.ReadFull(conn, p); err != nil {
			slog.Error("conn", "readpayload", err)
			continue
		}

		registry[mType].Handle(cid, conn, p)
	}
}
