// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Turns yt-dlp's error lines into calm sentences a user can act on. yt-dlp
// itself writes for developers ("ERROR: [youtube] abc: Sign in to confirm
// you're not a bot. Use --cookies-from-browser or --cookies ..."). The
// original text stays in the error for the log and the details view.

import (
	"errors"
	"strings"
)

type errorRule struct {
	any  []string // the error matches if its lower case text contains one of these
	msg  string
	hint string
	// again: the trouble is on the way to the site, not with the video.
	// The queue tries such a download again by itself (queue.go).
	again bool
}

// The first matching rule wins, so the specific ones come first. The texts
// are taken from yt-dlp's source and from real error reports.
var errorRules = []errorRule{
	{any: []string{"cookie database", "failed to decrypt", "dpapi", "app-bound", "could not find chrome cookies", "could not find firefox cookies", "could not copy"},
		msg:  "The cookies of the chosen browser could not be read",
		hint: "Close that browser completely and try again. Chrome and Edge often block this on Windows; Firefox works best."},
	{any: []string{"sign in to confirm", "not a bot", "login required", "requires authentication", "only available for registered users", "you need to log in", "this video is private", "private video", "members-only", "join this channel", "premium", "subscriber", "purchase", "paid"},
		msg:  "The site only shows this video to logged-in users",
		hint: "Log in to the site in your browser, then choose it at \"Logins from browser\" (More options) and try again."},
	{any: []string{"confirm your age", "age-restricted", "age restricted", "inappropriate for some users"},
		msg:  "The video has an age check",
		hint: "Log in to the site in your browser, then choose it at \"Logins from browser\" (More options) and try again."},
	{any: []string{"drm"},
		msg:  "The video is protected against copying (DRM)",
		hint: "yt-dlp cannot download protected videos."},
	{any: []string{"not available in your country", "geo restrict", "geo-restrict", "blocked it in your country"},
		msg:  "The video is not available in your country",
		hint: "The site decides this by the location of your internet connection."},
	{any: []string{"premieres in", "live event will begin", "this live event", "is upcoming"},
		msg:  "The video has not started yet",
		hint: "Try again when it is live or has been published."},
	{any: []string{"unsupported url"},
		msg:  "yt-dlp does not know this site or this kind of link",
		hint: "Check the link. If the page plays a video, try the link of the video page itself."},
	{any: []string{"requested format is not available", "no video formats found", "requested formats are incompatible"},
		msg:  "The chosen quality or format is not available for this video",
		hint: `Choose "best" or another quality and try again.`},
	{any: []string{"ffmpeg is not installed", "ffmpeg not found", "ffprobe not found", "ffprobe and ffmpeg not found"},
		msg:  "FFmpeg is missing for this download",
		hint: "Click \"Check for updates\" so VideoCatcher can install it, then try again."},
	{any: []string{"http error 429", "too many requests", "rate-limit", "rate limit"},
		msg:  "The site is limiting downloads right now",
		hint: "Wait a few minutes and try again, or download fewer videos at the same time."},
	{any: []string{"http error 403", "forbidden"},
		msg:  "The site refused the download",
		hint: "Click \"Check for updates\"; a new yt-dlp often fixes this. Cookies from your browser can help too."},
	{any: []string{"video unavailable", "is unavailable", "is not available", "has been removed", "http error 404", "does not exist", "no longer available", "account associated with this video has been terminated"},
		msg:  "The video is not available",
		hint: "It may have been deleted or made private. Check the link in your browser."},
	{any: []string{"no space left", "errno 28", "disk full"},
		msg:  "The disk is full",
		hint: "Free some space or choose another download folder."},
	{any: []string{"permission denied", "errno 13", "access is denied"},
		msg:  "VideoCatcher may not write into the download folder",
		hint: "Choose another folder, or check that no other program has the file open."},
	{any: []string{"unable to download webpage", "getaddrinfo failed", "name or service not known", "timed out", "connection reset", "connection refused", "network is unreachable", "temporary failure in name resolution", "ssl"},
		msg:  "The site could not be reached",
		hint: "Check the internet connection and try again.", again: true},
}

// explainYtDlp makes one UserError out of the error lines of a run. The
// cause holds all lines, so the log and the details view show yt-dlp's own
// words.
func explainYtDlp(errorLines []string, exitErr error) error {
	cause := strings.Join(errorLines, "\n")
	if cause == "" && exitErr != nil {
		cause = exitErr.Error()
	}
	all := strings.ToLower(cause)
	for _, r := range errorRules {
		for _, s := range r.any {
			if strings.Contains(all, s) {
				return userError(errors.New(cause), r.msg, r.hint)
			}
		}
	}
	short := firstLine(cause)
	if len(short) > 200 {
		short = short[:200] + "..."
	}
	hint := "Click \"Check for updates\" and try again. The details show what yt-dlp said."
	if short != "" {
		hint = "yt-dlp says: " + short
	}
	return userError(errors.New(cause), "yt-dlp could not download this", hint)
}

// worthAnotherTry says whether a failed run ended for a reason that may be
// gone a moment later (the connection dropped, the name was not found).
// The first matching rule decides, like in explainYtDlp: "HTTP Error 404"
// in an "unable to download webpage" line is no reason to try again.
func worthAnotherTry(errorLines []string) bool {
	all := strings.ToLower(strings.Join(errorLines, "\n"))
	if all == "" {
		return false
	}
	for _, r := range errorRules {
		for _, s := range r.any {
			if strings.Contains(all, s) {
				return r.again
			}
		}
	}
	return false
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
