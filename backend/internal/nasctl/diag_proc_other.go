//go:build !unix

package nasctl

import "os/exec"

func ownGroup(*exec.Cmd) {}
