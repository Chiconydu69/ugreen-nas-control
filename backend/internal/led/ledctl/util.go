package ledctl

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
)

// IFS whitespace of `read`: space, tab, newline.
const ifs = " \t\n"

func trimIFS(s string) string { return strings.Trim(s, ifs) }

// readLine mimics `read -r var <file`: the first line without surrounding
// whitespace. ok is false when the file is unreadable or has no newline
// (bash returns 1 then, but still sets the variable).
func readLine(path string) (line string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	s := string(data)
	i := strings.IndexByte(s, '\n')
	if i < 0 {
		return trimIFS(s), false
	}
	return trimIFS(s[:i]), true
}

// readLines returns the newline-terminated lines of a file, which is what
// `while read -r line` loops see (an unterminated last line is dropped).
func readLines(path string) ([]string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	s := string(data)
	var out []string
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return out, true
		}
		out = append(out, s[:i])
		s = s[i+1:]
	}
}

// splitRead mimics `read -r a b c`: n fields, the last one takes the rest.
func splitRead(line string, n int) []string {
	out := make([]string, n)
	s := strings.TrimLeft(line, " \t")
	for i := 0; i < n; i++ {
		if i == n-1 {
			out[i] = strings.TrimRight(s, " \t")
			break
		}
		j := strings.IndexAny(s, " \t")
		if j < 0 {
			out[i] = s
			break
		}
		out[i] = s[:j]
		s = strings.TrimLeft(s[j:], " \t")
	}
	return out
}

func isReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// atoi is the decimal number of a `[ x -gt y ]` operand.
func atoi(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n, err == nil
}

func atoiOr(s string, def int64) int64 {
	if n, ok := atoi(s); ok {
		return n
	}
	return def
}

func isByte(s string) bool {
	if !isDigits(s) {
		return false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n <= 255
}

// globMatch is a shell `case` pattern match (*, ?, [set]).
func globMatch(pattern, s string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch ch := pattern[i]; ch {
		case '*':
			b.WriteString("(?s:.*)")
		case '?':
			b.WriteString("(?s:.)")
		case '[':
			j := strings.IndexByte(pattern[i:], ']')
			if j < 0 {
				b.WriteString(`\[`)
				continue
			}
			b.WriteString(pattern[i : i+j+1])
			i += j
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(s)
}

func globAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if globMatch(p, s) {
			return true
		}
	}
	return false
}

// listDir returns the sorted entry names of a directory (a shell glob of dir/*).
func listDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// listPrefix returns the sorted paths of a glob "<prefix>*".
func listPrefix(prefix string) []string {
	dir, base := filepath.Dir(prefix), filepath.Base(prefix)
	if strings.HasSuffix(prefix, "/") {
		dir, base = prefix, ""
	}
	var out []string
	for _, n := range listDir(dir) {
		if strings.HasPrefix(n, base) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

// readlinkF is `readlink -f`: "" when the path does not resolve.
func readlinkF(path string) string {
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// sanitizeKey is ${s//[^A-Za-z0-9]/_}.
func sanitizeKey(s string) string {
	out := []rune(s)
	for i, ch := range out {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			out[i] = '_'
		}
	}
	return string(out)
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func appendFile(path, text string) {
	f, err := fsx.OpenAppend(path)
	if err != nil {
		return
	}
	_, _ = f.WriteString(text)
	_ = f.Close()
}
