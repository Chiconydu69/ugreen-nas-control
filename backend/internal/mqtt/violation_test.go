package mqtt

import (
	"bufio"
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

// What real brokers send (PINGRESP, SUBACK, PUBACK, PUBLISH) and the client must tolerate.
var tolerated = bytes.Join([][]byte{
	{mqttPingresp << 4, 0},
	{mqttSuback << 4, 3, 0, 1, 1},
	{mqttPuback << 4, 2, 0, 5},
	packet(mqttPublish, 2, append(encodeString("ok"), 0, 9, 'x')),
}, nil)

// A packet the broker may never send makes the client close the connection ([MQTT-4.8.0-1]) instead of ignoring it;
// the packets in tolerated before it do not.
func TestBrokerProtocolViolationClosesSession(t *testing.T) {
	cases := map[string][]byte{
		"CONNECT from broker":    packet(mqttConnect, 0, encodeString("MQTT")),
		"second CONNACK":         {mqttConnack << 4, 2, 0, 0},
		"SUBSCRIBE from broker":  packet(mqttSubscribe, 2, []byte{0, 1, 0, 1, 'a', 0}),
		"reserved type 0":        {0x00, 0},
		"reserved type 15":       {0xf0, 0},
		"PUBLISH QoS 3":          packet(mqttPublish, 6, append(encodeString("a"), 0, 1)),
		"PUBLISH QoS 1 id 0":     packet(mqttPublish, 2, append(encodeString("a"), 0, 0)),
		"PUBLISH 1 byte":         packet(mqttPublish, 0, []byte{0}),
		"PUBLISH topic overruns": packet(mqttPublish, 0, []byte{0, 9, 'a'}),
		"PUBLISH QoS 1 no id":    packet(mqttPublish, 2, encodeString("a")),
		"PUBREL malformed":       packet(mqttPubrel, 2, []byte{0}),
		"PUBLISH empty topic":    packet(mqttPublish, 0, encodeString("")),
		"PUBLISH wildcard topic": packet(mqttPublish, 0, encodeString("a/#")),
	}
	for name, bad := range cases {
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
			conn.Write([]byte{mqttConnack << 4, 2, 0, 0})
			conn.Write(tolerated)
			conn.Write(bad)
			time.Sleep(3 * time.Second)
		}()
		gotOK := make(chan bool, 1)
		c := &client{addr: ln.Addr().String(), clientID: "id"}
		done := make(chan error, 1)
		quit := make(chan struct{})
		c.onMessage = func(topic string, _ []byte, _ bool) {
			select {
			case gotOK <- topic == "ok":
			default:
			}
		}
		go func() { done <- c.session(quit) }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: session ended without error", name)
			}
			select {
			case ok := <-gotOK:
				if !ok {
					t.Errorf("%s: wrong message delivered", name)
				}
			default:
				t.Errorf("%s: the tolerated packets before it ended the session (err %v)", name, err)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: client ignored it and kept the connection", name)
			close(quit)
			<-done
		}
		ln.Close()
	}
}

// A QoS 2 PUBLISH is answered with PUBREC, passed on once however often it is repeated, and completed with PUBCOMP
// on PUBREL ([MQTT-3.3.4-1], [MQTT-4.3.3-2]); the connection stays open.
func TestQoS2PublishIsAcknowledged(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan [2]byte, 4)
	serverDone := make(chan struct{})
	defer close(serverDone)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		defer func() { <-serverDone }()
		r := bufio.NewReader(conn)
		_, _, _, _ = readPacket(r)
		conn.Write([]byte{mqttConnack << 4, 2, 0, 0})
		pub := packet(mqttPublish, 4, append(encodeString("a/cmd"), 0, 7, 'x'))
		conn.Write(pub)
		conn.Write(pub)
		conn.Write(packet(mqttPubrel, 2, []byte{0, 7}))
		for i := 0; i < 3; i++ {
			typ, _, body, err := readPacket(r)
			if err != nil || len(body) != 2 {
				return
			}
			got <- [2]byte{typ, body[1]}
		}
	}()
	var mu sync.Mutex
	delivered := 0
	c := &client{addr: ln.Addr().String(), clientID: "id"}
	c.onMessage = func(string, []byte, bool) { mu.Lock(); delivered++; mu.Unlock() }
	quit := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- c.session(quit) }()
	for i, want := range [][2]byte{{mqttPubrec, 7}, {mqttPubrec, 7}, {mqttPubcomp, 7}} {
		select {
		case g := <-got:
			if g != want {
				t.Fatalf("reply %d: got %v, want %v", i, g, want)
			}
		case err := <-done:
			t.Fatalf("session ended: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatalf("reply %d missing", i)
		}
	}
	close(quit)
	<-done
	mu.Lock()
	defer mu.Unlock()
	if delivered != 1 {
		t.Fatalf("delivered %d times, want once", delivered)
	}
}
