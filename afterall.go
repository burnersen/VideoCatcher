// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// "When all downloads are done": quit VideoCatcher, sleep, hibernate or shut
// the PC down. The choice holds for this run only (a PC that turns itself
// off a week later because of a forgotten setting is no help) and is
// carried out once, after a countdown in the window that can be stopped.
//
// It waits for downloads that run or wait; paused ones and links waiting for
// an OK do not end by themselves and do not count. Chosen while nothing
// runs, it waits for the next downloads.

import (
	"errors"
	"sync"
	"time"
)

var afterAllChoices = []string{"none", "quit", "sleep", "hibernate", "shutdown"}

// afterAllDelay: time to stop it, also for someone who comes to the PC
// only when the window comes to the front.
const afterAllDelay = 60 * time.Second

// AfterAllState is what the window shows.
type AfterAllState struct {
	Action  string `json:"action"`
	Seconds int    `json:"seconds"` // > 0: counting down
	Waiting bool   `json:"waiting"` // chosen, but no download has run since
}

type stopper interface{ Stop() bool }

type finishWatch struct {
	mu        sync.Mutex
	action    string
	busy      bool // a download ran since the choice
	timer     stopper
	deadline  time.Time
	delay     time.Duration
	afterFunc func(time.Duration, func()) stopper
	onChange  func(AfterAllState) // the window; may be nil
	onFire    func(action string)
}

func newFinishWatch(onFire func(string)) *finishWatch {
	return &finishWatch{action: "none", delay: afterAllDelay, onFire: onFire,
		afterFunc: func(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) }}
}

// Set chooses what happens; active is the number of downloads now.
func (w *finishWatch) Set(action string, active int) error {
	if oneOf("action", action, afterAllChoices) != nil {
		return userError(errors.New("after all: "+action), "This choice is not valid", "Choose a value from the list.")
	}
	w.mu.Lock()
	w.stopLocked()
	w.action, w.busy = action, active > 0
	st := w.stateLocked()
	w.mu.Unlock()
	w.tell(st)
	return nil
}

// Update is called whenever a download changed.
func (w *finishWatch) Update(active int) {
	w.mu.Lock()
	if w.action == "none" {
		w.mu.Unlock()
		return
	}
	changed := false
	switch {
	case active > 0:
		changed = !w.busy || w.timer != nil
		w.busy = true
		w.stopLocked() // a new download stops the countdown
	case w.busy && w.timer == nil:
		w.busy, changed = false, true
		w.deadline = time.Now().Add(w.delay)
		w.timer = w.afterFunc(w.delay, w.fire)
	}
	st := w.stateLocked()
	w.mu.Unlock()
	if changed {
		w.tell(st)
	}
}

// Abort stops the countdown and the choice.
func (w *finishWatch) Abort() {
	w.mu.Lock()
	w.stopLocked()
	w.action, w.busy = "none", false
	st := w.stateLocked()
	w.mu.Unlock()
	w.tell(st)
}

func (w *finishWatch) State() AfterAllState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stateLocked()
}

func (w *finishWatch) fire() {
	w.mu.Lock()
	if w.timer == nil { // stopped in the meantime
		w.mu.Unlock()
		return
	}
	a := w.action
	w.timer, w.action, w.busy = nil, "none", false // once; after waking up the PC nothing repeats
	st := w.stateLocked()
	w.mu.Unlock()
	w.tell(st)
	if a != "none" && w.onFire != nil {
		w.onFire(a)
	}
}

func (w *finishWatch) stopLocked() {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *finishWatch) stateLocked() AfterAllState {
	st := AfterAllState{Action: w.action, Waiting: w.action != "none" && !w.busy && w.timer == nil}
	if w.timer != nil {
		st.Seconds = max(1, int(time.Until(w.deadline).Round(time.Second)/time.Second))
	}
	return st
}

func (w *finishWatch) tell(st AfterAllState) {
	if w.onChange != nil {
		w.onChange(st)
	}
}
