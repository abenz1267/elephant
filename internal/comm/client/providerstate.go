// Package client provides simple functions to communicate with the socket.
package client

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

func ProviderState(data string) {
	req := pb.ProviderStateRequest{
		Provider: data,
	}

	b, err := json.Marshal(&req)
	if err != nil {
		panic(err)
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	var buffer bytes.Buffer
	buffer.Write([]byte{4})
	buffer.Write([]byte{1})

	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(b)))
	buffer.Write(lengthBuf)
	buffer.Write(b)

	_, err = conn.Write(buffer.Bytes())
	if err != nil {
		panic(err)
	}

	reader := bufio.NewReader(conn)

	for {
		header, err := reader.Peek(5)
		if err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}

		if header[0] == 255 {
			break
		}

		if header[0] != 3 {
			panic("invalid protocol prefix")
		}

		length := binary.BigEndian.Uint32(header[1:5])

		msg := make([]byte, 5+length)
		_, err = io.ReadFull(reader, msg)
		if err != nil {
			panic(err)
		}

		payload := msg[5:]

		resp := &pb.ProviderStateResponse{}
		if err := json.Unmarshal(payload, resp); err != nil {
			panic(err)
		}

		fmt.Println(resp)
	}
}
