// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import "testing"

func TestTrayNews(t *testing.T) {
	n := newTrayNews()
	tx := defaultTrayTexts()
	say := func(id, status string, active int) string {
		title, text := n.Update(Job{ID: id, Status: status, Title: "Video " + id, URL: "https://example.org/" + id}, active, tx)
		if title == "" {
			return ""
		}
		return title + ": " + text
	}
	steps := []struct {
		id, status string
		active     int
		want       string
	}{
		{"a", jobRunning, 3, ""},
		{"a", jobDone, 2, ""}, // others still run: no balloon per download
		{"b", jobFailed, 1, "A download failed: Video b"},
		{"c", jobDone, 0, "All downloads are done: 2 done, 1 failed"},
		// The next batch starts at zero.
		{"d", jobRunning, 1, ""},
		{"d", jobDone, 0, "All downloads are done: 1 done"},
		// Pausing or stopping the last one is the user's doing.
		{"e", jobRunning, 1, ""},
		{"e", jobPaused, 0, ""},
		{"f", jobCancelled, 0, ""},
		// A failed one that is tried again and then works counts once, as done.
		{"g", jobFailed, 1, "A download failed: Video g"},
		{"g", jobWaiting, 2, ""},
		{"g", jobDone, 1, ""},
		{"h", jobDone, 0, "All downloads are done: 2 done"},
	}
	for i, s := range steps {
		if got := say(s.id, s.status, s.active); got != s.want {
			t.Fatalf("step %d (%s %s): %q, want %q", i, s.id, s.status, got, s.want)
		}
	}
	// A job without a title is named by its site.
	if _, text := n.Update(Job{ID: "x", Status: jobFailed, URL: "https://www.example.org/v"}, 1, tx); text != "www.example.org" && text != "example.org" {
		t.Fatalf("%q", text)
	}
}
