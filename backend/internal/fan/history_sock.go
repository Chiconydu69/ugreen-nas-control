package fan

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// The daemon answers history requests on a unix socket (root only, in /var/run, i.e. RAM). Nothing runs for the
// history while nobody asks: the accept loop sleeps in the kernel. One request per connection: a line with the range
// ("1h" or "24h"); the answer is one line of JSON (HistoryAnswer).

var historyPath = envOr("UGREEN_FAN_HISTORY", "/var/run/ugreen-nas-control.history.sock")

const historyIO = 2 * time.Second

// serveHistory listens on path (empty: no history service) until the returned stop is called.
func serveHistory(path string, h *History, logf func(string, ...any)) (stop func()) {
	if path == "" {
		return func() {}
	}
	_ = os.Remove(path) // a socket of a daemon that was killed
	ln, err := net.Listen("unix", path)
	if err != nil {
		logf("fan history unavailable: %v", err)
		return func() {}
	}
	_ = os.Chmod(path, 0o600)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			serveConn(conn, h, logf)
		}
	}()
	return func() {
		_ = ln.Close()
		_ = os.Remove(path)
	}
}

// serveConn answers one connection; a panic there is logged and ends that connection only: an unrecovered one would take
// the whole daemon down without handing the fans back.
func serveConn(conn net.Conn, h *History, logf func(string, ...any)) {
	defer func() {
		if r := recover(); r != nil {
			logf("fan history connection crashed: %v", r)
			_ = conn.Close()
		}
	}()
	answerHistory(conn, h)
}

func answerHistory(conn net.Conn, h *History) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(historyIO))
	line, _ := bufio.NewReaderSize(io.LimitReader(conn, 64), 64).ReadString('\n')
	b, err := json.Marshal(h.Answer(strings.TrimSpace(line), now()))
	if err == nil {
		_, _ = conn.Write(append(b, '\n'))
	}
}

// QueryHistory asks the running daemon; a daemon that is not running (no socket, nobody answers) is no error.
func QueryHistory(path, rng string) (HistoryAnswer, error) {
	conn, err := net.DialTimeout("unix", path, historyIO)
	if err != nil {
		return HistoryAnswer{OK: true}, nil
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(historyIO))
	if _, err := fmt.Fprintln(conn, rng); err != nil {
		return HistoryAnswer{}, err
	}
	var a HistoryAnswer
	if err := json.NewDecoder(conn).Decode(&a); err != nil && !errors.Is(err, io.EOF) {
		return HistoryAnswer{}, err
	}
	return a, nil
}

// historyCommand is `ugreen-nas-fan history [1h|24h]`.
func historyCommand(args []string) {
	rng := Range1h
	if len(args) > 0 {
		rng = args[0]
	}
	if rng != Range1h && rng != Range24h {
		fail(fmt.Errorf("range must be %s or %s", Range1h, Range24h))
	}
	a, err := QueryHistory(historyPath, rng)
	if err != nil {
		fail(err)
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(a)
}
