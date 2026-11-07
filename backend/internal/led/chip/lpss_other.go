//go:build !linux

package chip

func (c *cli) runLPSSProbe() int { return c.fail(errNotLinux) }
