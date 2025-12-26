// Package client provides simple functions to communicate with the socket.
package client

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/abenz1267/elephant/v2/pkg/pb/pb"
)

var socket string

func init() {
	rd := os.Getenv("XDG_RUNTIME_DIR")

	if rd == "" {
		slog.Error("socket", "runtimedir", "XDG_RUNTIME_DIR not set. falling back to /tmp")
		socket = filepath.Join(os.TempDir(), "elephant", "elephant.sock")
	} else {
		socket = filepath.Join(rd, "elephant", "elephant.sock")
	}
}

func Query(data string, async, j bool) {
	v := strings.Split(data, ";")
	if len(v) < 3 || len(v) > 4 {
		fmt.Fprintln(os.Stderr, "query expects '<providers>;<query>;<limit>[;exactsearch]'")
		return
	}

	maxresults, err := strconv.Atoi(v[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "query: invalid limit %q: %v\n", v[2], err)
		return
	}

	exact := false
	if len(v) > 3 {
		exact, err = strconv.ParseBool(v[3])
		if err != nil {
			fmt.Fprintf(os.Stderr, "query: invalid exactsearch flag %q: %v\n", v[3], err)
			return
		}
	}

	req := pb.QueryRequest{
		Providers:   strings.Split(v[0], ","),
		Query:       v[1],
		Maxresults:  int32(maxresults),
		Exactsearch: exact,
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
	buffer.Write([]byte{0})
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

		if !async && header[0] == done {
			break
		}

		if header[0] != 0 && header[0] != 1 && header[0] != done && header[0] != empty {
			panic("invalid protocol prefix")
		}

		length := binary.BigEndian.Uint32(header[1:5])

		msg := make([]byte, 5+length)
		_, err = io.ReadFull(reader, msg)
		if err != nil {
			panic(err)
		}

		payload := msg[5:]

		resp := &pb.QueryResponse{}
		if err := json.Unmarshal(payload, resp); err != nil {
			panic(err)
		}

		if !j {
			fmt.Println(resp)
		} else {
			out, err := json.Marshal(resp)
			if err != nil {
				panic(err)
			}

			fmt.Println(string(out))
		}
	}
}
