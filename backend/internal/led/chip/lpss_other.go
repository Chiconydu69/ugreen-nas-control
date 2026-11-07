//go:build !linux

package chip

func (c *cli) runLPSSProbe(bool) int { return c.fail(errNotLinux) }
