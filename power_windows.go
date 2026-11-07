// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Sleep, hibernate and shut down for "when all downloads are done"
// (afterall.go).

import (
	"golang.org/x/sys/windows"
)

var procSetSuspendState = windows.NewLazySystemDLL("powrprof.dll").NewProc("SetSuspendState")

// suspendPC puts the PC to sleep, or into hibernation. Called directly,
// SetSuspendState(FALSE) sleeps also when hibernation is on (the known
// trouble is only with "rundll32 powrprof.dll,SetSuspendState").
func suspendPC(hibernate bool) error {
	h := uintptr(0)
	if hibernate {
		h = 1
	}
	if r, _, err := procSetSuspendState.Call(h, 0, 0); r == 0 {
		return err
	}
	return nil
}

// shutdownPC turns the PC off like the start menu does: programs are asked
// to close (VideoCatcher saves its list then), nothing is forced.
func shutdownPC() error {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return err
	}
	defer tok.Close() // ignore: only a handle
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeShutdownPrivilege"), &luid); err != nil {
		return err
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	if err := windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil); err != nil {
		return err
	}
	return windows.ExitWindowsEx(windows.EWX_SHUTDOWN|windows.EWX_POWEROFF,
		windows.SHTDN_REASON_MAJOR_APPLICATION|windows.SHTDN_REASON_MINOR_OTHER|windows.SHTDN_REASON_FLAG_PLANNED)
}
