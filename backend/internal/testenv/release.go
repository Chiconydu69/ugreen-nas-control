//go:build !testhooks

// Package testenv gates the environment variables that only tests use; see hooks.go. This is the release build: none are honoured.
package testenv

const Enabled = false

func Get(string) string { return "" }
