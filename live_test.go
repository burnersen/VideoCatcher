// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Live test with the real programs (06 T5): downloads yt-dlp (with the real
// signature check), FFmpeg and Deno, asks yt-dlp about a real video and
// downloads it. Runs only with VIDEOCATCHER_TEST_LIVE=1, in the workflow
// "Live test" on GitHub; never in the normal CI and never in a cloud
// session that may not run downloaded programs.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// liveTestURL is a 10 second test video (1 MB) of a site that offers them
// for exactly this. YouTube is not the default: it blocks the runners of
// GitHub ("This video is unavailable" for yt-dlp's own test video, seen on
// 2026-11-07). VIDEOCATCHER_TEST_URL tests another link by hand.
const liveTestURL = "https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4"

func TestLiveRealDownload(t *testing.T) {
	if os.Getenv("VIDEOCATCHER_TEST_LIVE") != "1" {
		t.Skip("VIDEOCATCHER_TEST_LIVE not set")
	}
	link := liveTestURL
	if u := os.Getenv("VIDEOCATCHER_TEST_URL"); u != "" {
		link = u
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	data := t.TempDir()
	rt, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	tb := newToolbox(newToolSet(data), newHTTPClient(), logger, rt)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := tb.Prepare(ctx, true); err != nil {
		t.Fatalf("prepare: %v\n%+v", err, tb.Statuses())
	}
	t.Logf("tools: %+v", tb.Statuses())

	info, err := probe(ctx, execRunner{}, tb.ts.YtDlp, probeArgs(link, probeSettings{Playlist: "single"}, tb.ts, tb.Ready, ""))
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	t.Logf("probe: %q, %d formats, heights %v", info.Title, len(info.Formats), info.Heights)

	// Through the speed relay, with a limit, as in the window: yt-dlp must
	// download through it (https tunnel and plain http).
	speed := newSpeedControl(50, 8<<20, logger) // 50 % of 8 MiB/s
	defer speed.Close()
	if speed.ProxyURL() == "" {
		t.Fatal("speed relay did not start")
	}
	q := newQueue(execRunner{}, tb.ts, tb.Ready, data, logger)
	q.proxy = speed.ProxyURL
	defer q.Close()
	o := defaultOptions()
	o.OutputDir, o.Quality, o.Container = t.TempDir(), "360", "mp4"
	j, err := q.Add(link, o, "window", "", info.Title, "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Minute)
	for {
		got := jobByID(q, j.ID)
		if isFinished(got.Status) {
			if got.Status != jobDone || len(got.Files) == 0 {
				t.Fatalf("download: %+v", got)
			}
			st, err := os.Stat(got.Files[0])
			if err != nil || st.Size() == 0 {
				t.Fatalf("file %s: %v", got.Files[0], err)
			}
			t.Logf("downloaded %s, %d bytes", got.Files[0], st.Size())
			if through := speed.meter.total.Load(); through < st.Size()/2 {
				t.Fatalf("only %d bytes went through the relay", through)
			}
			if name := filepath.Base(got.Files[0]); strings.ContainsAny(name, " []()") {
				t.Fatalf("file name not tidied: %s", name)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("still not done: %+v", got)
		}
		time.Sleep(time.Second)
	}
}

// liveTestBig: a file large enough to pause in the middle at 1 MiB/s.
const liveTestBig = "https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_5MB.mp4"

// Pause in the middle, continue: yt-dlp must go on from the parts, not
// start from zero (the point of 0.3.0's pause).
func TestLivePauseContinues(t *testing.T) {
	if os.Getenv("VIDEOCATCHER_TEST_LIVE") != "1" {
		t.Skip("VIDEOCATCHER_TEST_LIVE not set")
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	data := t.TempDir()
	rt, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	tb := newToolbox(newToolSet(data), newHTTPClient(), logger, rt)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := tb.Prepare(ctx, true); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	speed := newSpeedControl(25, 4<<20, logger) // 1 MiB/s: the 5 MB file takes about five seconds
	defer speed.Close()
	q := newQueue(execRunner{}, tb.ts, tb.Ready, data, logger)
	q.proxy = speed.ProxyURL
	defer q.Close()

	var mu sync.Mutex
	resumed := false
	var firstAfter float64 = -1
	q.onChange = func(j Job) {
		mu.Lock()
		defer mu.Unlock()
		if resumed && firstAfter < 0 && j.Status == jobRunning && j.Stage == "downloading" && j.Percent > 0 {
			firstAfter = j.Percent
		}
	}
	o := defaultOptions()
	o.OutputDir = t.TempDir()
	j, err := q.Add(liveTestBig, o, "window", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	wait := func(what string, cond func(Job) bool) Job {
		t.Helper()
		deadline := time.Now().Add(5 * time.Minute)
		for {
			got := jobByID(q, j.ID)
			if cond(got) {
				return got
			}
			if isFinished(got.Status) || time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %+v", what, got)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	wait("30 %", func(g Job) bool { return g.Status == jobRunning && g.Percent >= 30 })
	if err := q.Pause(j.ID); err != nil {
		t.Fatal(err)
	}
	p := wait("paused", func(g Job) bool { return g.Status == jobPaused })
	t.Logf("paused at %.0f %%", p.Percent)
	mu.Lock()
	resumed = true
	mu.Unlock()
	if err := q.Resume(j.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for jobByID(q, j.ID).Status != jobDone {
		if g := jobByID(q, j.ID); g.Status == jobFailed || time.Now().After(deadline) {
			t.Fatalf("after continue: %+v", g)
		}
		time.Sleep(200 * time.Millisecond)
	}
	mu.Lock()
	first := firstAfter
	mu.Unlock()
	t.Logf("first progress after continue: %.0f %%", first)
	if first < 20 {
		t.Fatalf("continued at %.0f %%: the download started again from the beginning", first)
	}
	st, err := os.Stat(jobByID(q, j.ID).Files[0])
	if err != nil || st.Size() < 4<<20 {
		t.Fatalf("file: %v", err)
	}
}
