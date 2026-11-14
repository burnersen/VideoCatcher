// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Is there a newer VideoCatcher? Asked together with the look for a new
// yt-dlp (at start if the setting allows it, and with "Check for
// updates"). The window then shows a note with a link to the release
// page. Nothing is downloaded or installed: the user decides, and a new
// exe is not signed by a key built in here like yt-dlp's is.

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
)

// appRepo is where the releases are. A variable only so tests can point it
// at a local server.
var appRepo = "https://github.com/burnersen/" + appName

// A release tag is "v1.2.3"; a version is the same without the "v".
// Anything else (a tag of another kind, a local build that says "dev") is
// not compared.
var appVersionPattern = regexp.MustCompile(`^v?([0-9]{1,5})\.([0-9]{1,5})\.([0-9]{1,5})$`)

func versionNumbers(v string) (n [3]int, ok bool) {
	m := appVersionPattern.FindStringSubmatch(v)
	if m == nil {
		return n, false
	}
	for i := range n {
		n[i], _ = strconv.Atoi(m[i+1]) // ignore: the pattern allows only digits
	}
	return n, true
}

// newerVersion says whether latest is a newer version than current.
func newerVersion(current, latest string) bool {
	c, ok1 := versionNumbers(current)
	l, ok2 := versionNumbers(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// newerApp returns the version of a newer release ("1.2.3"), or "" when
// this one is the newest or cannot be compared.
func newerApp(ctx context.Context, client *http.Client, current string) (string, error) {
	if _, ok := versionNumbers(current); !ok {
		return "", nil // a local build
	}
	tag, err := latestReleaseTag(ctx, client, appRepo, "the "+displayName+" releases")
	if err != nil || !newerVersion(current, tag) {
		return "", err
	}
	n, _ := versionNumbers(tag) // ignore: newerVersion has checked the form
	return strconv.Itoa(n[0]) + "." + strconv.Itoa(n[1]) + "." + strconv.Itoa(n[2]), nil
}

// releasePage is the page of one release. The version comes from
// newerApp, so it is three numbers and nothing else.
func releasePage(version string) string { return appRepo + "/releases/tag/v" + version }
