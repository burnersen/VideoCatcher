// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// What a download borrows from the browser besides the cookies: the request
// headers the player sent for the video. Sites that work with tokens often
// want more than a Referer: an "Authorization: Bearer ...", an Origin, a
// header of their own ("X-Token"), and the same User-Agent the login was
// made with.
//
// Such headers are logins like cookies. They are written into a file of
// their own for one job, in yt-dlp's configuration format, and yt-dlp reads
// that file (--config-locations); they never go into the download list, the
// log or a command line (08 S3). The User-Agent is no secret: it is kept
// with the job and passed as an argument.
//
// yt-dlp sends these headers with every request of the download, also to
// another host (the parts of a stream on a CDN). That is what the option can
// do; the browser sent them to the host of the video only.

import (
	"regexp"
	"sort"
	"strings"
)

// Limits (05 K9).
const (
	maxHeaders        = 12   // a player sends two or three of its own
	maxHeaderValueLen = 4096 // a signed token (JWT) with some claims
	maxUserAgentLen   = 400  // browsers stay below 200
)

// The same pattern yt-dlp accepts for a header name ([\w-]+), in ASCII.
var headerNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Headers that are not taken from the browser: yt-dlp sets them itself for
// each request (Host, Range, Accept ...), they have their own way (Cookie,
// Referer, User-Agent), or they describe the browser and not the login.
var skipHeaders = map[string]bool{
	"accept": true, "accept-encoding": true, "accept-language": true, "cache-control": true, "connection": true,
	"content-length": true, "content-type": true, "cookie": true, "dnt": true, "expect": true, "host": true,
	"keep-alive": true, "pragma": true, "priority": true, "range": true, "referer": true, "te": true,
	"trailer": true, "transfer-encoding": true, "upgrade": true, "upgrade-insecure-requests": true,
	"user-agent": true, "via": true, "x-client-data": true,
}
var skipHeaderPrefixes = []string{"sec-", "proxy-", "if-"}

type header struct{ Name, Value string }

// headerText accepts a header value: printable ASCII, no space at the ends.
// Anything else is no header a browser sent, and it must not be able to
// start a new line in the file yt-dlp reads.
func headerText(v string, limit int) bool {
	if v == "" || len(v) > limit || v != strings.TrimSpace(v) {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return false
		}
	}
	return true
}

// splitHeaders checks what the extension sent: the User-Agent goes out by
// itself, the headers worth sending are sorted by name. What is not valid
// is left out; the download then goes without it.
func splitHeaders(in map[string]string) (userAgent string, rest []header) {
	for name, value := range in {
		low := strings.ToLower(name)
		if low == "user-agent" {
			if headerText(value, maxUserAgentLen) {
				userAgent = value
			}
			continue
		}
		if !headerNamePattern.MatchString(name) || skipHeaders[low] || !headerText(value, maxHeaderValueLen) {
			continue
		}
		skip := false
		for _, p := range skipHeaderPrefixes {
			skip = skip || strings.HasPrefix(low, p)
		}
		if !skip {
			rest = append(rest, header{name, value})
		}
	}
	sort.Slice(rest, func(i, k int) bool { return strings.ToLower(rest[i].Name) < strings.ToLower(rest[k].Name) })
	if len(rest) > maxHeaders {
		rest = rest[:maxHeaders]
	}
	return userAgent, rest
}

// headerConfig writes the headers as a yt-dlp configuration file: one
// --add-headers per line, the value in double quotes. yt-dlp splits the
// file like a shell does; inside double quotes only \ and " need a \.
func headerConfig(hs []header) string {
	var b strings.Builder
	b.WriteString("# coding: utf-8\n# Written by " + displayName + " for one download.\n")
	quote := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	for _, h := range hs {
		b.WriteString(`--add-headers "` + quote.Replace(h.Name+":"+h.Value) + "\"\n")
	}
	return b.String()
}
