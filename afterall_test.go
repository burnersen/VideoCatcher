// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"testing"
	"time"
)

type fakeTimer struct {
	f       func()
	stopped bool
}

func (t *fakeTimer) Stop() bool { t.stopped = true; return true }

func testWatch() (*finishWatch, *[]string, func() *fakeTimer) {
	var fired []string
	var last *fakeTimer
	w := newFinishWatch(func(a string) { fired = append(fired, a) })
	w.afterFunc = func(_ time.Duration, f func()) stopper { last = &fakeTimer{f: f}; return last }
	return w, &fired, func() *fakeTimer { return last }
}

// Downloads run, the last one ends: the countdown starts, and when it ends
// the action happens once.
func TestAfterAllRunsOnceWhenDone(t *testing.T) {
	w, fired, timer := testWatch()
	if err := w.Set("shutdown", 2); err != nil {
		t.Fatal(err)
	}
	w.Update(1)
	if timer() != nil {
		t.Fatal("countdown while a download runs")
	}
	w.Update(0)
	if timer() == nil || w.State().Seconds == 0 {
		t.Fatal("no countdown after the last download")
	}
	timer().f()
	if len(*fired) != 1 || (*fired)[0] != "shutdown" {
		t.Fatalf("fired %v", *fired)
	}
	if w.State().Action != "none" {
		t.Fatal("the choice stays after it happened")
	}
	w.Update(1)
	w.Update(0)
	if len(*fired) != 1 {
		t.Fatal("happened twice")
	}
}

// Chosen while nothing runs: it waits for the next downloads.
func TestAfterAllWaitsForDownloads(t *testing.T) {
	w, _, timer := testWatch()
	w.Set("sleep", 0)
	w.Update(0)
	if timer() != nil || !w.State().Waiting {
		t.Fatalf("started without downloads: %+v", w.State())
	}
	w.Update(1)
	w.Update(0)
	if timer() == nil {
		t.Fatal("no countdown after the downloads")
	}
}

// A new download stops the countdown; Abort stops everything.
func TestAfterAllCanBeStopped(t *testing.T) {
	w, fired, timer := testWatch()
	w.Set("hibernate", 1)
	w.Update(0)
	first := timer()
	w.Update(1)
	if !first.stopped {
		t.Fatal("a new download did not stop the countdown")
	}
	w.Update(0)
	w.Abort()
	if !timer().stopped || w.State().Action != "none" {
		t.Fatal("abort did not stop")
	}
	timer().f() // a timer that fires late does nothing
	if len(*fired) != 0 {
		t.Fatalf("fired %v", *fired)
	}
	if err := w.Set("reboot", 0); err == nil {
		t.Fatal("unknown action accepted")
	}
}
