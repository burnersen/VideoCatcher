// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"os"
	"strings"
	"testing"
)

// docs/troubleshooting.md has every message with its exact wording (09 D8).
func TestTroubleshootingListsEveryMessage(t *testing.T) {
	doc, err := os.ReadFile("docs/troubleshooting.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range errorRules {
		if !strings.Contains(string(doc), "| "+r.msg+" |") {
			t.Errorf("docs/troubleshooting.md lacks %q", r.msg)
		}
	}
}
