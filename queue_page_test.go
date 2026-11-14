// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func linkOf(args []string) string { return args[len(args)-1] }

func argAfter(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// loginFiles makes a cookie and a header file where the extension's
// request would have put them.
func loginFiles(t *testing.T, q *queue) (cookies, headers string) {
	t.Helper()
	if err := os.MkdirAll(q.cookieDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cookies, headers = filepath.Join(q.cookieDir, "c-1.txt"), filepath.Join(q.cookieDir, "h-1.conf")
	for _, f := range []string{cookies, headers} {
		if err := os.WriteFile(f, []byte("# x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cookies, headers
}

// A stream link with a token the site no longer takes: the job tries the
// page it was found on, once, with the cookies and the browser's name but
// without what belonged to the request of the link. The user sees it
// waiting, not failed.
func TestRefusedLinkTriesThePage(t *testing.T) {
	var runs [][]string
	r := &stepRunner{steps: []func([]string) ([]string, error){
		func(args []string) ([]string, error) {
			runs = append(runs, args)
			return []string{"ERROR: [generic] Unable to download webpage: HTTP Error 403: Forbidden (caused by <HTTPError 403: Forbidden>)"}, errors.New("exit code 1")
		},
		func(args []string) ([]string, error) {
			runs = append(runs, args)
			return []string{"VCPROG finished|1000|1000|NA|NA|NA|NA|NA|137"}, nil
		},
	}}
	q, _ := newTestQueue(t, r)
	cookies, headers := loginFiles(t, q)
	var stages []string
	q.onChange = func(j Job) { stages = append(stages, j.Status+": "+j.Stage) } // one job: the calls come one after the other
	j, err := q.AddWith("https://cdn.example/a/master.m3u8?token=old", testOptions(t), "extension", "https://player.example/e/1", "T", cookies,
		jobExtras{PageURL: "https://paid.example/watch/1", UserAgent: "Mozilla/5.0 Test", HeaderFile: headers})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
	if len(runs) != 2 {
		t.Fatalf("%d runs", len(runs))
	}
	first, second := runs[0], runs[1]
	if linkOf(first) != "https://cdn.example/a/master.m3u8?token=old" || argAfter(first, "--config-locations") != headers ||
		argAfter(first, "--cookies") != cookies || !pair(first, "--add-headers", "Referer:https://player.example/e/1") {
		t.Errorf("first run: %v", first)
	}
	if linkOf(second) != "https://paid.example/watch/1" || slices.Contains(second, "--config-locations") ||
		argAfter(second, "--cookies") != cookies || !pair(second, "--add-headers", "User-Agent:Mozilla/5.0 Test") ||
		pair(second, "--add-headers", "Referer:https://player.example/e/1") {
		t.Errorf("second run: %v", second)
	}
	if seen := strings.Join(stages, "\n"); !strings.Contains(seen, "waiting: link refused, trying the page instead") || strings.Contains(seen, jobFailed) {
		t.Errorf("stages:\n%s", seen)
	}
	for _, f := range []string{cookies, headers} {
		if fileExists(f) {
			t.Errorf("%s stays after the job is done", f)
		}
	}
	got := jobByID(q, j.ID)
	if got.CookieName != "" || got.HeaderName != "" || !got.ViaPage {
		t.Errorf("%+v", got)
	}
}

// The page does not help either: the job fails once, and the message says
// what happened (the link was too old), not what yt-dlp thinks of the page.
func TestRefusedLinkAndPageFail(t *testing.T) {
	r := &stepRunner{steps: []func([]string) ([]string, error){
		func([]string) ([]string, error) {
			return []string{"ERROR: unable to download video data: HTTP Error 403: Forbidden"}, errors.New("exit code 1")
		},
		func([]string) ([]string, error) {
			return []string{"ERROR: Unsupported URL: https://paid.example/watch/1"}, errors.New("exit code 1")
		},
	}}
	q, _ := newTestQueue(t, r)
	j, err := q.AddWith("https://cdn.example/a.mp4?token=old", testOptions(t), "extension", "", "T", "", jobExtras{PageURL: "https://paid.example/watch/1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
	got := jobByID(q, j.ID)
	if r.runs() != 2 || !strings.HasPrefix(got.Error, expiredMsg+"\n") {
		t.Errorf("runs %d, error %q", r.runs(), got.Error)
	}
	// A clear word of the page counts more.
	if err := explainExpired([]string{"ERROR: This video is DRM protected"}, nil); !strings.Contains(err.Error(), "DRM") {
		t.Errorf("%v", err)
	}
}

// No page to fall back on, the page itself, or another kind of error: the
// job fails as before, after one run.
func TestNoPageNoSecondRun(t *testing.T) {
	for name, c := range map[string]struct{ link, page, line string }{
		"no page":       {"https://cdn.example/a.mp4", "", "ERROR: HTTP Error 403: Forbidden"},
		"the page":      {"https://paid.example/watch/1", "https://paid.example/watch/1", "ERROR: HTTP Error 403: Forbidden"},
		"other trouble": {"https://cdn.example/a.mp4", "https://paid.example/watch/1", "ERROR: This video is DRM protected"},
	} {
		r := &stepRunner{steps: []func([]string) ([]string, error){
			func([]string) ([]string, error) { return []string{c.line}, errors.New("exit code 1") },
		}}
		q, _ := newTestQueue(t, r)
		j, err := q.AddWith(c.link, testOptions(t), "extension", "", "T", "", jobExtras{PageURL: c.page})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
		if r.runs() != 1 || jobByID(q, j.ID).ViaPage {
			t.Errorf("%s: %d runs", name, r.runs())
		}
	}
}

// A paused job keeps its header file over a restart, like its cookies.
func TestHeaderFileSurvivesARestart(t *testing.T) {
	q, data := newTestQueue(t, &fakeRunner{})
	q.SetParallel(1)
	q.Hold()
	cookies, headers := loginFiles(t, q)
	j, err := q.AddWith("https://cdn.example/a.m3u8", testOptions(t), "extension", "", "T", cookies, jobExtras{HeaderFile: headers})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Pause(j.ID); err != nil {
		t.Fatal(err)
	}
	q2 := newQueue(&fakeRunner{}, newToolSet(data), allTools, data, q.logger)
	t.Cleanup(q2.Close)
	q2.Hold()
	if err := q2.load(); err != nil {
		t.Fatal(err)
	}
	if c, h := q2.loginFilesOf(j.ID); c != cookies || h != headers || !fileExists(headers) {
		t.Errorf("after the restart: %q %q", c, h)
	}
	if raw, _ := os.ReadFile(filepath.Join(data, "queue.json")); strings.Contains(string(raw), q.cookieDir) {
		t.Error("queue.json has the path of a login file")
	}
	q2.Remove(j.ID)
	if fileExists(headers) || fileExists(cookies) {
		t.Error("login files stay after the job is removed")
	}
}
