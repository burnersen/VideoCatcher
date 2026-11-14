// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

//go:build !windows

package main

import "errors"

// TrayTexts come from the page, in its language (see tray_windows.go).
type TrayTexts struct {
	Open         string `json:"open"`
	Quit         string `json:"quit"`
	Running      string `json:"running"`
	BalloonTitle string `json:"balloonTitle"`
	BalloonText  string `json:"balloonText"`
	AllDoneTitle string `json:"allDoneTitle"`
	AllDoneText  string `json:"allDoneText"`
	AllDoneBad   string `json:"allDoneBad"`
	FailedTitle  string `json:"failedTitle"`
	FailedText   string `json:"failedText"`
}

// trayIcon: Linux comes later (E9); without an icon the close button
// always quits, and downloads continue at the next start.
type trayIcon struct{}

func newTray(func(), func()) (*trayIcon, error) {
	return nil, errors.New("no notification area icon on this system yet")
}

func (*trayIcon) SetTexts(TrayTexts)      {}
func (*trayIcon) Show(string, bool) error { return errors.New("no tray") }
func (*trayIcon) Hide()                   {}
func (*trayIcon) SetTip(string)           {}
func (*trayIcon) Notify(string, string)   {}
func (*trayIcon) Visible() bool           { return false }
func (*trayIcon) Texts() TrayTexts        { return TrayTexts{} }
func (*trayIcon) Close()                  {}
