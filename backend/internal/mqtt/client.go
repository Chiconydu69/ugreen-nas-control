// Package mqtt is the publisher of `ugreen-nas-fan publish`: it sends the status of the NAS to an MQTT broker and, only if
// switched on, carries out a few fixed commands. See docs/mqtt.md.
package mqtt

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// A small MQTT 3.1.1 client: connect with user and password and a last will, publish with QoS 0 (retained
// or not), subscribe with QoS 0 or 1. It is all the publisher needs, and keeps the tool free of dependencies.

const (
	mqttConnect   = 1
	mqttConnack   = 2
	mqttPublish   = 3
	mqttPuback    = 4
	mqttSubscribe = 8
	mqttSuback    = 9
	mqttPingreq   = 12
	mqttPingresp  = 13
	mqttDisconn   = 14

	mqttKeepAlive = 30 * time.Second
)

type client struct {
	addr     string
	useTLS   bool
	user     string
	pass     string
	clientID string

	willTopic   string
	willPayload []byte

	// Called on every (re)connect, after CONNACK, and for every message received.
	onConnect func()
	onMessage func(topic string, payload []byte, retained bool)

	mu     sync.Mutex
	conn   net.Conn
	nextID uint16
}

func encodeLength(n int) []byte {
	var b []byte
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		b = append(b, d)
		if n == 0 {
			return b
		}
	}
}

func encodeString(s string) []byte {
	return append([]byte{byte(len(s) >> 8), byte(len(s))}, s...)
}

func packet(typ, flags byte, body []byte) []byte {
	p := []byte{typ<<4 | flags}
	p = append(p, encodeLength(len(body))...)
	return append(p, body...)
}

func (c *client) connectPacket() []byte {
	flags := byte(0x02) // clean session
	body := append(encodeString("MQTT"), 4)
	if c.willTopic != "" {
		flags |= 0x04 | 0x20 // will, retained
	}
	if c.user != "" {
		flags |= 0x80
		if c.pass != "" {
			flags |= 0x40
		}
	}
	ka := int(mqttKeepAlive.Seconds())
	body = append(body, flags, byte(ka>>8), byte(ka))
	body = append(body, encodeString(c.clientID)...)
	if c.willTopic != "" {
		body = append(body, encodeString(c.willTopic)...)
		body = append(body, byte(len(c.willPayload)>>8), byte(len(c.willPayload)))
		body = append(body, c.willPayload...)
	}
	if c.user != "" {
		body = append(body, encodeString(c.user)...)
		if c.pass != "" {
			body = append(body, encodeString(c.pass)...)
		}
	}
	return packet(mqttConnect, 0, body)
}

func publishPacket(topic string, payload []byte, retain bool) []byte {
	var flags byte
	if retain {
		flags = 1
	}
	return packet(mqttPublish, flags, append(encodeString(topic), payload...))
}

func subscribePacket(id uint16, topic string, qos byte) []byte {
	body := append([]byte{byte(id >> 8), byte(id)}, encodeString(topic)...)
	return packet(mqttSubscribe, 2, append(body, qos))
}

// readPacket reads one packet: its type, its flags and the body.
func readPacket(r *bufio.Reader) (typ, flags byte, body []byte, err error) {
	h, err := r.ReadByte()
	if err != nil {
		return 0, 0, nil, err
	}
	n, mult := 0, 1
	for i := 0; ; i++ {
		d, err := r.ReadByte()
		if err != nil {
			return 0, 0, nil, err
		}
		n += int(d&0x7f) * mult
		if d&0x80 == 0 {
			break
		}
		if i == 3 {
			return 0, 0, nil, errors.New("mqtt: bad length")
		}
		mult *= 128
	}
	if n > 1<<20 {
		return 0, 0, nil, errors.New("mqtt: packet too large")
	}
	body = make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, 0, nil, err
	}
	return h >> 4, h & 0x0f, body, nil
}

func (c *client) write(p []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return errors.New("mqtt: not connected")
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.conn.Write(p)
	return err
}

func (c *client) publish(topic string, payload []byte, retain bool) error {
	return c.write(publishPacket(topic, payload, retain))
}

func (c *client) subscribe(topic string) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	return c.write(subscribePacket(id, topic, 1))
}

// session connects once and serves the connection until it breaks.
func (c *client) session(quit <-chan struct{}) error {
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if c.useTLS {
		conn, err = tls.DialWithDialer(&d, "tcp", c.addr, &tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.Dial("tcp", c.addr)
	}
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	r := bufio.NewReader(conn)
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(c.connectPacket()); err != nil {
		return err
	}
	typ, _, body, err := readPacket(r)
	if err != nil {
		return err
	}
	if typ != mqttConnack || len(body) != 2 {
		return errors.New("mqtt: no CONNACK")
	}
	if body[1] != 0 {
		return fmt.Errorf("mqtt: connection refused (code %d)", body[1])
	}
	_ = conn.SetDeadline(time.Time{})

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()

	stop := make(chan struct{})
	defer close(stop)
	// The publisher is shutting down: a clean DISCONNECT makes the broker drop the last will, so the retained offline
	// message goes out first (same connection, in order), then goodbye and close, so that the read below ends now and
	// not after the keep-alive period.
	go func() {
		select {
		case <-quit:
			if c.willTopic != "" {
				_ = c.publish(c.willTopic, c.willPayload, true)
			}
			_ = c.write(packet(mqttDisconn, 0, nil))
			_ = conn.Close()
		case <-stop:
		}
	}()
	go func() {
		t := time.NewTicker(mqttKeepAlive / 2)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if c.write(packet(mqttPingreq, 0, nil)) != nil {
					_ = conn.Close()
					return
				}
			case <-stop:
				return
			}
		}
	}()

	if c.onConnect != nil {
		go c.onConnect()
	}

	for {
		// Anything, a ping answer included, must arrive within one and a half keep-alive periods.
		_ = conn.SetReadDeadline(time.Now().Add(mqttKeepAlive * 3 / 2))
		typ, flags, body, err := readPacket(r)
		if err != nil {
			return err
		}
		if typ != mqttPublish || len(body) < 2 {
			continue
		}
		tl := int(body[0])<<8 | int(body[1])
		if len(body) < 2+tl {
			continue
		}
		topic, rest := string(body[2:2+tl]), body[2+tl:]
		if qos := flags >> 1 & 3; qos > 0 {
			if len(rest) < 2 {
				continue
			}
			_ = c.write(packet(mqttPuback, 0, rest[:2]))
			rest = rest[2:]
		}
		if c.onMessage != nil {
			c.onMessage(topic, append([]byte(nil), rest...), flags&1 != 0)
		}
	}
}

// run keeps the connection up: after a failure it waits, longer each time up to a minute, and tries again.
func (c *client) run(stop <-chan struct{}) {
	wait := time.Second
	for {
		start := time.Now()
		err := c.session(stop)
		select {
		case <-stop:
			return
		default:
		}
		if time.Since(start) > time.Minute {
			wait = time.Second
		}
		logf("mqtt: %v; retrying in %s", err, wait)
		select {
		case <-time.After(wait):
		case <-stop:
			return
		}
		if wait < time.Minute {
			wait *= 2
		}
	}
}
