// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

//go:build !windows

package main

// The helper for the browser extension on other systems: it runs (the tests
// use it), but registering it with the browsers comes with Linux later.

import (
	"errors"
	"fmt"
	"os/exec"
)

func startDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", exe, err)
	}
	return cmd.Process.Release()
}

var errNoNativeHost = errors.New("the browser extension is only available on Windows for now")

func nativeHostState(string, string) string   { return "off" }
func registerNativeHost(string, string) error { return errNoNativeHost }
func unregisterNativeHost() error             { return errNoNativeHost }
