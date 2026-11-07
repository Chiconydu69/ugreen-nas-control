package mqtt

import (
	"bufio"
	"net"
	"testing"
)

func TestSubscribePacketIDNeverZero(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := &client{conn: a, nextID: 65535}
	go func() { _ = c.subscribe("t") }()
	_, _, body, err := readPacket(bufio.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if id := int(body[0])<<8 | int(body[1]); id == 0 {
		t.Fatal("SUBSCRIBE packet identifier 0 [MQTT-2.3.1-1]")
	}
}
