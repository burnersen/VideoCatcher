// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

//go:build !windows

package main

// The file manager and the videocatcher:// link on other systems: the file
// manager through xdg-open; the link is not offered yet (Linux comes later).

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
)

func showInFolder(path string) error { return openFolder(filepath.Dir(path)) }

func openFile(path string) error {
	cmd := exec.Command("xdg-open", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start xdg-open: %w", err)
	}
	go cmd.Wait() // ignore: the player runs on its own
	return nil
}

func openFolder(dir string) error {
	cmd := exec.Command("xdg-open", dir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start xdg-open: %w", err)
	}
	go cmd.Wait() // ignore: the file manager runs on its own
	return nil
}

var errNoProtocol = errors.New("browser links are only available on Windows for now")

func protocolRegistered() bool  { return false }
func registerProtocol() error   { return errNoProtocol }
func unregisterProtocol() error { return errNoProtocol }
