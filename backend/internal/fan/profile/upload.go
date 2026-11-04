package profile

import (
	"fmt"
	"strings"
)

// ParseUpload recognises what a user gave (a file or its text) and parses it. name is only a hint.
func ParseUpload(_ string, b []byte, env Env, rules Rules) (*Result, error) {
	if err := CheckText(b); err != nil {
		return nil, err
	}
	trim := strings.TrimSpace(string(b))
	if strings.HasPrefix(trim, "{") {
		switch {
		case strings.Contains(trim, `"`+Kind+`"`):
			r, err := Decode(b)
			if err != nil {
				return nil, err
			}
			r.Finish(rules)
			return r, nil
		case strings.Contains(trim, `"fanctrlplus2-config"`):
			return ParseFanCtrlPlus2Backup(b, env, rules)
		}
		return nil, fmt.Errorf("this JSON file is neither a profile of this plugin nor a FanCtrl Plus 2 backup")
	}
	kv := parseKV(b)
	switch {
	case looksITKuny(kv):
		return ParseITKuny(b, rules)
	case kv["custom"] != "" || kv["disk_group_count"] != "" || kv["interval_sec"] != "" || kv["controller_identity"] != "":
		version := 1
		for _, k := range []string{"disk_group_count", "interval_sec", "controller_identity", "aux_enable"} {
			if _, ok := kv[k]; ok {
				version = 2
			}
		}
		prefix := "fanctrlplus_"
		if version == 2 {
			prefix = "fanctrlplus2_"
		}
		return ParseFanCtrlPlus(map[string][]byte{prefix + uploadName(kv) + ".cfg": b}, version, env, rules)
	case kv["pwm"] != "" || kv["controller"] != "":
		return ParseDynamix(map[string][]byte{"dynamix.system.autofan.cfg": b}, env, rules)
	}
	return nil, fmt.Errorf("the file is not a profile, a FanCtrl Plus / Plus 2 / Dynamix Autofan config or an IT-Kuny env file")
}

func uploadName(kv map[string]string) string {
	if n := kv["custom"]; fcNameRe.MatchString(n) {
		return n
	}
	return "upload"
}
