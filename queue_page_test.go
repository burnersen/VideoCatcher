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
	if len(runs) != 3 {
		t.Fatalf("%d runs", len(runs))
	}
	// In between, the same link once more, answering like a browser.
	first, browser, second := runs[0], runs[1], runs[2]
	if linkOf(browser) != linkOf(first) || argAfter(browser, "--impersonate") != "chrome" || argAfter(browser, "--cookies") != cookies ||
		argAfter(browser, "--config-locations") != headers || slices.Contains(first, "--impersonate") || argAfter(second, "--impersonate") != "chrome" {
		t.Errorf("run as a browser: %v", browser)
	}
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
	if r.runs() != 3 || !strings.HasPrefix(got.Error, expiredMsg+"\n") {
		t.Errorf("runs %d, error %q", r.runs(), got.Error)
	}
	// A clear word of the page counts more.
	if err := explainEnd([]string{"ERROR: HTTP Error 410: Gone"}, []string{"ERROR: This video is DRM protected"}, nil, true, false); !strings.Contains(err.Error(), "DRM") {
		t.Errorf("%v", err)
	}
}

// No page to fall back on, the page itself, or another kind of error: the
// page is not tried. A plain "no" gets its one run as a browser.
func TestNoPageNoSecondRun(t *testing.T) {
	for name, c := range map[string]struct {
		link, page, line string
		runs             int
	}{
		"no page":       {"https://cdn.example/a.mp4", "", "ERROR: HTTP Error 403: Forbidden", 2},
		"the page":      {"https://paid.example/watch/1", "https://paid.example/watch/1", "ERROR: HTTP Error 403: Forbidden", 2},
		"other trouble": {"https://cdn.example/a.mp4", "https://paid.example/watch/1", "ERROR: This video is DRM protected", 1},
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
		if r.runs() != c.runs || jobByID(q, j.ID).ViaPage {
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

const wallLine = `ERROR: [generic] Got HTTP Error 403 caused by Cloudflare anti-bot challenge; try again with  --extractor-args "generic:impersonate"`

// A bot wall in front of the site: the job runs once more answering like
// the browser the link comes from, and the user sees it waiting.
func TestBotWallTriesAsABrowser(t *testing.T) {
	for ua, target := range map[string]string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:144.0) Gecko/20100101 Firefox/144.0":                                              "firefox",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 Edg/142.0.0.0": "chrome",
		"": "chrome",
	} {
		var runs [][]string
		r := &stepRunner{steps: []func([]string) ([]string, error){
			func(args []string) ([]string, error) {
				runs = append(runs, args)
				return []string{wallLine}, errors.New("exit code 1")
			},
			func(args []string) ([]string, error) {
				runs = append(runs, args)
				return []string{"VCPROG finished|1000|1000|NA|NA|NA|NA|NA|137"}, nil
			},
		}}
		q, _ := newTestQueue(t, r)
		var stages []string
		q.onChange = func(j Job) { stages = append(stages, j.Status+": "+j.Stage) }
		j, err := q.AddWith("https://cdn.example/a/master.m3u8?t=1", testOptions(t), "extension", "https://player.example/e/1", "T", "",
			jobExtras{PageURL: "https://site.example/watch/1", UserAgent: ua})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, "done", func() bool { return jobByID(q, j.ID).Status == jobDone })
		if len(runs) != 2 || slices.Contains(runs[0], "--impersonate") || argAfter(runs[1], "--impersonate") != target ||
			linkOf(runs[1]) != linkOf(runs[0]) || !pair(runs[1], "--add-headers", "Referer:https://player.example/e/1") {
			t.Errorf("%q: runs %v", ua, runs)
		}
		if seen := strings.Join(stages, "\n"); !strings.Contains(seen, "waiting: site wants a browser, trying as one") || strings.Contains(seen, jobFailed) {
			t.Errorf("stages:\n%s", seen)
		}
	}
}

// The wall stays shut for the link and for the page: the message names the
// wall, not "the link was too old" and not what yt-dlp thinks of the page,
// and the details keep what yt-dlp said in every try.
func TestBotWallStaysShut(t *testing.T) {
	r := &stepRunner{steps: []func([]string) ([]string, error){
		func([]string) ([]string, error) { return []string{wallLine}, errors.New("exit code 1") },
		func([]string) ([]string, error) { return []string{wallLine}, errors.New("exit code 1") },
		func([]string) ([]string, error) {
			return []string{"WARNING: [generic] Falling back on generic information extractor", "ERROR: Unsupported URL: https://site.example/watch/1"}, errors.New("exit code 1")
		},
	}}
	q, _ := newTestQueue(t, r)
	j, err := q.AddWith("https://cdn.example/a/master.m3u8?t=1", testOptions(t), "extension", "", "T", "", jobExtras{PageURL: "https://site.example/watch/1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
	got := jobByID(q, j.ID)
	if r.runs() != 3 || !strings.HasPrefix(got.Error, wallMsg+"\n") {
		t.Errorf("runs %d, error %q", r.runs(), got.Error)
	}
	details := strings.Join(got.Details, "\n")
	for _, want := range []string{"Cloudflare anti-bot", "Next try: as a browser", "Next try: the page the video was found on", "Unsupported URL"} {
		if !strings.Contains(details, want) {
			t.Errorf("details lack %q:\n%s", want, details)
		}
	}
	// "Try again" starts clean.
	if err := q.Retry(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed again", func() bool { g := jobByID(q, j.ID); return g.Status == jobFailed && r.runs() > 3 })
	if d := strings.Join(jobByID(q, j.ID).Details, "\n"); strings.Count(d, "Unsupported URL") != 1 {
		t.Errorf("details after a new try:\n%s", d)
	}
}

// A yt-dlp without the part for answering like a browser: that try does
// not count, the page is still tried, and the end says what is missing.
func TestBotWallWithoutImpersonation(t *testing.T) {
	var runs [][]string
	missing := `ERROR: Impersonate target "chrome" is not available. Use --list-impersonate-targets to see available targets. This request requires browser impersonation, however you may be missing dependencies required to support this target.`
	step := func(lines ...string) func([]string) ([]string, error) {
		return func(args []string) ([]string, error) {
			runs = append(runs, args)
			return lines, errors.New("exit code 1")
		}
	}
	r := &stepRunner{steps: []func([]string) ([]string, error){
		step(wallLine), step(missing), step("ERROR: Unsupported URL: https://site.example/watch/1"),
	}}
	q, _ := newTestQueue(t, r)
	j, err := q.AddWith("https://cdn.example/a/master.m3u8?t=1", testOptions(t), "extension", "", "T", "", jobExtras{PageURL: "https://site.example/watch/1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed", func() bool { return jobByID(q, j.ID).Status == jobFailed })
	got := jobByID(q, j.ID)
	if len(runs) != 3 || linkOf(runs[2]) != "https://site.example/watch/1" || slices.Contains(runs[2], "--impersonate") || !got.NoBrowser {
		t.Fatalf("runs %v", runs)
	}
	if !strings.HasPrefix(got.Error, "The site wants a real browser, and this yt-dlp cannot answer like one\n") {
		t.Errorf("error %q", got.Error)
	}
}

func TestExplainEnd(t *testing.T) {
	for name, c := range map[string]struct {
		before, last []string
		viaPage      bool
		want         string
	}{
		"one try":             {nil, []string{"ERROR: Unsupported URL: https://a.example/"}, false, "yt-dlp does not know this site"},
		"wall named at first": {[]string{wallLine}, []string{"ERROR: HTTP Error 403: Forbidden"}, false, wallMsg},
		"plain no, twice":     {[]string{"ERROR: HTTP Error 403: Forbidden"}, []string{"ERROR: HTTP Error 403: Forbidden"}, false, "The site refused the download"},
		"clear word at last":  {[]string{wallLine}, []string{"ERROR: This video is only available for registered users"}, true, "logged-in users"},
		"too old":             {[]string{"ERROR: HTTP Error 410: Gone"}, []string{"ERROR: Unsupported URL: https://a.example/"}, true, expiredMsg},
	} {
		if err := explainEnd(c.before, c.last, errors.New("exit code 1"), c.viaPage, false); !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
