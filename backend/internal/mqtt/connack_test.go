package mqtt

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

func TestConnackRefusalHasReason(t *testing.T) {
	for code, want := range map[byte]string{
		1: "protocol version", 2: "client identifier", 3: "unavailable", 4: "user name or password", 5: "not authorized",
	} {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			_, _, _, _ = readPacket(bufio.NewReader(conn))
			conn.Write([]byte{mqttConnack << 4, 2, 0, code})
			time.Sleep(200 * time.Millisecond)
		}()
		c := &client{addr: ln.Addr().String(), clientID: "id"}
		err = c.session(make(chan struct{}))
		ln.Close()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("code %d: error %v lacks %q", code, err, want)
		}
	}
}
