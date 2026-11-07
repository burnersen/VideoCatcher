// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// What the user chose in the window, and how that becomes the argument list
// for yt-dlp. Everything the window offers is checked here first: a value
// that is not on the list never reaches yt-dlp. The arguments are a list,
// never one string for a shell (08 S7).

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// DownloadOptions is one download's settings, as the window sends them.
type DownloadOptions struct {
	Quality        string `json:"quality"`        // best, 2160, 1440, 1080, 720, 480, 360, audio
	FormatID       string `json:"formatId"`       // a format picked from the list; wins over Quality
	Container      string `json:"container"`      // auto, mp4, mkv, webm
	AudioFormat    string `json:"audioFormat"`    // for audio only: best, mp3, m4a, opus, flac, wav
	AudioQuality   string `json:"audioQuality"`   // best, 320, 256, 192, 128 (kbit/s)
	SubLangs       string `json:"subLangs"`       // e.g. "de,en" or "all"; empty means none
	SubMode        string `json:"subMode"`        // none, embed, file, both
	AutoSubs       bool   `json:"autoSubs"`       // also automatic subtitles
	OutputDir      string `json:"outputDir"`      // absolute folder
	Template       string `json:"template"`       // yt-dlp output template, relative to OutputDir
	Playlist       string `json:"playlist"`       // single, all
	PlaylistItems  string `json:"playlistItems"`  // e.g. "1-5,8"; empty means all
	EmbedThumbnail bool   `json:"embedThumbnail"` // cover picture in the file
	EmbedMetadata  bool   `json:"embedMetadata"`  // title, artist, date in the file
	EmbedChapters  bool   `json:"embedChapters"`  // chapter marks in the file
	SponsorBlock   string `json:"sponsorBlock"`   // off, mark, remove (YouTube only)
	RateLimit      string `json:"rateLimit"`      // empty, or e.g. "5M" (bytes per second)
	Fragments      int    `json:"fragments"`      // parallel fragments for HLS/DASH, 1 to 16
	Cookies        string `json:"cookies"`        // none, or the browser to take cookies from
}

// Lists of what the window may send. The window builds its menus from the
// same lists (Choices), so they cannot drift apart.
var (
	qualityChoices      = []string{"best", "2160", "1440", "1080", "720", "480", "360", "audio"}
	containerChoices    = []string{"auto", "mp4", "mkv", "webm"}
	audioFormatChoices  = []string{"best", "mp3", "m4a", "opus", "flac", "wav"}
	audioQualityChoices = []string{"best", "320", "256", "192", "128"}
	subModeChoices      = []string{"none", "embed", "file", "both"}
	playlistChoices     = []string{"single", "all"}
	sponsorChoices      = []string{"off", "mark", "remove"}
	// The browsers yt-dlp can read cookies from on Windows (yt-dlp README,
	// --cookies-from-browser, checked 2026-11-07). Safari and Whale left out.
	cookieChoices = []string{"none", "firefox", "chrome", "edge", "brave", "opera", "vivaldi", "chromium"}
)

// Limits with their reason (05 K9).
const (
	maxFragments   = 16   // more brings nothing on a home line and annoys servers
	maxTemplateLen = 300  // a Windows path has 260 characters by default
	maxURLLen      = 4096 // longer is no link someone pasted
)

var (
	formatIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_.\-+/\[\]<=>*]{1,80}$`)
	subLangsPattern  = regexp.MustCompile(`^(all|[A-Za-z]{2,3}([_-][A-Za-z0-9]{2,8})*(\.\*)?)(,-?(all|live_chat|[A-Za-z]{2,3}([_-][A-Za-z0-9]{2,8})*(\.\*)?))*$`)
	itemsPattern     = regexp.MustCompile(`^[0-9]{1,5}(-[0-9]{0,5})?(,[0-9]{1,5}(-[0-9]{0,5})?)*$`)
	rateLimitPattern = regexp.MustCompile(`^[1-9][0-9]{0,4}(\.[0-9]{1,2})?[KMG]$`)
)

func defaultOptions() DownloadOptions {
	return DownloadOptions{
		Quality: "best", Container: "auto", AudioFormat: "best", AudioQuality: "best",
		SubMode: "none", SubLangs: "de,en", Template: "%(title)s [%(id)s].%(ext)s", Playlist: "single",
		EmbedMetadata: true, EmbedChapters: true, SponsorBlock: "off", Fragments: 4, Cookies: "none",
	}
}

func oneOf(field, v string, list []string) error {
	for _, x := range list {
		if v == x {
			return nil
		}
	}
	return fmt.Errorf("%s %q is not one of %s", field, v, strings.Join(list, ", "))
}

// validate checks every field. The error is a UserError naming the field.
func (o DownloadOptions) validate() error {
	bad := func(err error, what string) error {
		return userError(err, "The setting "+what+" is not valid", "Choose a value from the list.")
	}
	for _, c := range []struct {
		name, v string
		list    []string
	}{
		{"quality", o.Quality, qualityChoices}, {"container", o.Container, containerChoices},
		{"audio format", o.AudioFormat, audioFormatChoices}, {"audio quality", o.AudioQuality, audioQualityChoices},
		{"subtitles", o.SubMode, subModeChoices}, {"playlist", o.Playlist, playlistChoices},
		{"SponsorBlock", o.SponsorBlock, sponsorChoices}, {"cookies", o.Cookies, cookieChoices},
	} {
		if err := oneOf(c.name, c.v, c.list); err != nil {
			return bad(err, c.name)
		}
	}
	if o.FormatID != "" && !formatIDPattern.MatchString(o.FormatID) {
		return bad(fmt.Errorf("format %q", o.FormatID), "format")
	}
	if o.SubMode != "none" && !subLangsPattern.MatchString(o.SubLangs) {
		return userError(fmt.Errorf("subtitle languages %q", o.SubLangs),
			"The subtitle languages are not valid", `Use language codes separated by commas, for example "de,en", or "all".`)
	}
	if o.PlaylistItems != "" && !itemsPattern.MatchString(o.PlaylistItems) {
		return userError(fmt.Errorf("playlist items %q", o.PlaylistItems),
			"The playlist items are not valid", `Use numbers and ranges, for example "1-5,8".`)
	}
	if o.RateLimit != "" && !rateLimitPattern.MatchString(o.RateLimit) {
		return userError(fmt.Errorf("rate limit %q", o.RateLimit),
			"The speed limit is not valid", `Use a number with K, M or G, for example "5M" for 5 MB per second.`)
	}
	if o.Fragments < 1 || o.Fragments > maxFragments {
		return userError(fmt.Errorf("fragments %d", o.Fragments),
			"The number of parallel parts is not valid", fmt.Sprintf("Use 1 to %d.", maxFragments))
	}
	if !filepath.IsAbs(o.OutputDir) {
		return userError(fmt.Errorf("output dir %q", o.OutputDir),
			"No download folder is chosen", "Choose a folder for the downloads.")
	}
	return checkTemplate(o.Template)
}

// checkTemplate makes sure a file name template stays inside the download
// folder: no absolute path, no "..", no drive letter (README promise: files
// go only into the folder you choose).
func checkTemplate(t string) error {
	hint := `For example "%(title)s.%(ext)s" or "%(uploader)s/%(title)s.%(ext)s".`
	switch {
	case strings.TrimSpace(t) == "":
		return userError(errors.New("empty template"), "The file name is empty", hint)
	case len(t) > maxTemplateLen:
		return userError(fmt.Errorf("template has %d characters", len(t)), "The file name is too long", hint)
	case !strings.Contains(t, "%(ext)s"):
		return userError(fmt.Errorf("template %q has no %%(ext)s", t), `The file name must end with ".%(ext)s"`, hint)
	}
	norm := strings.ReplaceAll(t, `\`, "/")
	if strings.HasPrefix(norm, "/") || (len(norm) > 1 && norm[1] == ':') {
		return userError(fmt.Errorf("absolute template %q", t), "The file name must not be a full path",
			"The folder is chosen separately. "+hint)
	}
	for _, part := range strings.Split(norm, "/") {
		if strings.TrimSpace(part) == ".." {
			return userError(fmt.Errorf("template %q leaves the folder", t),
				`The file name must not contain ".."`, "Downloads stay inside the chosen folder. "+hint)
		}
	}
	return nil
}

// checkURL accepts what someone could paste or a browser could send: http
// and https links only. Everything else (file://, javascript:, a yt-dlp
// option disguised as a link) is refused.
func checkURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	hint := "Paste a link that starts with http:// or https://."
	if s == "" {
		return "", userError(errors.New("empty link"), "There is no link", hint)
	}
	if len(s) > maxURLLen {
		return "", userError(fmt.Errorf("link has %d characters", len(s)), "The link is too long", hint)
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", userError(fmt.Errorf("not a web link: %q", s), "This is not a web link", hint)
	}
	return u.String(), nil
}

// jobPaths are the places of one download.
type jobPaths struct {
	Temp    string // unfinished parts; removed when the job ends
	Cookies string // a cookie file for this job, or empty
}

// buildArgs turns the options into yt-dlp's argument list. The link comes
// last, after "--", so it can never be read as an option.
func buildArgs(o DownloadOptions, link string, ts toolSet, have func(tool string) bool, jp jobPaths, referer string) []string {
	a := []string{
		"--ignore-config", // a yt-dlp.conf somewhere on the PC must not change what the window shows
		"--color", "never",
		"--newline", "--progress", "--no-simulate",
		"--progress-template", "download:" + progressTemplate,
		"--progress-template", "postprocess:" + postTemplate,
		"--print", "before_dl:" + itemTemplate,
		"--print", "after_move:" + fileTemplate,
		"--no-overwrites", // README promise: an existing file is never overwritten
		"-P", o.OutputDir,
		"-P", "temp:" + jp.Temp,
		"-o", o.Template,
		"-N", strconv.Itoa(o.Fragments),
	}
	if runtime.GOOS == "windows" {
		a = append(a, "--windows-filenames")
	}
	if have("ffmpeg") {
		a = append(a, "--ffmpeg-location", ts.Dir)
	}
	if have("deno") {
		a = append(a, "--js-runtimes", "deno:"+ts.Deno)
	}

	audioOnly := o.Quality == "audio" && o.FormatID == ""
	switch {
	case o.FormatID != "":
		a = append(a, "-f", o.FormatID)
	case audioOnly:
		a = append(a, "-f", "ba/b", "-x")
		if o.AudioFormat != "best" {
			a = append(a, "--audio-format", o.AudioFormat)
		}
		if o.AudioQuality != "best" {
			a = append(a, "--audio-quality", o.AudioQuality+"K")
		} else {
			a = append(a, "--audio-quality", "0")
		}
	default:
		a = append(a, "-f", "bv*+ba/b")
		sort := []string{}
		if o.Quality != "best" {
			// Prefer this height or lower, but still take a larger one
			// rather than fail when nothing smaller exists.
			sort = append(sort, "res:"+o.Quality)
		}
		if o.Container == "mp4" {
			sort = append(sort, "ext:mp4:m4a") // H.264 and AAC: plays everywhere, no conversion
		}
		if len(sort) > 0 {
			a = append(a, "-S", strings.Join(sort, ","))
		}
	}
	if !audioOnly && o.Container != "auto" {
		a = append(a, "--merge-output-format", o.Container, "--remux-video", o.Container)
	}

	if o.SubMode != "none" {
		a = append(a, "--sub-langs", o.SubLangs)
		if o.AutoSubs {
			a = append(a, "--write-auto-subs")
		}
		switch o.SubMode {
		case "embed":
			a = append(a, "--embed-subs")
		case "file":
			a = append(a, "--write-subs", "--convert-subs", "srt")
		case "both":
			a = append(a, "--embed-subs", "--write-subs")
		}
	}
	if o.EmbedThumbnail {
		a = append(a, "--embed-thumbnail")
	}
	if o.EmbedMetadata {
		a = append(a, "--embed-metadata")
	}
	if o.EmbedChapters {
		a = append(a, "--embed-chapters")
	}
	switch o.SponsorBlock {
	case "mark":
		a = append(a, "--sponsorblock-mark", "all")
	case "remove":
		a = append(a, "--sponsorblock-remove", "sponsor,selfpromo,interaction")
	}
	if o.RateLimit != "" {
		a = append(a, "-r", o.RateLimit)
	}
	if o.Playlist == "all" {
		a = append(a, "--yes-playlist")
		if o.PlaylistItems != "" {
			a = append(a, "-I", o.PlaylistItems)
		}
	} else {
		a = append(a, "--no-playlist")
	}
	switch {
	case jp.Cookies != "":
		a = append(a, "--cookies", jp.Cookies)
	case o.Cookies != "none":
		a = append(a, "--cookies-from-browser", o.Cookies)
	}
	if referer != "" {
		a = append(a, "--add-headers", "Referer:"+referer)
	}
	return append(a, "--", link)
}
