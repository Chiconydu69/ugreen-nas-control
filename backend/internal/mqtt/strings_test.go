package mqtt

import (
	"net"
	"strings"
	"testing"
	"time"
)

// A string the wire format cannot carry must be refused before anything is sent: encodeString would wrap the
// 2-byte length and corrupt the whole CONNECT ([MQTT-1.5.3-1], [MQTT-1.5.3-2]).
func TestConnectRejectsUnencodableStrings(t *testing.T) {
	cases := map[string]*client{
		"user over 65535 bytes": {user: strings.Repeat("u", 70000)},
		"pass over 65535 bytes": {user: "u", pass: strings.Repeat("p", 70000)},
		"user not UTF-8":        {user: "j\xfcrgen"},
		"user with NUL":         {user: "a\x00b"},
		"client id not UTF-8":   {clientID: "\xff"},
	}
	for name, c := range cases {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		accepted := make(chan bool, 1)
		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
			accepted <- true
		}()
		c.addr = ln.Addr().String()
		if err := c.session(make(chan struct{})); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Errorf("%s: got error %v, want an 'invalid' error before connecting", name, err)
		}
		select {
		case <-accepted:
			t.Errorf("%s: client connected and sent a corrupt CONNECT", name)
		case <-time.After(100 * time.Millisecond):
		}
		ln.Close()
	}
}
