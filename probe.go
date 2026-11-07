// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Asks yt-dlp what is behind a link before anything is downloaded: title,
// length, formats, subtitles, or the list of a playlist. Also the preview
// of the file name, which yt-dlp computes from the same information
// without going online again (--load-info-json).

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MediaInfo is what the window shows about a link.
type MediaInfo struct {
	URL        string        `json:"url"`
	Title      string        `json:"title"`
	Uploader   string        `json:"uploader"`
	Site       string        `json:"site"`
	Duration   float64       `json:"duration"` // seconds, 0 if unknown
	Thumbnail  string        `json:"thumbnail"`
	Live       bool          `json:"live"`
	IsPlaylist bool          `json:"isPlaylist"`
	Count      int           `json:"count"`   // playlist entries
	Entries    []EntryInfo   `json:"entries"` // the first maxEntriesShown of a playlist
	Formats    []FormatInfo  `json:"formats"`
	Heights    []int         `json:"heights"` // distinct video heights, largest first
	Subtitles  []string      `json:"subtitles"`
	AutoSubs   []string      `json:"autoSubs"`
	Warnings   []string      `json:"warnings"`
	ProbeID    string        `json:"probeId"` // to ask for the file name preview
	Raw        []byte        `json:"-"`       // yt-dlp's JSON, for the preview
	Options    probeSettings `json:"-"`
}

// probeSettings are the options a probe ran with; the download of the same
// link must use the same cookies and playlist choice to see the same thing.
type probeSettings struct {
	Playlist string
	Cookies  string
}

type EntryInfo struct {
	Index    int     `json:"index"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
}

type FormatInfo struct {
	ID       string  `json:"id"`
	Ext      string  `json:"ext"`
	Height   int     `json:"height"`
	FPS      float64 `json:"fps"`
	VCodec   string  `json:"vcodec"`
	ACodec   string  `json:"acodec"`
	Bitrate  float64 `json:"bitrate"` // kbit/s
	Size     int64   `json:"size"`    // bytes, 0 if unknown
	Approx   bool    `json:"approx"`  // Size is an estimate
	Note     string  `json:"note"`
	HasVideo bool    `json:"hasVideo"`
	HasAudio bool    `json:"hasAudio"`
}

// maxEntriesShown keeps the window fast with huge playlists; the count is
// still the real one.
const maxEntriesShown = 500

type rawInfo struct {
	Type          string         `json:"_type"`
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Uploader      string         `json:"uploader"`
	Channel       string         `json:"channel"`
	Extractor     string         `json:"extractor_key"`
	Duration      float64        `json:"duration"`
	Thumbnail     string         `json:"thumbnail"`
	WebpageURL    string         `json:"webpage_url"`
	LiveStatus    string         `json:"live_status"`
	IsLive        bool           `json:"is_live"`
	Formats       []rawFormat    `json:"formats"`
	Subtitles     map[string]any `json:"subtitles"`
	AutoCaptions  map[string]any `json:"automatic_captions"`
	Entries       []rawEntry     `json:"entries"`
	PlaylistCount int            `json:"playlist_count"`
}

type rawFormat struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	Height         float64 `json:"height"`
	FPS            float64 `json:"fps"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	TBR            float64 `json:"tbr"`
	Filesize       float64 `json:"filesize"`
	FilesizeApprox float64 `json:"filesize_approx"`
	FormatNote     string  `json:"format_note"`
	Protocol       string  `json:"protocol"`
}

type rawEntry struct {
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
}

// probeArgs is the argument list for "what is behind this link".
func probeArgs(link string, ps probeSettings, ts toolSet, have func(string) bool, cookieFile string) []string {
	a := []string{"--ignore-config", "--color", "never", "-J", "--flat-playlist"}
	if ps.Playlist == "all" {
		a = append(a, "--yes-playlist")
	} else {
		a = append(a, "--no-playlist")
	}
	if have("deno") {
		a = append(a, "--js-runtimes", "deno:"+ts.Deno)
	}
	switch {
	case cookieFile != "":
		a = append(a, "--cookies", cookieFile)
	case ps.Cookies != "" && ps.Cookies != "none":
		a = append(a, "--cookies-from-browser", ps.Cookies)
	}
	return append(a, "--", link)
}

// probe runs yt-dlp -J and reads its answer.
func probe(ctx context.Context, r runner, ytdlp string, args []string) (MediaInfo, error) {
	var out []byte
	var errs, warns []string
	err := r.Run(ctx, ytdlp, args, func(isErr bool, line string) {
		if !isErr && strings.HasPrefix(line, "{") {
			out = []byte(line)
			return
		}
		switch l := parseLine(line); l.Kind {
		case lineError:
			errs = append(errs, l.Text)
		case lineWarning:
			warns = append(warns, l.Text)
		}
	})
	if ctx.Err() != nil {
		return MediaInfo{}, ctx.Err()
	}
	if err != nil || len(out) == 0 {
		return MediaInfo{}, explainYtDlp(errs, err)
	}
	info, perr := parseInfo(out)
	if perr != nil {
		return MediaInfo{}, userError(perr, "yt-dlp gave an answer VideoCatcher does not understand",
			"Click \"Check for updates\" and try again.")
	}
	info.Warnings = warns
	return info, nil
}

func parseInfo(data []byte) (MediaInfo, error) {
	var raw rawInfo
	if err := json.Unmarshal(data, &raw); err != nil {
		return MediaInfo{}, fmt.Errorf("read yt-dlp JSON: %w", err)
	}
	m := MediaInfo{
		URL: raw.WebpageURL, Title: raw.Title, Uploader: firstOf(raw.Uploader, raw.Channel),
		Site: raw.Extractor, Duration: raw.Duration, Thumbnail: raw.Thumbnail,
		Live: raw.IsLive || raw.LiveStatus == "is_live", Raw: data,
		Formats: []FormatInfo{}, Heights: []int{}, Entries: []EntryInfo{},
		Subtitles: sortedKeys(raw.Subtitles, false), AutoSubs: sortedKeys(raw.AutoCaptions, true),
	}
	if raw.Type == "playlist" || raw.Entries != nil {
		m.IsPlaylist = true
		m.Count = raw.PlaylistCount
		if m.Count == 0 {
			m.Count = len(raw.Entries)
		}
		for i, e := range raw.Entries {
			if i >= maxEntriesShown {
				break
			}
			m.Entries = append(m.Entries, EntryInfo{Index: i + 1, Title: e.Title, Duration: e.Duration})
		}
		return m, nil
	}
	seen := map[int]bool{}
	for _, f := range raw.Formats {
		hasV := f.VCodec != "" && f.VCodec != "none"
		hasA := f.ACodec != "" && f.ACodec != "none"
		if f.Ext == "mhtml" || (!hasV && !hasA && f.Height == 0) {
			continue // storyboards: preview pictures, no video
		}
		size, approx := int64(f.Filesize), false
		if size == 0 && f.FilesizeApprox > 0 {
			size, approx = int64(f.FilesizeApprox), true
		}
		h := int(f.Height)
		// Sites without codec fields (many direct MP4 links) still have
		// picture and sound; a height says there is a picture.
		if f.VCodec == "" && f.ACodec == "" {
			hasV, hasA = h > 0, true
		}
		m.Formats = append(m.Formats, FormatInfo{ID: f.FormatID, Ext: f.Ext, Height: h, FPS: f.FPS,
			VCodec: codecName(f.VCodec), ACodec: codecName(f.ACodec), Bitrate: f.TBR, Size: size,
			Approx: approx, Note: f.FormatNote, HasVideo: hasV, HasAudio: hasA})
		if hasV && h > 0 && !seen[h] {
			seen[h] = true
			m.Heights = append(m.Heights, h)
		}
	}
	sort.SliceStable(m.Formats, func(i, j int) bool {
		a, b := m.Formats[i], m.Formats[j]
		if a.HasVideo != b.HasVideo {
			return a.HasVideo
		}
		if a.Height != b.Height {
			return a.Height > b.Height
		}
		return a.Bitrate > b.Bitrate
	})
	sort.Sort(sort.Reverse(sort.IntSlice(m.Heights)))
	return m, nil
}

// codecName shortens "avc1.640028" to "avc1" and "none" to "".
func codecName(c string) string {
	if c == "none" {
		return ""
	}
	name, _, _ := strings.Cut(c, ".")
	return name
}

func firstOf(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// sortedKeys lists the subtitle languages. Automatic captions exist for
// about 150 languages on YouTube; only the original ones ("-orig") and the
// common ones are of interest, the rest stay reachable through "all".
func sortedKeys(m map[string]any, auto bool) []string {
	out := []string{}
	for k := range m {
		if k == "live_chat" {
			continue // a chat log, not subtitles
		}
		if auto && !strings.HasSuffix(k, "-orig") && !commonLanguage[k] {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var commonLanguage = map[string]bool{
	"de": true, "en": true, "es": true, "fr": true, "it": true, "nl": true, "pl": true, "pt": true,
	"ru": true, "tr": true, "uk": true, "ja": true, "ko": true, "zh-Hans": true, "ar": true,
}

// previewArgs asks yt-dlp for the file name a download would get.
func previewArgs(infoFile string, o DownloadOptions) []string {
	return []string{"--ignore-config", "--color", "never", "--load-info-json", infoFile,
		"-P", o.OutputDir, "-o", o.outputTemplate(), "--print", "filename"}
}

// finalExtension corrects the preview: yt-dlp names the file before a
// conversion, so audio conversion and a chosen container change the
// extension afterwards. The user sees the name the file will really have.
func finalExtension(name string, o DownloadOptions) string {
	ext := ""
	switch {
	case o.Quality == "audio" && o.FormatID == "" && o.AudioFormat != "best":
		ext = o.AudioFormat
	case o.Quality != "audio" && o.Container != "auto":
		ext = o.Container
	}
	if ext == "" {
		return name
	}
	if i := strings.LastIndexAny(name, `./\`); i >= 0 && name[i] == '.' {
		return name[:i+1] + ext
	}
	return name + "." + ext
}
