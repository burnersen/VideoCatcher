// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// A stream whose list never had a link: some players build their HLS list
// in the page's memory (the list of qualities as a blob, each quality
// written into it as a "data:" link) and only the parts are loaded from the
// net. The browser extension passes such a list along as text. Here it is
// checked and written into files yt-dlp can read (--enable-file-urls).
//
// The text comes out of a web page, so nothing in it is trusted. What comes
// out names only two kinds of links: web links (http, https), and files
// this code wrote itself for this one job. A list that names anything else
// (a file of this PC, another program's protocol) is refused as a whole.
//
// What this does not do: read a key out of a player. A list whose parts are
// locked with a key the player keeps to itself (a "blob:" link, or a key
// the site only hands to its own script) stays locked.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxListBytes = 2 << 20 // a list of qualities with each quality written into it
	maxListFiles = 12      // qualities and keys of one list
	maxKeyBytes  = 64      // an AES-128 key has 16
)

// errListKey: the parts are locked and the key is not something a
// downloader can ask for.
var errListKey = errors.New("the key of this stream is kept in the player's memory")

var (
	listURI  = regexp.MustCompile(`URI="([^"]*)"`)
	listHead = regexp.MustCompile(`^\x{FEFF}?\s*#EXTM3U`)
)

// What a link in a list stands for; it decides what the link may be.
const (
	refPart = iota // a piece of the video, or the start of one (EXT-X-MAP)
	refList        // another list: a quality, an audio track
	refKey         // the key the parts are locked with
)

type listWriter struct {
	base  *url.URL
	write func(pattern, text string) (string, error)
	files []string
}

// writeList checks the list of a request and writes it for yt-dlp. The
// first file is the one to hand to yt-dlp; all of them belong to the job
// and are deleted with its cookies. base is the page of the player: links
// without a host are read against it.
func writeList(text, base string, write func(pattern, text string) (string, error)) ([]string, error) {
	if len(text) > maxListBytes {
		return nil, fmt.Errorf("list has %d bytes, at most %d", len(text), maxListBytes)
	}
	if !listHead.MatchString(text) {
		return nil, errors.New("not an HLS list")
	}
	b, err := url.Parse(base)
	if err != nil || (b.Scheme != "http" && b.Scheme != "https") || b.Host == "" {
		return nil, errors.New("the list has no page it belongs to")
	}
	w := &listWriter{base: b, write: write}
	out, err := w.list(text, 0)
	if err == nil {
		var main string
		if main, err = w.add("l-*.m3u8", out); err == nil {
			// The list to start with goes first.
			return append([]string{main}, w.files[:len(w.files)-1]...), nil
		}
	}
	return w.files, err // the caller removes what was written
}

func (w *listWriter) add(pattern, text string) (string, error) {
	if len(w.files) >= maxListFiles {
		return "", fmt.Errorf("list names more than %d lists and keys", maxListFiles)
	}
	name, err := w.write(pattern, text)
	if err != nil {
		return "", err
	}
	w.files = append(w.files, name)
	return name, nil
}

// list rewrites one list: every link in it becomes a web link or a file of
// this job. depth 0 is the list from the page, 1 a list written into it.
func (w *listWriter) list(text string, depth int) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	master := strings.Contains(text, "#EXT-X-STREAM-INF")
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			kind := refList
			switch {
			case strings.HasPrefix(line, "#EXT-X-KEY"), strings.HasPrefix(line, "#EXT-X-SESSION-KEY"):
				kind = refKey
			case strings.HasPrefix(line, "#EXT-X-MAP"), strings.HasPrefix(line, "#EXT-X-PART"), strings.HasPrefix(line, "#EXT-X-PRELOAD-HINT"):
				kind = refPart
			}
			var err error
			lines[i] = listURI.ReplaceAllStringFunc(line, func(m string) string {
				ref, rerr := w.ref(listURI.FindStringSubmatch(m)[1], kind, depth)
				if rerr != nil {
					err = rerr
					return m
				}
				return `URI="` + ref + `"`
			})
			if err != nil {
				return "", err
			}
		default:
			kind := refPart
			if master {
				kind = refList
			}
			ref, err := w.ref(line, kind, depth)
			if err != nil {
				return "", err
			}
			lines[i] = ref
		}
	}
	return strings.Join(lines, "\n"), nil
}

// ref turns one link of a list into what yt-dlp gets instead.
func (w *listWriter) ref(ref string, kind, depth int) (string, error) {
	low := strings.ToLower(ref)
	switch {
	case strings.HasPrefix(low, "data:"):
		data, err := dataOf(ref)
		if err != nil {
			return "", err
		}
		switch {
		case kind == refList && depth == 0 && listHead.Match(data):
			sub, err := w.list(string(data), 1)
			if err != nil {
				return "", err
			}
			name, err := w.add("l-*.m3u8", sub)
			return fileURL(name), err
		case kind == refKey && len(data) > 0 && len(data) <= maxKeyBytes:
			name, err := w.add("k-*.key", string(data))
			return fileURL(name), err
		}
		return "", errors.New("the list holds data that is neither a list nor a key")
	case strings.HasPrefix(low, "blob:") && kind == refKey:
		return "", errListKey
	}
	u, err := w.base.Parse(ref)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		if kind == refKey {
			return "", errListKey
		}
		return "", errors.New("the list names something that is no web link")
	}
	return u.String(), nil
}

// dataOf reads what a "data:" link holds.
func dataOf(link string) ([]byte, error) {
	meta, body, ok := strings.Cut(link[len("data:"):], ",")
	if !ok {
		return nil, errors.New("a data link without data")
	}
	if !strings.Contains(strings.ToLower(meta), ";base64") {
		s, err := url.PathUnescape(body)
		if err != nil {
			return nil, fmt.Errorf("read a data link: %w", err)
		}
		return []byte(s), nil
	}
	body = strings.TrimRight(strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, body), "=")
	data, err := base64.RawStdEncoding.DecodeString(body)
	if err != nil {
		if data, err = base64.RawURLEncoding.DecodeString(body); err != nil {
			return nil, fmt.Errorf("read a data link: %w", err)
		}
	}
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), nil
}

// fileURL is the link of a file on this PC, as yt-dlp reads it.
func fileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // "C:/Users/..." becomes "/C:/Users/..."
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}
