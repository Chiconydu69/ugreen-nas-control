package profile

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var backupNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)

// ParseFanCtrlPlus2Backup reads the JSON backup of FanCtrl Plus 2 ({"format":"fanctrlplus2-config","version":1,
// "files":{name: text}}): strict about format and version, unknown top-level fields are ignored.
func ParseFanCtrlPlus2Backup(b []byte, env Env, rules Rules) (*Result, error) {
	files, err := fc2BackupFiles(b)
	if err != nil {
		return nil, err
	}
	return ParseFanCtrlPlus(files, 2, env, rules)
}

func fc2BackupFiles(b []byte) (map[string][]byte, error) {
	if err := CheckText(b); err != nil {
		return nil, err
	}
	var doc struct {
		Format  string            `json:"format"`
		Version int               `json:"version"`
		Files   map[string]string `json:"files"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("not a FanCtrl Plus 2 backup: %v", err)
	}
	if doc.Format != "fanctrlplus2-config" || doc.Version != 1 {
		return nil, fmt.Errorf("not a FanCtrl Plus 2 backup (format %q, version %d)", clipPair(doc.Format), doc.Version)
	}
	if len(doc.Files) == 0 || len(doc.Files) > 256 {
		return nil, fmt.Errorf("the backup has %d files", len(doc.Files))
	}
	files := map[string][]byte{}
	for name, text := range doc.Files {
		if !backupNameRe.MatchString(name) {
			return nil, fmt.Errorf("the backup has a file with a bad name")
		}
		files[name] = []byte(text)
	}
	return files, nil
}
