// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

//go:build !windows

package main

import "errors"

// Linux comes later (E9); until then the window offers only "quit".
var errPowerNotHere = errors.New("not available on this system yet")

func suspendPC(bool) error { return errPowerNotHere }
func shutdownPC() error    { return errPowerNotHere }
