package profile

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// parseKV reads KEY=value lines as the rival tools write them: comments and blank lines are skipped, a value is bare,
// 'single quoted' or "double quoted" (inside double quotes \\ \" \$ \` are escapes), "export " in front is allowed.
// Nothing is evaluated: no expansion, no command substitution. Later lines win. A line that is not a setting is ignored.
func parseKV(b []byte) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !keyRe.MatchString(key) {
			continue
		}
		kv[key] = unquote(strings.TrimSpace(val))
	}
	return kv
}

func unquote(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		inner := v[1 : len(v)-1]
		var sb strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\\' && i+1 < len(inner) && strings.IndexByte("\\\"$`", inner[i+1]) >= 0 {
				i++
			}
			sb.WriteByte(inner[i])
		}
		return sb.String()
	}
	return v
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// num reads an integer setting; false when absent or not a number.
func num(kv map[string]string, key string) (int, bool) {
	s, ok := kv[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
