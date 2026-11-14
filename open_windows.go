// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Explorer and the videocatcher:// link, on Windows.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// showInFolder opens Explorer with the file selected. Explorer reads its
// command line itself, so the argument is passed as it must look there.
func showInFolder(path string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`explorer.exe /select,"%s"`, path)}
	// Explorer ends with exit code 1 even when it worked.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start explorer: %w", err)
	}
	go cmd.Wait() // ignore: its exit code means nothing, see above
	return nil
}

// openFile opens a file with the program Windows has for its kind, like a
// double click in Explorer. The caller decides which kinds (app.go,
// playable).
func openFile(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	verb, _ := windows.UTF16PtrFromString("open") // ignore: constant text without NUL
	if err := windows.ShellExecute(0, verb, p, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	return nil
}

func openFolder(dir string) error {
	cmd := exec.Command("explorer.exe", dir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start explorer: %w", err)
	}
	go cmd.Wait() // ignore: its exit code means nothing, see above
	return nil
}

// The videocatcher:// link is registered for the current user only
// (HKEY_CURRENT_USER, no administrator rights, 08 S9), and only when the
// user turns it on in the settings.
const protocolKey = `Software\Classes\` + protocolScheme

func protocolRegistered() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, protocolKey+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close() // read only: a close error changes nothing
	v, _, err := k.GetStringValue("")
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	return err == nil && v == protocolCommand(exe)
}

func protocolCommand(exe string) string { return `"` + exe + `" "%1"` }

func registerProtocol() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find own program: %w", err)
	}
	root, _, err := registry.CreateKey(registry.CURRENT_USER, protocolKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s: %w", protocolKey, err)
	}
	defer root.Close() // written values are kept either way
	if err := root.SetStringValue("", "URL:"+displayName); err != nil {
		return fmt.Errorf("write %s: %w", protocolKey, err)
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return fmt.Errorf("write %s: %w", protocolKey, err)
	}
	cmd, _, err := registry.CreateKey(registry.CURRENT_USER, protocolKey+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s command: %w", protocolKey, err)
	}
	defer cmd.Close() // written values are kept either way
	return cmd.SetStringValue("", protocolCommand(exe))
}

func unregisterProtocol() error {
	for _, k := range []string{`\shell\open\command`, `\shell\open`, `\shell`, ``} {
		err := registry.DeleteKey(registry.CURRENT_USER, protocolKey+k)
		if err != nil && !errors.Is(err, registry.ErrNotExist) && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return fmt.Errorf("remove %s%s: %w", protocolKey, k, err)
		}
	}
	return nil
}
