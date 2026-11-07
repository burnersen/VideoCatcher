// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Tidy file names: letters and digits stay, everything else becomes a dot,
// never two dots in a row, none at the ends. The same rule as the names in
// NVENCForge (normalizeName): "Holiday 2026 (Part #2) - Sea.mp4" becomes
// "Holiday.2026.Part.Nr2.Sea.mp4". Letters of every script count as
// letters, so umlauts, Japanese or Cyrillic stay as they are.
//
// yt-dlp writes the file under its own name first; the file and the files
// that belong to it (subtitles, cover picture) are renamed after the
// download. The preview shows the tidy name before.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	reHashDigit = regexp.MustCompile(`#(\d)`)
	reManyDots  = regexp.MustCompile(`\.{2,}`)
)

// maxNameTries: "name.2" up to "name.99" when the tidy name is taken.
const maxNameTries = 99

// tidyName cleans a file name without its extension. "" means nothing
// usable is left (or Windows would refuse the name); the caller keeps the
// name it had.
func tidyName(s string) string {
	s = reHashDigit.ReplaceAllString(s, "Nr$1") // "#2" is a number, not a symbol
	s = strings.ReplaceAll(s, "#", "")
	s = strings.Map(func(r rune) rune {
		// Marks belong to their letter (Devanagari vowel signs, a combining
		// umlaut); as dots they would tear words apart.
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			return r
		}
		return '.'
	}, s)
	s = strings.Trim(reManyDots.ReplaceAllString(s, "."), ".")
	if len(s) > maxFileNameLen {
		cut := maxFileNameLen
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.Trim(s[:cut], ".")
	}
	if s == "" || isWindowsDeviceName(s) {
		return ""
	}
	return s
}

// tidyPath is the path with a tidy file name; the folder and the extension
// stay. Used for the preview.
func tidyPath(p string) string {
	dir, base, ext := splitName(p)
	n := tidyName(base)
	if n == "" {
		return p
	}
	return dir + n + ext
}

// splitName cuts "D:\V\Holiday [abc].mp4" into "D:\V\", "Holiday [abc]" and
// ".mp4". Both kinds of slash count: yt-dlp prints Windows paths, and the
// tests run on Linux.
func splitName(p string) (dir, base, ext string) {
	i := strings.LastIndexAny(p, `/\`)
	dir, name := p[:i+1], p[i+1:]
	if j := strings.LastIndexByte(name, '.'); j > 0 {
		return dir, name[:j], name[j:]
	}
	return dir, name, ""
}

// companionExts are the files yt-dlp writes next to a video that carry its
// name: subtitles, the cover picture, the description. They are renamed
// with it so they still belong together.
var companionExts = []string{".srt", ".vtt", ".ass", ".ssa", ".lrc", ".ttml", ".srv1", ".srv2", ".srv3", ".json3",
	".jpg", ".jpeg", ".png", ".webp", ".description", ".info.json"}

// tidyFile renames a downloaded file to its tidy name and its companions
// with it. It returns the new path; on any problem the old one with the
// error (the download itself is fine, only the name stays).
func tidyFile(p string) (string, error) {
	dir, base, ext := splitName(p)
	n := tidyName(base)
	if n == "" || n == base {
		return p, nil
	}
	target := ""
	for i := 1; i <= maxNameTries && target == ""; i++ {
		cand := n
		if i > 1 {
			cand = fmt.Sprintf("%s.%d", n, i)
		}
		if _, err := os.Lstat(dir + cand + ext); errors.Is(err, fs.ErrNotExist) {
			target = cand
		}
	}
	if target == "" {
		return p, fmt.Errorf("names %s up to .%d are taken", n, maxNameTries)
	}
	if err := os.Rename(p, dir+target+ext); err != nil {
		return p, err
	}
	// Companions: "Holiday [abc].de.srt" -> "Holiday.abc.de.srt".
	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		return dir + target + ext, nil // the video has its name; companions keep theirs
	}
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), base+".")
		if !ok || e.IsDir() || !isCompanion(rest) {
			continue
		}
		to := dir + target + "." + rest
		if _, err := os.Lstat(to); errors.Is(err, fs.ErrNotExist) {
			os.Rename(dir+e.Name(), to) // ignore: a companion keeping its old name is no harm
		}
	}
	return dir + target + ext, nil
}

func isCompanion(rest string) bool {
	low := strings.ToLower("." + rest)
	for _, e := range companionExts {
		if strings.HasSuffix(low, e) {
			return true
		}
	}
	return false
}
