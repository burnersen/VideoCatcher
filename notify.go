// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// What the icon in the notification area tells while the window is away:
// one balloon when a download failed, one when all are done. Not one per
// finished download: a playlist of thirty links would be thirty balloons.
//
// "All done" is said when the last download that ran or waited has ended by
// itself (done or failed). Pausing or stopping the last one is the user's
// own doing and needs no news.

import (
	"strconv"
	"strings"
	"sync"
)

func defaultTrayTexts() TrayTexts {
	return TrayTexts{Open: "Open " + displayName, Quit: "Quit", Running: displayName + ": {n} downloads",
		AllDoneTitle: "All downloads are done", AllDoneText: "{d} done", AllDoneBad: "{d} done, {f} failed",
		FailedTitle: "A download failed", FailedText: "{t}"}
}

// trayNews counts what ended since the downloads last began, and says when
// there is something to tell.
type trayNews struct {
	mu     sync.Mutex
	done   map[string]bool // jobs that ended by themselves in this batch
	failed map[string]bool
}

func newTrayNews() *trayNews {
	return &trayNews{done: map[string]bool{}, failed: map[string]bool{}}
}

// Update takes a changed job and the number of downloads that still run or
// wait. It returns the balloon to show, or "" for none.
func (n *trayNews) Update(j Job, active int, tx TrayTexts) (title, text string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	switch j.Status {
	case jobDone:
		n.done[j.ID] = true
		delete(n.failed, j.ID)
	case jobFailed:
		n.failed[j.ID] = true
		delete(n.done, j.ID)
	default:
		// Tried again, removed from the count, or simply progress.
		delete(n.done, j.ID)
		delete(n.failed, j.ID)
		return "", ""
	}
	if active > 0 {
		if j.Status == jobFailed {
			name := j.Title
			if name == "" {
				name = hostOf(j.URL)
			}
			return tx.FailedTitle, strings.ReplaceAll(tx.FailedText, "{t}", name)
		}
		return "", ""
	}
	d, f := len(n.done), len(n.failed)
	n.done, n.failed = map[string]bool{}, map[string]bool{}
	text = tx.AllDoneText
	if f > 0 {
		text = tx.AllDoneBad
	}
	text = strings.ReplaceAll(text, "{d}", strconv.Itoa(d))
	return tx.AllDoneTitle, strings.ReplaceAll(text, "{f}", strconv.Itoa(f))
}
