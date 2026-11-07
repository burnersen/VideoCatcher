// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"testing"
	"time"
)

// The tray window starts, takes calls from other threads and ends without
// hanging. Whether the icon shows depends on the desktop of the test runner
// (a service session has no notification area), so that is only logged.
func TestTrayStartsAndCloses(t *testing.T) {
	for round := 0; round < 2; round++ { // a second tray after the first is closed
		tr, err := newTray(func() {}, func() {})
		if err != nil {
			t.Fatal(err)
		}
		tr.SetTexts(TrayTexts{Open: "Öffnen", Quit: "Beenden", Running: "{n}", BalloonTitle: "Video-Catcher", BalloonText: "läuft weiter"})
		if err := tr.Show("Video-Catcher: 1", true); err != nil {
			t.Logf("icon not shown here: %v", err)
		}
		tr.SetTip("Video-Catcher: 2")
		tr.Hide()
		if tr.Visible() {
			t.Fatal("still visible after Hide")
		}
		done := make(chan struct{})
		go func() { tr.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Close hangs")
		}
	}
}

// Long texts are cut to the fixed Win32 fields and still end in NUL.
func TestPutUTF16Cuts(t *testing.T) {
	var field [8]uint16
	putUTF16(field[:], "a very long tooltip")
	if field[7] != 0 || field[0] != 'a' {
		t.Fatalf("%v", field)
	}
}
