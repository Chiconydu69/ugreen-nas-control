package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Source is a fan tool whose settings were found on this NAS.
type Source struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Files []string `json:"files"`
	// Path is where an IT-Kuny env file was found; the user names the path when it is empty.
	Path string `json:"path,omitempty"`
}

var sourceDirs = []struct {
	id, name, dir, glob string
}{
	{SourceFanCtrlPlus, "FanCtrl Plus", "fanctrlplus", "fanctrlplus_*.cfg"},
	{SourceFanCtrlPlus2, "FanCtrl Plus 2", "fanctrlplus2", "fanctrlplus2_*.cfg"},
	{SourceDynamix, "Dynamix Auto Fan Control", "dynamix.system.autofan", "dynamix.system.autofan*.cfg"},
}

// ITKunyCandidates are the places an IT-Kuny env file is looked for without being told (the Unraid location is not
// fixed, so a file elsewhere needs a path from the user).
func ITKunyCandidates(boot string) []string {
	return []string{"/etc/ugreen/ugreen-fan-control.env", filepath.Join(boot, "ugreen-it87", "ugreen-fan-control.env")}
}

// Discover lists the rival configs under the plugins folder (/boot/config/plugins). Read only.
func Discover(boot string) []Source {
	var out []Source
	for _, s := range sourceDirs {
		matches, _ := filepath.Glob(filepath.Join(boot, s.dir, s.glob))
		var names []string
		for _, m := range matches {
			if st, err := os.Lstat(m); err == nil && st.Mode().IsRegular() {
				if n := filepath.Base(m); !strings.Contains(n, "_temp_") {
					names = append(names, n)
				}
			}
		}
		if len(names) > 0 {
			sort.Strings(names)
			out = append(out, Source{ID: s.id, Name: s.name, Files: names})
		}
	}
	for _, p := range ITKunyCandidates(boot) {
		if st, err := os.Lstat(p); err == nil && st.Mode().IsRegular() {
			out = append(out, Source{ID: SourceITKuny, Name: "IT-Kuny fan control", Files: []string{filepath.Base(p)}, Path: p})
			break
		}
	}
	return out
}

// readCapped reads a regular file of at most MaxBytes; links, devices and larger files are refused.
func readCapped(path string) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s", filepath.Base(path))
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	if st.Size() > MaxBytes {
		return nil, fmt.Errorf("%s is larger than %d KB", filepath.Base(path), MaxBytes>>10)
	}
	return os.ReadFile(path)
}

// LoadSource reads the files of a discovered source and parses them. path is only used by the IT-Kuny source.
func LoadSource(id, boot, path string, env Env, rules Rules) (*Result, error) {
	if id == SourceITKuny {
		if path == "" {
			for _, s := range Discover(boot) {
				if s.ID == id {
					path = s.Path
				}
			}
		}
		if err := allowedUserPath(path); err != nil {
			return nil, err
		}
		b, err := readCapped(path)
		if err != nil {
			return nil, err
		}
		return ParseITKuny(b, rules)
	}
	for _, s := range sourceDirs {
		if s.id != id {
			continue
		}
		files := map[string][]byte{}
		for _, name := range Discover(boot) {
			if name.ID != id {
				continue
			}
			for _, f := range name.Files {
				b, err := readCapped(filepath.Join(boot, s.dir, f))
				if err != nil {
					return nil, err
				}
				files[f] = b
			}
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("%s is not configured on this NAS", s.name)
		}
		switch id {
		case SourceDynamix:
			return ParseDynamix(files, env, rules)
		case SourceFanCtrlPlus2:
			return ParseFanCtrlPlus(files, 2, env, rules)
		}
		return ParseFanCtrlPlus(files, 1, env, rules)
	}
	return nil, fmt.Errorf("unknown source %q", id)
}

// userPathRoots: where a user-named file may be (flash drive, shares, root's home, /etc, /tmp). Tests add a root.
var userPathRoots = []string{"/boot/", "/mnt/", "/root/", "/etc/", "/tmp/"}

// allowedUserPath: an absolute path without "..", under one of the roots.
func allowedUserPath(p string) error {
	if p == "" {
		return fmt.Errorf("tell me the path of the env file")
	}
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsRune(p, 0) {
		return fmt.Errorf("the path must be absolute and plain (no .. or //)")
	}
	for _, root := range userPathRoots {
		if strings.HasPrefix(p, root) {
			return nil
		}
	}
	return fmt.Errorf("files are only read from /boot, /mnt, /root, /etc and /tmp")
}
