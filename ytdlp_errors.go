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

const loginHint = "Log in to the site in your browser, then choose it at \"Logins from browser\" (More options) and try again. " +
	"Or send the video with the browser extension and tick \"Send the site's login (cookies) along\"."

// The first matching rule wins, so the specific ones come first. The texts
// are taken from yt-dlp's source and from real error reports.
var errorRules = []errorRule{
	{any: []string{"cookie database", "failed to decrypt", "dpapi", "app-bound", "could not find chrome cookies", "could not find firefox cookies", "could not copy"},
		msg:  "The cookies of the chosen browser could not be read",
		hint: "Close that browser completely and try again. Chrome and Edge often block this on Windows; Firefox works best."},
	{any: []string{"sign in to confirm", "not a bot", "login required", "http error 401", "unauthorized", "requires authentication", "only available for registered users", "you need to log in", "this video is private", "private video", "members-only", "join this channel", "premium", "subscriber", "purchase", "paid"},
		msg:  "The site only shows this video to logged-in users",
		hint: loginHint},
	{any: []string{"confirm your age", "age-restricted", "age restricted", "inappropriate for some users"},
		msg:  "The video has an age check",
		hint: loginHint},
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
		msg: "yt-dlp does not know this site and found no video on this page",
		hint: "A page or a player of a site yt-dlp has no reader for cannot be downloaded by its address. " +
			"In the browser extension, send a stream from its list (HLS, DASH) instead of the page or the player. " +
			"If the list shows none, the site keeps its stream out of sight."},
	{any: []string{"requested format is not available", "no video formats found", "requested formats are incompatible"},
		msg:  "The chosen quality or format is not available for this video",
		hint: `Choose "best" or another quality and try again.`},
	{any: []string{"ffmpeg is not installed", "ffmpeg not found", "ffprobe not found", "ffprobe and ffmpeg not found"},
		msg:  "FFmpeg is missing for this download",
		hint: "Click \"Check for updates\" so VideoCatcher can install it, then try again."},
	{any: []string{"impersonate target", "impersonation dependency", "missing dependencies"},
		msg:  "The site wants a real browser, and this yt-dlp cannot answer like one",
		hint: "Click \"Check for updates\" to get the newest yt-dlp, then try again."},
	{any: wallWords,
		msg: wallMsg, hint: wallHint},
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
	{any: []string{"unable to download webpage", "getaddrinfo failed", "name or service not known", "timed out", "connection reset", "connection refused", "network is unreachable", "temporary failure in name resolution", "ssl", "tunnel connection failed"},
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
	if r := ruleFor(cause); r != nil {
		return userError(errors.New(cause), r.msg, r.hint)
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
	r := ruleFor(strings.Join(errorLines, "\n"))
	return r != nil && r.again
}

// ruleFor is the first rule that matches yt-dlp's error text, or nil.
func ruleFor(text string) *errorRule {
	all := strings.ToLower(text)
	if all == "" {
		return nil
	}
	for i, r := range errorRules {
		for _, s := range r.any {
			if strings.Contains(all, s) {
				return &errorRules[i]
			}
		}
	}
	return nil
}

// A bot wall: the site (mostly Cloudflare in front of it) looks at who is
// asking and lets only browsers in.
var wallWords = []string{"cloudflare", "anti-bot", "generic:impersonate"}

const (
	wallMsg  = "The site checks for a real browser and did not let Video-Catcher in"
	wallHint = "Open the page in your browser until it plays, then send the video again with the browser extension right away: " +
		"it passes along the proof that you got in. If that fails too, tick \"Send the site's login (cookies) along\" there."
)

func hasAny(errorLines []string, words []string) bool {
	all := strings.ToLower(strings.Join(errorLines, "\n"))
	for _, s := range words {
		if strings.Contains(all, s) {
			return true
		}
	}
	return false
}

func botWall(errorLines []string) bool { return hasAny(errorLines, wallWords) }

// wantsBrowser says whether a failed run may work when yt-dlp answers like
// a browser: the site named its bot wall, or it said no (403) without a
// reason, which is what such a wall looks like from most sites. A job
// tries that once (queue.go).
func wantsBrowser(errorLines []string) bool {
	return botWall(errorLines) || hasAny(errorLines, []string{"http error 403", "forbidden"})
}

// cannotImpersonate says whether yt-dlp ended because it has nothing to
// answer like a browser with (the part for it is missing in this build).
func cannotImpersonate(errorLines []string) bool {
	return hasAny(errorLines, []string{"impersonate target", "impersonation dependency"})
}

// linkRefused says whether the site turned the link itself down: no
// permission (401, 403) or gone (410). That is what a link with a token in
// it looks like when the token is too old or was made for someone else. A
// job from the browser extension then tries the page the link was found on
// (queue.go).
func linkRefused(errorLines []string) bool {
	all := strings.ToLower(strings.Join(errorLines, "\n"))
	for _, s := range []string{"http error 401", "http error 403", "http error 410", "unauthorized", "forbidden", "expired"} {
		if strings.Contains(all, s) {
			return true
		}
	}
	return false
}

// expiredMsg and expiredHint are for a link from the browser that the site
// refused, after the page it was found on did not work either.
const (
	expiredMsg  = "The site no longer accepts the link of this video"
	expiredHint = "Such links are only good for a short time, and often only for the player in the browser. " +
		"Open the page again, start the video and send it once more; tick \"Send the site's login (cookies) along\" if you are logged in there."
)

// explainEnd explains the end of a job that tried more than one way. A
// clear word of the last try counts (login, DRM, country). Without one,
// the user is told what stood in the way from the start: a bot wall, a
// yt-dlp that cannot answer like a browser, or a link that was too old.
// The cause holds the lines of all tries.
func explainEnd(before, errorLines []string, exitErr error, viaPage, noBrowser bool) error {
	all := append(append([]string{}, before...), errorLines...)
	cause := strings.Join(all, "\n")
	if cause == "" && exitErr != nil {
		cause = exitErr.Error()
	}
	if len(before) == 0 {
		return explainYtDlp(errorLines, exitErr)
	}
	last := strings.ToLower(strings.Join(errorLines, "\n"))
	if r := ruleFor(last); r != nil && !wantsBrowser(errorLines) && !linkRefused(errorLines) && !strings.Contains(last, "unsupported url") {
		return userError(errors.New(cause), r.msg, r.hint)
	}
	switch {
	case botWall(all) && noBrowser:
		r := ruleFor("impersonate target")
		return userError(errors.New(cause), r.msg, r.hint)
	case botWall(all):
		return userError(errors.New(cause), wallMsg, wallHint)
	case viaPage:
		return userError(errors.New(cause), expiredMsg, expiredHint)
	}
	if r := ruleFor(strings.Join(before, "\n")); r != nil {
		return userError(errors.New(cause), r.msg, r.hint)
	}
	return explainYtDlp(all, exitErr)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
