package client

import (
	"bytes"
	"encoding/binary"
	"net"

	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
	"google.golang.org/protobuf/proto"
)

func RequestMenu(menu string) {
	req := pb.MenuRequest{
		Menu: menu,
	}

	b, err := proto.Marshal(&req)
	if err != nil {
		panic(err)
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	var buffer bytes.Buffer
	buffer.Write([]byte{3})

	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(b)))
	buffer.Write(lengthBuf)
	buffer.Write(b)

	_, err = conn.Write(buffer.Bytes())
	if err != nil {
		panic(err)
	}
}
