// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Reads what yt-dlp prints while it works. yt-dlp is told (options.go) to
// print its progress in a fixed form of our own, one line per update, with
// a marker at the start, so nothing depends on yt-dlp's normal wording:
//
//	VCPROG downloading|1048576|10485760|NA|524288.5|18|NA|NA|137
//	VCPOST started|Merger
//	VCITEM 2|10|dQw4w9WgXcQ|Some title
//	VCFILE C:\Users\<benutzer>\Downloads\Some title [id].mp4
//
// A field yt-dlp does not know is printed as "NA". Lines without a marker
// are yt-dlp's own messages: "ERROR:" and "WARNING:" are kept for the user,
// the rest only goes to the log.

import (
	"strconv"
	"strings"
)

const (
	progressTemplate = "VCPROG %(progress.status)s|%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|%(progress.speed)s|%(progress.eta)s|%(progress.fragment_index)s|%(progress.fragment_count)s|%(info.format_id)s"
	postTemplate     = "VCPOST %(progress.status)s|%(progress.postprocessor)s"
	itemTemplate     = "VCITEM %(playlist_index)s|%(n_entries)s|%(id)s|%(title)s"
	fileTemplate     = "VCFILE %(filepath)s"
)

// Kinds of lines.
const (
	lineProgress = "progress"
	linePost     = "post"
	lineItem     = "item"
	lineFile     = "file"
	lineError    = "error"
	lineWarning  = "warning"
	lineOther    = "other"
)

// ytdlpLine is one line of yt-dlp's output, understood.
type ytdlpLine struct {
	Kind string

	// progress
	Status     string  // downloading, finished, error
	Done       int64   // bytes
	Total      int64   // bytes, 0 if unknown
	Estimated  bool    // Total is only an estimate
	Speed      float64 // bytes per second, 0 if unknown
	ETA        int64   // seconds, -1 if unknown
	Fragment   int64   // fragment number, 0 if not fragmented
	Fragments  int64
	FormatID   string
	PostStep   string // post: Merger, FFmpegExtractAudio, EmbedSubtitle ...
	Index      int64  // item: position in the playlist, 0 if none
	Count      int64  // item: number of entries, 0 if unknown
	VideoID    string
	Title      string
	Path       string // file
	Text       string // error, warning, other: the message without its prefix
	Unreadable bool   // a marker line that did not have the expected fields
}

func parseLine(line string) ytdlpLine {
	line = strings.TrimRight(line, "\r\n")
	marker, rest, _ := strings.Cut(line, " ")
	switch marker {
	case "VCPROG":
		f := strings.Split(rest, "|")
		if len(f) != 9 {
			return ytdlpLine{Kind: lineProgress, Unreadable: true, Text: line}
		}
		l := ytdlpLine{Kind: lineProgress, Status: f[0], Done: num(f[1]), Total: num(f[2]),
			Speed: flt(f[4]), ETA: -1, Fragment: num(f[6]), Fragments: num(f[7]), FormatID: na(f[8])}
		if l.Total == 0 {
			l.Total, l.Estimated = num(f[3]), num(f[3]) > 0
		}
		if f[5] != "NA" {
			l.ETA = num(f[5])
		}
		return l
	case "VCPOST":
		status, step, _ := strings.Cut(rest, "|")
		return ytdlpLine{Kind: linePost, Status: status, PostStep: na(step)}
	case "VCITEM":
		f := strings.SplitN(rest, "|", 4)
		if len(f) != 4 {
			return ytdlpLine{Kind: lineItem, Unreadable: true, Text: line}
		}
		return ytdlpLine{Kind: lineItem, Index: num(f[0]), Count: num(f[1]), VideoID: na(f[2]), Title: na(f[3])}
	case "VCFILE":
		return ytdlpLine{Kind: lineFile, Path: rest}
	}
	switch {
	case strings.HasPrefix(line, "ERROR: "):
		return ytdlpLine{Kind: lineError, Text: strings.TrimPrefix(line, "ERROR: ")}
	case strings.HasPrefix(line, "WARNING: "):
		return ytdlpLine{Kind: lineWarning, Text: strings.TrimPrefix(line, "WARNING: ")}
	}
	return ytdlpLine{Kind: lineOther, Text: line}
}

// num reads a whole number; yt-dlp prints some byte counts as floats.
func num(s string) int64 {
	if s == "NA" || s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return int64(flt(s))
}

func flt(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

func na(s string) string {
	if s == "NA" {
		return ""
	}
	return s
}

// postStepLabel says in plain words what a post processor does.
func postStepLabel(step string) string {
	switch step {
	case "Merger", "FFmpegMerger":
		return "joining picture and sound"
	case "ExtractAudio", "FFmpegExtractAudio":
		return "converting the audio"
	case "EmbedSubtitle", "FFmpegEmbedSubtitle":
		return "adding subtitles"
	case "EmbedThumbnail":
		return "adding the cover picture"
	case "Metadata", "FFmpegMetadata":
		return "adding title and chapters"
	case "VideoRemuxer", "FFmpegVideoRemuxer", "VideoConvertor", "FFmpegVideoConvertor":
		return "changing the file format"
	case "SubtitlesConvertor", "FFmpegSubtitlesConvertor":
		return "converting subtitles"
	case "SponsorBlock", "ModifyChapters":
		return "handling sponsor segments"
	case "MoveFiles", "MoveFilesAfterDownload":
		return "moving the file into place"
	}
	return "finishing"
}
