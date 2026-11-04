package ledctl

func (c *Ctl) setColor(led, r, g, b string, _ ...string) int {
	if !c.requireLED(led) {
		return 1
	}
	for _, v := range []string{r, g, b} {
		if !isByte(v) {
			c.errf("ERROR: RGB values must be between 0 and 255.\n")
			return 1
		}
	}
	return c.tool("set", led, "color", r, g, b)
}

// setBrightness: 0 switches the LED off; any other value also switches an off LED on.
func (c *Ctl) setBrightness(led, value string) int {
	if !c.requireLED(led) {
		return 1
	}
	if !isByte(value) {
		c.errf("ERROR: Brightness must be between 0 and 255.\n")
		return 1
	}
	if n, _ := atoi(value); n == 0 {
		return c.tool("set", led, "off")
	}
	if c.tool("set", led, "brightness", value) != 0 {
		return 1
	}
	if c.ledMode(led) == "off" {
		c.tool("set", led, "on")
	}
	return 0
}

func (c *Ctl) setOn(led string) int {
	if !c.requireLED(led) {
		return 1
	}
	if rc := c.tool("set", led, "brightness", "255"); rc != 0 {
		return rc
	}
	return c.tool("set", led, "on")
}

func (c *Ctl) setOff(led string) int {
	if !c.requireLED(led) {
		return 1
	}
	return c.tool("set", led, "off")
}

func (c *Ctl) setEffect(effect, led, on, off string) int {
	if !c.requireLED(led) {
		return 1
	}
	return c.tool("set", led, effect, on, off)
}

// stopEffect goes back to a steady light; an LED that is off stays off.
func (c *Ctl) stopEffect(led string) int {
	if !c.requireLED(led) {
		return 1
	}
	switch c.ledMode(led) {
	case "blink", "breath":
		return c.tool("set", led, "on")
	}
	return 0
}
