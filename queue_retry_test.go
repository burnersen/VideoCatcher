// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stepRunner plays one prepared run of yt-dlp after the other; the last
// one repeats.
type stepRunner struct {
	mu    sync.Mutex
	n     int
	steps []func(args []string) ([]string, error)
}

func (s *stepRunner) Run(_ context.Context, _ string, args []string, onLine func(bool, string)) error {
	s.mu.Lock()
	step := s.steps[min(s.n, len(s.steps)-1)]
	s.n++
	s.mu.Unlock()
	lines, err := step(args)
	for _, l := range lines {
		onLine(strings.HasPrefix(l, "ERROR") || strings.HasPrefix(l, "WARNING"), l)
	}
	return err
}

func (s *stepRunner) runs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// The connection drops: the download waits a moment and tries again by
// itself, on the parts it has. The user sees it waiting, not failed.
func TestNetworkTroubleTriesAgainOnTheSameParts(t *testing.T) {
	var firstTemp string
	var partThere bool
	r := &stepRunner{steps: []func([]string) ([]string, error){
		func(args []string) ([]string, error) {
			firstTemp = tempOf(args)
			os.WriteFile(filepath.Join(firstTemp, "v.mp4.part"), []byte("half"), 0o644) // ignore: checked in the second run
			return []string{"VCPROG downloading|500|1000|NA|100|5|NA|NA|137",
				"ERROR: [generic] Unable to download webpage: <urlopen error [Errno 11001] getaddrinfo failed>"}, errors.New("exit code 1")
		},
		func(args []string) ([]string, error) {
			_, err := os.Stat(filepath.Join(tempOf(args), "v.mp4.part"))
			partThere = err == nil && tempOf(args) == firstTemp
			return []string{"VCPROG finished|1000|1000|NA|NA|NA|NA|NA|137"}, nil
		},
	}}
	q, _ := newTestQueue(t, r)
	q.delays = []time.Duration{20 * time.Millisecond, 20 * time.Millisecond}
	var mu sync.Mutex
	var stages []string
	q.onChange = func(j Job) {
		mu.Lock()
		stages = append(stages, j.Status+": "+j.Stage)
		mu.Unlock()
	}
	j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
	if r.runs() != 2 || !partThere {
		t.Fatalf("runs %d, part kept for the second run: %v", r.runs(), partThere)
	}
	mu.Lock()
	seen := strings.Join(stages, "\n")
	mu.Unlock()
	if !strings.Contains(seen, "waiting: site not reached, trying again soon") || strings.Contains(seen, jobFailed) {
		t.Fatalf("stages:\n%s", seen)
	}
	if got := jobByID(q, j.ID); got.Tries != 0 || got.Error != "" || !got.RetryAt.IsZero() {
		t.Fatalf("%+v", got)
	}
}

// The tries are limited: a site that stays away ends as failed, with the
// usual message, and the parts go.
func TestNetworkTroubleGivesUpAfterItsTries(t *testing.T) {
	var temp string
	r := &stepRunner{steps: []func([]string) ([]string, error){func(args []string) ([]string, error) {
		temp = tempOf(args)
		return []string{"ERROR: unable to download video data: The read operation timed out"}, errors.New("exit code 1")
	}}}
	q, _ := newTestQueue(t, r)
	q.delays = []time.Duration{time.Millisecond, time.Millisecond}
	j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
	if r.runs() != 3 {
		t.Fatalf("%d runs, want the first and two more", r.runs())
	}
	if got := jobByID(q, j.ID); !strings.HasPrefix(got.Error, "The site could not be reached\n") {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(temp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parts still there: %v", err)
	}
}

// Trouble with the video itself is not tried again: it would fail the same
// way and only keep the user waiting.
func TestOtherErrorsAreNotTriedAgain(t *testing.T) {
	for _, line := range []string{
		"ERROR: [youtube] abc: Sign in to confirm you're not a bot",
		"ERROR: [generic] Unable to download webpage: HTTP Error 404: Not Found",
		"ERROR: This video is DRM protected",
		"ERROR: something nobody knows",
	} {
		if worthAnotherTry([]string{line}) {
			t.Errorf("would try again after %q", line)
		}
	}
	if !worthAnotherTry([]string{"ERROR: [Errno 104] Connection reset by peer"}) || worthAnotherTry(nil) {
		t.Error("connection reset must be tried again, no error lines must not")
	}
}

// A job that waits for its new try can be stopped, and a restart does not
// keep it waiting for a time of the last session.
func TestWaitingForANewTryCanBeStoppedAndSurvivesARestart(t *testing.T) {
	r := &stepRunner{steps: []func([]string) ([]string, error){func([]string) ([]string, error) {
		return []string{"ERROR: [Errno 111] Connection refused"}, errors.New("exit code 1")
	}}}
	q, data := newTestQueue(t, r)
	q.delays = []time.Duration{time.Hour}
	j, err := q.Add("https://example.org/v", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "waiting for the new try", func() bool { g := jobByID(q, j.ID); return g.Status == jobWaiting && !g.RetryAt.IsZero() })
	if q.Active() != 1 || q.Running() != 0 {
		t.Fatalf("active %d running %d", q.Active(), q.Running())
	}

	q2 := newQueue(&fakeRunner{wait: true}, newToolSet(data), noTools, data, q.logger)
	if err := q2.load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q2.Close)
	if got := jobByID(q2, j.ID); got.Status != jobWaiting || !got.RetryAt.IsZero() || !got.Resumable {
		t.Fatalf("after a restart: %+v", got)
	}

	if err := q.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	if got := jobByID(q, j.ID); got.Status != jobCancelled {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(filepath.Join(data, "partial", j.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parts still there: %v", err)
	}
}

// One private video in a playlist: the others are there, so the job is
// done, says how many are missing, and its files can be opened.
func TestPlaylistWithAFailedEntryIsDone(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(dir, "One.mp4"), filepath.Join(dir, "Two.mp4")
	r := &stepRunner{steps: []func([]string) ([]string, error){func([]string) ([]string, error) {
		os.WriteFile(one, []byte("12345"), 0o644) // ignore: the size is checked below
		os.WriteFile(two, []byte("123"), 0o644)   // ignore: the size is checked below
		return []string{
			"VCITEM 1|3|a|One", "VCFILE " + one,
			"VCITEM 2|3|b|NA", "ERROR: [youtube] b: Private video. Sign in if you've been granted access to this video",
			"VCITEM 3|3|c|Two", "VCFILE " + two,
		}, errors.New("exit code 1")
	}}}
	q, _ := newTestQueue(t, r)
	o := testOptions(t)
	o.Playlist, o.CleanNames = "all", false
	j, err := q.Add("https://example.org/list", o, "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
	got := jobByID(q, j.ID)
	if got.Skipped != 1 || len(got.Files) != 2 || got.Size != 8 || got.Stage != "done, but not every video" ||
		!strings.HasPrefix(got.Error, "The site only shows this video to logged-in users\n") || len(got.Details) != 1 {
		t.Fatalf("%+v", got)
	}
}

// A playlist of which nothing arrived stays failed.
func TestPlaylistWithoutAnyFileFails(t *testing.T) {
	r := &stepRunner{steps: []func([]string) ([]string, error){func([]string) ([]string, error) {
		return []string{"ERROR: [youtube] a: Video unavailable"}, errors.New("exit code 1")
	}}}
	q, _ := newTestQueue(t, r)
	o := testOptions(t)
	o.Playlist = "all"
	j, err := q.Add("https://example.org/list", o, "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
}

// "Next" puts a waiting job before the older waiting ones.
func TestStartNextGoesBeforeOlderJobs(t *testing.T) {
	r := &fakeRunner{wait: true}
	q, _ := newTestQueue(t, r)
	q.SetParallel(1)
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		j, err := q.Add("https://example.org/"+name, testOptions(t), "window", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	waitFor(t, "first running", func() bool { return jobByID(q, ids[0]).Status == jobRunning })
	if err := q.StartNext(ids[2]); err != nil {
		t.Fatal(err)
	}
	if got := jobByID(q, ids[2]); !got.Next || got.Stage != "next" {
		t.Fatalf("%+v", got)
	}
	if err := q.Cancel(ids[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "third running", func() bool { return jobByID(q, ids[2]).Status == jobRunning })
	if got := jobByID(q, ids[1]); got.Status != jobWaiting {
		t.Fatalf("second job is %s", got.Status)
	}
	if got := jobByID(q, ids[2]); got.Next {
		t.Fatal("a running job is still marked as next")
	}
	if q.StartNext("nope") == nil {
		t.Fatal("unknown job accepted")
	}
}

// "Try the failed ones again" takes failed jobs, not the stopped ones.
func TestRetryFailedTakesOnlyFailedJobs(t *testing.T) {
	r := &stepRunner{steps: []func([]string) ([]string, error){
		func([]string) ([]string, error) {
			return []string{"ERROR: [youtube] abc: Video unavailable"}, errors.New("exit code 1")
		},
		func([]string) ([]string, error) { return nil, nil },
	}}
	q, _ := newTestQueue(t, r)
	failed, err := q.Add("https://example.org/f", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, failed.ID).Status == jobFailed })
	q.Hold()
	stopped, err := q.Add("https://example.org/s", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Cancel(stopped.ID); err != nil {
		t.Fatal(err)
	}
	q.Release()
	if n := q.RetryFailed(); n != 1 {
		t.Fatalf("%d jobs tried again", n)
	}
	waitFor(t, "done", func() bool { return jobByID(q, failed.ID).Status == jobDone })
	if got := jobByID(q, stopped.ID); got.Status != jobCancelled {
		t.Fatalf("%+v", got)
	}
	if n := q.RetryFailed(); n != 0 {
		t.Fatalf("%d jobs tried again, none is failed", n)
	}
}

// The window asks before it adds a link twice: Known finds a link that
// waits, runs or is done, not one that failed or was stopped.
func TestKnownFindsALinkThatIsInTheList(t *testing.T) {
	q, _ := newTestQueue(t, &fakeRunner{wait: true})
	q.Hold()
	if _, ok := q.Known("https://example.org/v"); ok {
		t.Fatal("known before it was added")
	}
	j, err := q.Add(" https://example.org/v ", testOptions(t), "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := q.Known("https://example.org/v"); !ok || got.ID != j.ID {
		t.Fatalf("%v %+v", ok, got)
	}
	if _, ok := q.Known("https://example.org/other"); ok {
		t.Fatal("another link is known")
	}
	if _, ok := q.Known("not a link"); ok {
		t.Fatal("no link is known")
	}
	if err := q.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := q.Known("https://example.org/v"); ok {
		t.Fatal("a stopped job counts as known")
	}
}

// What the window title shows.
func TestProgressIsTheAverageOfRunningJobs(t *testing.T) {
	q, _ := newTestQueue(t, &fakeRunner{wait: true, lines: func(args []string) []string {
		if args[len(args)-1] == "https://example.org/a" {
			return []string{"VCPROG downloading|200|1000|NA|100|5|NA|NA|137"}
		}
		return []string{"VCPROG downloading|600|1000|NA|100|5|NA|NA|137"}
	}})
	if n, _ := q.Progress(); n != 0 {
		t.Fatalf("%d running", n)
	}
	for _, name := range []string{"a", "b"} {
		if _, err := q.Add("https://example.org/"+name, testOptions(t), "window", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "both at their numbers", func() bool { n, p := q.Progress(); return n == 2 && p == 40 })
}
