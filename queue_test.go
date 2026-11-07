// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestQueue(t *testing.T, r runner) (*queue, string) {
	t.Helper()
	data := t.TempDir()
	q := newQueue(r, newToolSet(data), allTools, data, slog.New(slog.DiscardHandler))
	t.Cleanup(q.Close)
	return q, data
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func jobByID(q *queue, id string) Job {
	for _, j := range q.List() {
		if j.ID == id {
			return j
		}
	}
	return Job{}
}

func TestJobRunsAndFinishes(t *testing.T) {
	var seenTemp string
	r := &fakeRunner{lines: func(args []string) []string {
		for i, a := range args {
			if a == "-P" && strings.HasPrefix(args[i+1], "temp:") {
				seenTemp = strings.TrimPrefix(args[i+1], "temp:")
			}
		}
		os.WriteFile(filepath.Join(seenTemp, "x.part"), []byte("half"), 0o644) // ignore: the test checks it is gone
		return []string{
			"VCITEM NA|NA|abc|The real title",
			"VCPROG downloading|500|1000|NA|100|5|NA|NA|137",
			"WARNING: something minor",
			"VCPROG finished|1000|1000|NA|NA|NA|NA|NA|137",
			"VCPOST started|Merger",
			`VCFILE C:\Videos\The real title [abc].mp4`,
		}
	}}
	q, _ := newTestQueue(t, r)
	j, err := q.Add("https://example.org/watch?v=abc", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
	got := jobByID(q, j.ID)
	if got.Title != "The real title" || got.Percent != 100 || len(got.Files) != 1 || len(got.Details) != 1 {
		t.Fatalf("%+v", got)
	}
	// README promise: no unfinished parts are left behind.
	if _, err := os.Stat(seenTemp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp folder %s still there: %v", seenTemp, err)
	}
}

func TestOnlyParallelJobsRunAtOnce(t *testing.T) {
	r := &fakeRunner{wait: true}
	q, _ := newTestQueue(t, r)
	q.SetParallel(2)
	var ids []string
	for i := 0; i < 3; i++ {
		j, err := q.Add("https://example.org/"+string(rune('a'+i)), testOptions(t), "window", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	waitFor(t, "two running", func() bool { return q.Running() == 2 })
	if s := jobByID(q, ids[2]).Status; s != jobWaiting {
		t.Fatalf("third job is %s", s)
	}
	if err := q.Cancel(ids[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first stopped", func() bool { return jobByID(q, ids[0]).Status == jobCancelled })
	waitFor(t, "third started", func() bool { return jobByID(q, ids[2]).Status == jobRunning })
}

func TestFailedJobIsExplainedAndCanBeRetried(t *testing.T) {
	r := &fakeRunner{lines: func([]string) []string {
		return []string{"ERROR: [youtube] abc: Video unavailable"}
	}, err: errors.New("exit code 1")}
	q, _ := newTestQueue(t, r)
	j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
	got := jobByID(q, j.ID)
	if !strings.HasPrefix(got.Error, "The video is not available\n") || len(got.Details) != 1 {
		t.Fatalf("%+v", got)
	}
	r.err, r.lines = nil, nil
	if err := q.Retry(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done after retry", func() bool { return jobByID(q, j.ID).Status == jobDone })
}

// A link from a web page (videocatcher://) never starts by itself.
func TestLinksFromOutsideWaitForTheUser(t *testing.T) {
	r := &fakeRunner{}
	q, _ := newTestQueue(t, r)
	j, err := q.Add("https://example.org/v", testOptions(t), "protocol", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if s := jobByID(q, j.ID).Status; s != jobConfirm || len(r.calls) != 0 {
		t.Fatalf("status %s, %d runs", s, len(r.calls))
	}
	if err := q.Confirm(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done after OK", func() bool { return jobByID(q, j.ID).Status == jobDone })
}

func TestBadInputIsRefused(t *testing.T) {
	q, _ := newTestQueue(t, &fakeRunner{})
	if _, err := q.Add("file:///C:/Windows", testOptions(t), "window", "", "", ""); err == nil {
		t.Error("file link accepted")
	}
	o := testOptions(t)
	o.Template = "../x.%(ext)s"
	if _, err := q.Add("https://example.org", o, "window", "", "", ""); err == nil {
		t.Error("template outside the folder accepted")
	}
	if len(q.List()) != 0 {
		t.Error("refused jobs are in the list")
	}
}

// Closing VideoCatcher while downloads run: they start again next time.
func TestListSurvivesARestart(t *testing.T) {
	r := &fakeRunner{wait: true}
	data := t.TempDir()
	q := newQueue(r, newToolSet(data), allTools, data, slog.New(slog.DiscardHandler))
	j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "My video", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running", func() bool { return q.Running() == 1 })
	q.Close()

	q2 := newQueue(&fakeRunner{}, newToolSet(data), func(string) bool { return false }, data, slog.New(slog.DiscardHandler))
	if err := q2.load(); err != nil {
		t.Fatal(err)
	}
	got := q2.List()
	if len(got) != 1 || got[0].ID != j.ID || got[0].Status != jobWaiting || got[0].Title != "My video" {
		t.Fatalf("%+v", got)
	}
}

func TestNewerListIsNotOverwritten(t *testing.T) {
	data := t.TempDir()
	file := filepath.Join(data, "queue.json")
	if err := os.WriteFile(file, []byte(`{"format": 7, "jobs": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	q := newQueue(&fakeRunner{}, newToolSet(data), allTools, data, slog.New(slog.DiscardHandler))
	if err := q.load(); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Fatalf("%v", err)
	}
}

func TestCookieFileIsRemovedWhenTheJobEnds(t *testing.T) {
	cookies := filepath.Join(t.TempDir(), "c.txt")
	if err := os.WriteFile(cookies, []byte("# Netscape HTTP Cookie File"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	q, _ := newTestQueue(t, r)
	j, err := q.Add("https://example.org/v", testOptions(t), "extension", "", "", cookies)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
	if !pair(r.calls[0], "--cookies", cookies) {
		t.Fatalf("cookie file not passed: %v", r.calls[0])
	}
	if _, err := os.Stat(cookies); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cookie file still there: %v", err)
	}
}

// The saved list holds links and titles, never cookies.
func TestSavedListHasNoCookies(t *testing.T) {
	q, data := newTestQueue(t, &fakeRunner{wait: true})
	if _, err := q.Add("https://example.org/v", testOptions(t), "extension", "", "", "/secret/cookies.txt"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(data, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatalf("cookie file in the list: %s", b)
	}
}

// A job with a name of its own keeps its title: for a stream from the
// extension, yt-dlp's title is only the stream file's name (0.2.0).
func TestOwnNameKeepsTitle(t *testing.T) {
	r := &fakeRunner{lines: func([]string) []string {
		return []string{"VCITEM NA|NA|index-f3-v1-a1|index-f3-v1-a1", "VCPROG finished|1000|1000|NA|NA|NA|NA|NA|0"}
	}}
	q, _ := newTestQueue(t, r)
	o := testOptions(t)
	o.FileName = "The page title"
	j, err := q.Add("https://cdn.example.org/hls/index-f3-v1-a1.m3u8", o, "extension", "", "The page title", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
	if got := jobByID(q, j.ID).Title; got != "The page title" {
		t.Fatalf("title %q", got)
	}
}
