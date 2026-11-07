// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// The helper for the browser extension on Windows: starting the window so it
// outlives the helper, and telling the browsers where the helper is. Only
// HKEY_CURRENT_USER is touched (no administrator rights, 08 S9), and only
// after the user turned it on in the settings.

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The places where the browsers look for helpers of the current user
// (checked 2026-11-07: developer.chrome.com native messaging, Microsoft
// Edge docs, MDN "Native manifests"). Chromium and Brave read the key of
// Google Chrome too, so one key covers them.
var nativeHostKeys = []struct {
	key     string
	firefox bool
}{
	{`Software\Google\Chrome\NativeMessagingHosts\` + nativeHostName, false},
	{`Software\Chromium\NativeMessagingHosts\` + nativeHostName, false},
	{`Software\Microsoft\Edge\NativeMessagingHosts\` + nativeHostName, false},
	{`Software\Mozilla\NativeMessagingHosts\` + nativeHostName, true},
}

// startDetached starts the window so it keeps running after the helper ends.
// Firefox puts its helpers into a job object that ends every process in it
// when the helper ends; CREATE_BREAKAWAY_FROM_JOB takes the window out of it.
// Where a job does not allow that, the start fails with "access denied" and
// is tried again without the flag (Chrome does not end the window anyway).
func startDetached(exe string, args ...string) error {
	start := func(flags uint32) error {
		cmd := exec.Command(exe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	err := start(windows.CREATE_BREAKAWAY_FROM_JOB | windows.CREATE_NEW_PROCESS_GROUP)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		err = start(windows.CREATE_NEW_PROCESS_GROUP)
	}
	if err != nil {
		return fmt.Errorf("start %s: %w", exe, err)
	}
	return nil
}

// nativeHostState says whether the browsers know the helper: "on" (all
// keys point to manifests naming exe), "moved" (they point to manifests of
// another exe, for example an older copy in another folder) or "off".
func nativeHostState(dataDir, exe string) string {
	cp, fp := manifestFiles(dataDir)
	seen, current := 0, 0
	for _, k := range nativeHostKeys {
		key, err := registry.OpenKey(registry.CURRENT_USER, k.key, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue("")
		key.Close() // read only: a close error changes nothing
		if err != nil {
			continue
		}
		seen++
		want := cp
		if k.firefox {
			want = fp
		}
		if v == want && manifestPointsTo(want, exe) {
			current++
		}
	}
	switch {
	case current == len(nativeHostKeys):
		return "on"
	case seen > 0:
		return "moved"
	}
	return "off"
}

func registerNativeHost(dataDir, exe string) error {
	if err := writeManifests(dataDir, exe); err != nil {
		return err
	}
	cp, fp := manifestFiles(dataDir)
	for _, k := range nativeHostKeys {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, k.key, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("create %s: %w", k.key, err)
		}
		p := cp
		if k.firefox {
			p = fp
		}
		err = key.SetStringValue("", p)
		key.Close() // the value is written or err says why
		if err != nil {
			return fmt.Errorf("write %s: %w", k.key, err)
		}
	}
	return nil
}

func unregisterNativeHost() error {
	for _, k := range nativeHostKeys {
		err := registry.DeleteKey(registry.CURRENT_USER, k.key)
		if err != nil && !errors.Is(err, registry.ErrNotExist) && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return fmt.Errorf("remove %s: %w", k.key, err)
		}
	}
	return nil
}
