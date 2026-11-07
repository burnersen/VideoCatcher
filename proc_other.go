// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

//go:build !windows

package main

import "os/exec"

func prepareCmd(*exec.Cmd) {}

// afterStart returns how to stop the process and what to do when it ended.
func afterStart(cmd *exec.Cmd) (kill func() error, cleanup func()) {
	return cmd.Process.Kill, func() {}
}
