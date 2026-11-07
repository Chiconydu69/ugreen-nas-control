package mqtt

import (
	"bufio"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSubackFailureIsLogged(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		_, _, _, _ = readPacket(r)
		conn.Write([]byte{mqttConnack << 4, 2, 0, 0})
		for {
			typ, _, body, err := readPacket(r)
			if err != nil {
				return
			}
			if typ == mqttSubscribe {
				conn.Write([]byte{mqttSuback << 4, 3, body[0], body[1], 0x80}) // refused, e.g. by an ACL
			}
		}
	}()
	rd, wr, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = wr
	c := &client{addr: ln.Addr().String(), clientID: "id"}
	c.onConnect = func() { _ = c.subscribe("a/cmd") }
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { c.run(stop); close(done) }()
	time.Sleep(500 * time.Millisecond)
	close(stop)
	<-done
	os.Stderr = old
	wr.Close()
	out, _ := io.ReadAll(rd)
	if !strings.Contains(string(out), "subscription refused") {
		t.Fatalf("SUBACK 0x80 (section 3.9.3) went unnoticed; log: %q", out)
	}
}
