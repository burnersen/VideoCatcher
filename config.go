// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// config holds the settings the user may change, in the window or in the
// INI file. The window saves them through saveConfig, which changes only
// the values and keeps every comment and line of the user.
type config struct {
	Defaults       DownloadOptions // what a new download starts with
	DownloadDir    string          // empty: the default folder (defaultDownloadDir)
	Parallel       int
	CheckUpdates   bool
	Language       string // auto, en, de
	WatchClipboard bool
	CloseAction    string // closeChoices
	SpeedPercent   int    // the speed slider, 1 to 100
	MeasuredSpeed  int64  // bytes per second: 100 % of the slider (speed.go); 0 = not measured yet
}

// closeChoices: what the close button of the window does. quit ends
// VideoCatcher (running downloads pause and continue at the next start);
// tray-if-running keeps it in the notification area while downloads run;
// tray always does.
var closeChoices = []string{"quit", "tray-if-running", "tray"}

// setting is one key of the INI file. This table is the only place a key is
// described: defaults, the comments in a new file and the checks all come
// from here, so they cannot drift apart.
type setting struct {
	key     string
	def     string
	comment string // shown above the key in the file; one line per entry
	apply   func(c *config, value string) error
	get     func(c config) string // the value to save
}

// maxParallel: more parallel downloads than this get many sites to block
// the connection (HTTP 429), and a home line is full long before.
const maxParallel = 5

func choice(list []string, set func(c *config, v string)) func(c *config, v string) error {
	return func(c *config, v string) error {
		if err := oneOf("value", v, list); err != nil {
			return errors.New("use one of " + strings.Join(list, ", "))
		}
		set(c, v)
		return nil
	}
}

func boolSetting(set func(c *config, v bool)) func(c *config, v string) error {
	return func(c *config, v string) error {
		b, err := parseBool(v)
		if err == nil {
			set(c, b)
		}
		return err
	}
}

func onOff(b bool) string { return strconv.FormatBool(b) }

var settings = []setting{
	{key: "downloadFolder", def: "", comment: "Folder for downloads. Empty: Downloads\\VideoCatcher in your user folder.",
		apply: func(c *config, v string) error {
			if v != "" && !filepath.IsAbs(v) {
				return errors.New("use a full path like D:\\Videos, or leave it empty")
			}
			c.DownloadDir = v
			return nil
		}, get: func(c config) string { return c.DownloadDir }},
	{key: "fileName", def: "%(title)s [%(id)s].%(ext)s", comment: "File name template of yt-dlp, below the download folder. Must end with .%(ext)s.",
		apply: func(c *config, v string) error {
			if err := checkTemplate(v); err != nil {
				return errors.New(errorText(err))
			}
			c.Defaults.Template = v
			return nil
		}, get: func(c config) string { return c.Defaults.Template }},
	{key: "quality", def: "best", comment: "Video quality: " + strings.Join(qualityChoices, ", ") + ".",
		apply: choice(qualityChoices, func(c *config, v string) { c.Defaults.Quality = v }), get: func(c config) string { return c.Defaults.Quality }},
	{key: "container", def: "auto", comment: "File format of videos: " + strings.Join(containerChoices, ", ") + ".",
		apply: choice(containerChoices, func(c *config, v string) { c.Defaults.Container = v }), get: func(c config) string { return c.Defaults.Container }},
	{key: "audioFormat", def: "best", comment: "Format for audio only: " + strings.Join(audioFormatChoices, ", ") + ".",
		apply: choice(audioFormatChoices, func(c *config, v string) { c.Defaults.AudioFormat = v }), get: func(c config) string { return c.Defaults.AudioFormat }},
	{key: "audioQuality", def: "best", comment: "Audio quality in kbit/s: " + strings.Join(audioQualityChoices, ", ") + ".",
		apply: choice(audioQualityChoices, func(c *config, v string) { c.Defaults.AudioQuality = v }), get: func(c config) string { return c.Defaults.AudioQuality }},
	{key: "subtitles", def: "none", comment: "Subtitles: none, embed (into the video), file (separate .srt), both.",
		apply: choice(subModeChoices, func(c *config, v string) { c.Defaults.SubMode = v }), get: func(c config) string { return c.Defaults.SubMode }},
	{key: "subtitleLanguages", def: "de,en", comment: "Subtitle languages, separated by commas (e.g. de,en), or all.",
		apply: func(c *config, v string) error {
			if !subLangsPattern.MatchString(v) {
				return errors.New("use language codes separated by commas, or all")
			}
			c.Defaults.SubLangs = v
			return nil
		}, get: func(c config) string { return c.Defaults.SubLangs }},
	{key: "autoSubtitles", def: "false", comment: "Also take automatic subtitles (true or false).",
		apply: boolSetting(func(c *config, v bool) { c.Defaults.AutoSubs = v }), get: func(c config) string { return onOff(c.Defaults.AutoSubs) }},
	{key: "playlist", def: "single", comment: "A link to a video in a playlist: single (only the video) or all (the whole playlist).",
		apply: choice(playlistChoices, func(c *config, v string) { c.Defaults.Playlist = v }), get: func(c config) string { return c.Defaults.Playlist }},
	{key: "embedThumbnail", def: "false", comment: "Put the cover picture into the file (true or false).",
		apply: boolSetting(func(c *config, v bool) { c.Defaults.EmbedThumbnail = v }), get: func(c config) string { return onOff(c.Defaults.EmbedThumbnail) }},
	{key: "embedMetadata", def: "true", comment: "Put title, artist and date into the file (true or false).",
		apply: boolSetting(func(c *config, v bool) { c.Defaults.EmbedMetadata = v }), get: func(c config) string { return onOff(c.Defaults.EmbedMetadata) }},
	{key: "embedChapters", def: "true", comment: "Put chapter marks into the file (true or false).",
		apply: boolSetting(func(c *config, v bool) { c.Defaults.EmbedChapters = v }), get: func(c config) string { return onOff(c.Defaults.EmbedChapters) }},
	{key: "sponsorBlock", def: "off", comment: "YouTube sponsor segments: off, mark (as chapters), remove.",
		apply: choice(sponsorChoices, func(c *config, v string) { c.Defaults.SponsorBlock = v }), get: func(c config) string { return c.Defaults.SponsorBlock }},
	{key: "speedLimit", def: "", comment: "Speed limit per download, e.g. 5M for 5 MB/s. Empty: no limit.",
		apply: func(c *config, v string) error {
			if v != "" && !rateLimitPattern.MatchString(v) {
				return errors.New("use a number with K, M or G, e.g. 5M")
			}
			c.Defaults.RateLimit = v
			return nil
		}, get: func(c config) string { return c.Defaults.RateLimit }},
	{key: "fragments", def: "4", comment: "Parts of a stream loaded at the same time (1 to 16).",
		apply: func(c *config, v string) (err error) {
			c.Defaults.Fragments, err = parseIntRange(v, 1, maxFragments)
			return err
		}, get: func(c config) string { return strconv.Itoa(c.Defaults.Fragments) }},
	{key: "cookiesFromBrowser", def: "none", comment: "Use the logins of a browser: " + strings.Join(cookieChoices, ", ") + ".",
		apply: choice(cookieChoices, func(c *config, v string) { c.Defaults.Cookies = v }), get: func(c config) string { return c.Defaults.Cookies }},
	{key: "parallelDownloads", def: "2", comment: "Downloads running at the same time (1 to 5).",
		apply: func(c *config, v string) (err error) {
			c.Parallel, err = parseIntRange(v, 1, maxParallel)
			return err
		}, get: func(c config) string { return strconv.Itoa(c.Parallel) }},
	{key: "checkUpdatesAtStart", def: "true", comment: "Look for a new yt-dlp at every start (true or false).",
		apply: boolSetting(func(c *config, v bool) { c.CheckUpdates = v }), get: func(c config) string { return onOff(c.CheckUpdates) }},
	{key: "language", def: "auto", comment: "Language of the window: auto (like Windows), en, de.",
		apply: choice([]string{"auto", "en", "de"}, func(c *config, v string) { c.Language = v }), get: func(c config) string { return c.Language }},
	{key: "watchClipboard", def: "false", comment: "Offer a link as soon as one is copied (true or false).",
		apply: boolSetting(func(c *config, v bool) { c.WatchClipboard = v }), get: func(c config) string { return onOff(c.WatchClipboard) }},
	{key: "tidyFileNames", def: "true", comment: "Rename downloaded files: letters and digits stay, everything else becomes one dot (true or false).",
		apply: boolSetting(func(c *config, v bool) { c.Defaults.CleanNames = v }), get: func(c config) string { return onOff(c.Defaults.CleanNames) }},
	{key: "closeButton", def: "tray-if-running", comment: "The close button of the window: quit (downloads pause and go on at the next start), tray-if-running (keep running in the notification area while downloads run), tray (always keep running there).",
		apply: choice(closeChoices, func(c *config, v string) { c.CloseAction = v }), get: func(c config) string { return c.CloseAction }},
	{key: "speedPercent", def: "100", comment: "The speed slider: share of the fastest speed measured, 1 to 100. 100: no limit.",
		apply: func(c *config, v string) (err error) {
			c.SpeedPercent, err = parseIntRange(v, minSpeedPercent, maxSpeedPercent)
			return err
		}, get: func(c config) string { return strconv.Itoa(c.SpeedPercent) }},
	{key: "measuredSpeed", def: "0", comment: "Fastest download speed measured without a limit, in bytes per second; 100 % of the speed slider. Measured by " + appName + " itself; 0: not yet. Set it to 0 to measure again.",
		apply: func(c *config, v string) error {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 || n > maxMeasuredSpeed {
				return fmt.Errorf("use a whole number from 0 to %d", maxMeasuredSpeed)
			}
			c.MeasuredSpeed = n
			return nil
		}, get: func(c config) string { return strconv.FormatInt(c.MeasuredSpeed, 10) }},
}

// maxMeasuredSpeed: 100 GB/s, far beyond any line; a bigger number is a typo.
const maxMeasuredSpeed = 100 << 30

// configLoad is what loadConfig found out. Created, Added and Warnings are
// reported to the user by the caller (main.go, reportConfig).
type configLoad struct {
	Config   config
	Created  bool     // the file did not exist and was written with defaults
	Added    []string // keys that were missing and were appended (old file kept as .bak)
	Warnings []string // unknown keys and invalid values; defaults were used instead
}

// loadConfig reads the INI file.
//
//   - Missing file: written with all defaults and comments.
//   - Invalid value: a warning names line, key and the default used instead.
//     The program runs on; the user's line stays as it is.
//   - Unknown key: a warning, nothing else.
//   - Missing keys (a newer version added some): appended with their
//     comments, after a copy of the old file was saved as .bak.
//
// The user's own lines are never changed or removed.
func loadConfig(path string) (configLoad, error) {
	var res configLoad
	res.Config = defaultConfig()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := writeFileAtomic(path, []byte(configTemplate(settings)), 0o644); err != nil {
			// Defaults still work; the user just cannot change them yet.
			res.Warnings = append(res.Warnings, fmt.Sprintf("Cannot create the settings file %s (%v). Using the defaults.", path, err))
			return res, nil
		}
		res.Created = true
		return res, nil
	}
	if err != nil {
		return res, fileError(err, "read the settings file", path)
	}

	text := strings.TrimPrefix(string(data), "\ufeff") // Windows editors like to add a BOM
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		s, known := findSetting(key)
		switch {
		case !ok:
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s line %d: %q is not key=value, ignored.", path, i+1, line))
		case !known:
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s line %d: unknown setting %q, ignored.", path, i+1, key))
		default:
			seen[s.key] = true
			if err := s.apply(&res.Config, value); err != nil {
				_ = s.apply(&res.Config, s.def) // ignore: defaults are checked by TestDefaultsAreValid
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s line %d: %s=%s is invalid (%v). Using %s instead.", path, i+1, s.key, value, err, s.def))
			}
		}
	}

	var missing []setting
	for _, s := range settings {
		if !seen[s.key] {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		if err := appendMissing(path, data, missing); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Cannot add new settings to %s (%v). Their defaults are used.", path, err))
		} else {
			for _, s := range missing {
				res.Added = append(res.Added, s.key)
			}
		}
	}
	return res, nil
}

func defaultConfig() config {
	c := config{Defaults: defaultOptions()}
	for _, s := range settings {
		_ = s.apply(&c, s.def) // ignore: defaults are checked by TestDefaultsAreValid
	}
	return c
}

func findSetting(key string) (setting, bool) {
	for _, s := range settings {
		if strings.EqualFold(s.key, key) {
			return s, true
		}
	}
	return setting{}, false
}

// configTemplate is a complete new settings file.
func configTemplate(list []setting) string {
	return "# " + appName + " settings\n" +
		"# Lines starting with # are comments. Delete a line to get its default back.\n" +
		configEntries(list)
}

// configEntries is one block per key: a blank line, the comment, key=default.
func configEntries(list []setting) string {
	var b strings.Builder
	for _, s := range list {
		fmt.Fprintf(&b, "\n# %s\n%s=%s\n", s.comment, s.key, s.def)
	}
	return b.String()
}

// appendMissing keeps a copy of the old file, then appends the missing keys
// with the same line endings the file already uses.
func appendMissing(path string, old []byte, missing []setting) error {
	if err := writeFileAtomic(path+".bak", old, 0o644); err != nil {
		return fmt.Errorf("save backup: %w", err)
	}
	add := "\n# Added by " + appName + " " + version + ":\n" + configEntries(missing)
	if strings.Contains(string(old), "\r\n") {
		add = strings.ReplaceAll(add, "\n", "\r\n")
	}
	text := string(old)
	if !strings.HasSuffix(text, "\n") && text != "" {
		add = "\n" + add
	}
	return writeFileAtomic(path, []byte(text+add), 0o644)
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, errors.New("use true or false")
}

func parseIntRange(v string, minimum, maximum int) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, errors.New("not a whole number")
	}
	if n < minimum || n > maximum {
		return 0, fmt.Errorf("must be between %d and %d", minimum, maximum)
	}
	return n, nil
}

// saveConfig writes c into the INI file. Only values change: comments, the
// order and lines the program does not know stay as the user left them,
// and so do the line endings. The old file is kept as .bak. Keys the file
// does not have yet are appended with their comment.
func saveConfig(path string, c config) error {
	for _, s := range settings {
		v := s.get(c)
		if strings.ContainsAny(v, "\r\n") {
			return userError(fmt.Errorf("%s has a line break", s.key), "A setting contains a line break", "Remove it and save again.")
		}
		var probe config
		if err := s.apply(&probe, v); err != nil {
			return userError(err, "The setting "+s.key+" is not valid", err.Error())
		}
	}
	old, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		old = []byte(configTemplate(settings))
	} else if err != nil {
		return fileError(err, "read the settings file", path)
	} else if err := writeFileAtomic(path+".bak", old, 0o644); err != nil {
		return fileError(err, "save a copy of", path)
	}

	eol := "\n"
	if strings.Contains(string(old), "\r\n") {
		eol = "\r\n"
	}
	bom := strings.HasPrefix(string(old), "\ufeff")
	lines := strings.Split(strings.TrimPrefix(strings.ReplaceAll(string(old), "\r\n", "\n"), "\ufeff"), "\n")
	written := map[string]bool{}
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
			continue
		}
		key, _, ok := strings.Cut(t, "=")
		s, known := findSetting(strings.TrimSpace(key))
		if !ok || !known || written[s.key] {
			continue
		}
		lines[i] = s.key + "=" + s.get(c)
		written[s.key] = true
	}
	text := strings.Join(lines, "\n")
	var missing []setting
	for _, s := range settings {
		if !written[s.key] {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		for _, s := range missing {
			text += fmt.Sprintf("\n# %s\n%s=%s\n", s.comment, s.key, s.get(c))
		}
	}
	text = strings.ReplaceAll(text, "\n", eol)
	if bom {
		text = "\ufeff" + text
	}
	if err := writeFileAtomic(path, []byte(text), 0o644); err != nil {
		return fileError(err, "save the settings to", path)
	}
	return nil
}
