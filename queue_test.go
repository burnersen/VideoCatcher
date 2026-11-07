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
	"sync"
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

// tempOf finds the folder for unfinished parts in yt-dlp's arguments.
func tempOf(args []string) string {
	for i, a := range args {
		if a == "-P" && i+1 < len(args) && strings.HasPrefix(args[i+1], "temp:") {
			return strings.TrimPrefix(args[i+1], "temp:")
		}
	}
	return ""
}

// Pause keeps the parts, Continue starts yt-dlp again on the same folder,
// so it goes on where it stopped instead of starting from zero (0.2.0
// threw the parts away).
func TestPauseKeepsPartsAndContinues(t *testing.T) {
	var mu sync.Mutex
	var temps []string
	r := &fakeRunner{wait: true, lines: func(args []string) []string {
		temp := tempOf(args)
		mu.Lock()
		temps = append(temps, temp)
		mu.Unlock()
		os.WriteFile(filepath.Join(temp, "v.mp4.part"), []byte("half"), 0o644) // ignore: checked below
		return []string{"VCPROG downloading|500|1000|NA|100|5|NA|NA|137"}
	}}
	q, _ := newTestQueue(t, r)
	j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running", func() bool { return jobByID(q, j.ID).Percent == 50 })
	if err := q.Pause(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "paused", func() bool { return jobByID(q, j.ID).Status == jobPaused })
	if q.Active() != 0 {
		t.Fatalf("a paused job counts as active")
	}
	mu.Lock()
	first := temps[0]
	mu.Unlock()
	if _, err := os.Stat(filepath.Join(first, "v.mp4.part")); err != nil {
		t.Fatalf("parts gone after pause: %v", err)
	}
	if got := jobByID(q, j.ID); got.Percent != 50 || !got.Resumable {
		t.Fatalf("%+v", got)
	}

	if err := q.Resume(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running again", func() bool { return q.Running() == 1 })
	if got := jobByID(q, j.ID); got.Stage != "continuing" && got.Stage != "downloading" {
		t.Fatalf("stage %q", got.Stage)
	}
	waitFor(t, "second run", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) == 2 })
	mu.Lock()
	same := len(temps) == 2 && temps[0] == temps[1]
	mu.Unlock()
	if !same {
		t.Fatal("the second run uses another folder")
	}

	// Stop throws the parts away.
	if err := q.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stopped", func() bool { return jobByID(q, j.ID).Status == jobCancelled })
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parts still there after stop: %v", err)
	}
}

// A paused job that is stopped or removed from the list loses its parts.
func TestStoppingAPausedJobRemovesItsParts(t *testing.T) {
	r := &fakeRunner{wait: true}
	q, data := newTestQueue(t, r)
	for _, remove := range []bool{false, true} {
		j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "running", func() bool { return jobByID(q, j.ID).Status == jobRunning })
		q.Pause(j.ID)
		waitFor(t, "paused", func() bool { return jobByID(q, j.ID).Status == jobPaused })
		parts := filepath.Join(data, "partial", j.ID)
		if _, err := os.Stat(parts); err != nil {
			t.Fatalf("no parts folder: %v", err)
		}
		if remove {
			q.Remove(j.ID)
		} else {
			q.Cancel(j.ID)
		}
		if _, err := os.Stat(parts); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove=%v: parts still there: %v", remove, err)
		}
	}
}

// Pause on a waiting job holds it; Pause all and Continue all work on the
// whole list.
func TestPauseAllAndResumeAll(t *testing.T) {
	r := &fakeRunner{wait: true}
	q, _ := newTestQueue(t, r)
	q.SetParallel(1)
	a, _ := q.Add("https://example.org/a", testOptions(t), "window", "", "", "")
	b, _ := q.Add("https://example.org/b", testOptions(t), "window", "", "", "")
	waitFor(t, "a running", func() bool { return jobByID(q, a.ID).Status == jobRunning })
	q.PauseAll()
	waitFor(t, "both paused", func() bool {
		return jobByID(q, a.ID).Status == jobPaused && jobByID(q, b.ID).Status == jobPaused
	})
	if q.Running() != 0 {
		t.Fatal("something still runs")
	}
	q.ResumeAll()
	waitFor(t, "one runs again", func() bool { return q.Running() == 1 })
	if q.Active() != 2 {
		t.Fatalf("active %d", q.Active())
	}
}

// Closing VideoCatcher during a download keeps the parts; after the next
// start the job continues them. Parts of jobs that are gone are removed.
func TestRestartContinuesParts(t *testing.T) {
	data := t.TempDir()
	q := newQueue(&fakeRunner{wait: true}, newToolSet(data), allTools, data, slog.New(slog.DiscardHandler))
	j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running", func() bool { return q.Running() == 1 })
	q.Close()
	parts := filepath.Join(data, "partial", j.ID)
	if _, err := os.Stat(parts); err != nil {
		t.Fatalf("parts gone after close: %v", err)
	}
	orphan := filepath.Join(data, "partial", "gone-job")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}

	q2 := newQueue(&fakeRunner{}, newToolSet(data), func(string) bool { return false }, data, slog.New(slog.DiscardHandler))
	if err := q2.load(); err != nil {
		t.Fatal(err)
	}
	got := q2.List()
	if len(got) != 1 || got[0].Status != jobWaiting || !got[0].Resumable || got[0].Stage != "interrupted, continues" {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(parts); err != nil {
		t.Fatalf("parts of the waiting job removed: %v", err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan parts kept: %v", err)
	}
}

// A paused job from the extension keeps its login over a restart: queue.json
// has the name of the cookie file, never the cookies.
func TestPausedJobKeepsItsCookiesOverARestart(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "cookies")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cookies := filepath.Join(dir, "c-123.txt")
	if err := os.WriteFile(cookies, []byte("# Netscape HTTP Cookie File\nsecretvalue"), 0o600); err != nil {
		t.Fatal(err)
	}
	q := newQueue(&fakeRunner{wait: true}, newToolSet(data), allTools, data, slog.New(slog.DiscardHandler))
	j, err := q.Add("https://example.org/v", testOptions(t), "extension", "", "", cookies)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running", func() bool { return q.Running() == 1 })
	q.Pause(j.ID)
	waitFor(t, "paused", func() bool { return jobByID(q, j.ID).Status == jobPaused })
	q.Close()
	b, err := os.ReadFile(filepath.Join(data, "queue.json"))
	if err != nil || strings.Contains(string(b), "secretvalue") {
		t.Fatalf("queue.json: %v %s", err, b)
	}

	r := &fakeRunner{}
	q2 := newQueue(r, newToolSet(data), allTools, data, slog.New(slog.DiscardHandler))
	t.Cleanup(q2.Close)
	if err := q2.load(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cookies); err != nil {
		t.Fatalf("cookie file removed at start: %v", err)
	}
	q2.Resume(j.ID)
	waitFor(t, "done", func() bool { return jobByID(q2, j.ID).Status == jobDone })
	if !pair(r.calls[0], "--cookies", cookies) {
		t.Fatalf("cookies not used after the restart: %v", r.calls[0])
	}
	if _, err := os.Stat(cookies); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cookie file still there after the job: %v", err)
	}
}

// After the download the file gets its tidy name, and so does a subtitle
// that belongs to it; the job lists the new name.
func TestFilesGetTidyNames(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Holiday 2026 (Part #2) [abc].mp4")
	sub := filepath.Join(dir, "Holiday 2026 (Part #2) [abc].de.srt")
	r := &fakeRunner{lines: func([]string) []string {
		os.WriteFile(video, []byte("v"), 0o644) // ignore: checked below
		os.WriteFile(sub, []byte("s"), 0o644)   // ignore: checked below
		return []string{"VCFILE " + video}
	}}
	q, _ := newTestQueue(t, r)
	o := testOptions(t)
	o.CleanNames = true
	j, err := q.Add("https://example.org/v", o, "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
	want := filepath.Join(dir, "Holiday.2026.Part.Nr2.abc.mp4")
	if got := jobByID(q, j.ID).Files; len(got) != 1 || got[0] != want {
		t.Fatalf("files %v", got)
	}
	for _, p := range []string{want, filepath.Join(dir, "Holiday.2026.Part.Nr2.abc.de.srt")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNamesStayWhenCleaningIsOff(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Holiday 2026 [abc].mp4")
	r := &fakeRunner{lines: func([]string) []string {
		os.WriteFile(video, []byte("v"), 0o644) // ignore: checked below
		return []string{"VCFILE " + video}
	}}
	q, _ := newTestQueue(t, r)
	o := testOptions(t)
	o.CleanNames = false
	j, _ := q.Add("https://example.org/v", o, "window", "", "", "")
	waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
	if _, err := os.Stat(video); err != nil {
		t.Fatal(err)
	}
}

// The relay's password never reaches the job's details or the log, also if
// yt-dlp names its proxy in an error.
func TestProxyPasswordIsHidden(t *testing.T) {
	r := &fakeRunner{lines: func(args []string) []string {
		for i, a := range args {
			if a == "--proxy" {
				return []string{"ERROR: Unable to connect to proxy " + args[i+1]}
			}
		}
		return []string{"ERROR: no proxy given"}
	}, err: errors.New("exit code 1")}
	q, _ := newTestQueue(t, r)
	q.proxy = func() string { return "http://vc:s3cretpass@127.0.0.1:5555" }
	j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
	got := jobByID(q, j.ID)
	if !strings.Contains(strings.Join(got.Details, ""), "vc:***@127.0.0.1") || strings.Contains(got.Error+strings.Join(got.Details, ""), "s3cretpass") {
		t.Fatalf("%+v", got)
	}
}
