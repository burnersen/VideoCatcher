// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Names and version of the program, in one place for every file.

// version is set by the release build with -ldflags "-X main.version=1.2.3".
// Local builds say "dev". Never edit it by hand: the tag is the only source.
var version = "dev"

const (
	appName     = "VideoCatcher"  // file and folder names
	appID       = "videocatcher"  // lower case: log file name
	displayName = "Video-Catcher" // window title and texts for users
)
