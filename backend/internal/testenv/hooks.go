//go:build testhooks

// Package testenv gates the environment variables that only tests use: fake roots (UGREEN_SYS, UGREEN_DMI, ...), tool
// overrides (UGREEN_LED_I2C, UGREEN_DMIDECODE, UGREEN_NAS_FAN, ...), UGREEN_PRODUCT, UGREEN_LAYOUT, UGREEN_NOW, UGREEN_LED_CHIP.
// Release builds compile Get to an empty string, so no environment of a root process (cron, php-fpm) can point the
// tools at another binary or tree. Tests build with -tags testhooks.
package testenv

import "os"

const Enabled = true

func Get(key string) string { return os.Getenv(key) }
