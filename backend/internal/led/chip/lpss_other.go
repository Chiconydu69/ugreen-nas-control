//go:build !linux

package chip

import "io"

func (c *cli) runLPSSProbe(bool) int { return c.fail(errNotLinux) }

func setLPSSDebug(bool, io.Writer) {}
