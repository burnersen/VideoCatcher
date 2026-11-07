// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Starts yt-dlp and reads what it prints, line by line. Always with an
// argument list and a context, never through a shell (08 S7). On Windows
// the process gets no console window and runs in a job object, so that
// stopping a download also stops everything yt-dlp started (its Python
// child and FFmpeg), and nothing keeps running if VideoCatcher ends.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// maxLineBytes is the longest line read. "yt-dlp -J" prints the whole
// description of a playlist as one line; 64 MiB is far more than the
// largest seen (a playlist of 5000 videos, about 20 MiB).
const maxLineBytes = 64 << 20

// runner starts a program; tests replace it with a fake.
type runner interface {
	Run(ctx context.Context, exe string, args []string, onLine func(stderr bool, line string)) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, exe string, args []string, onLine func(stderr bool, line string)) error {
	cmd := exec.CommandContext(ctx, exe, args...)
	// yt-dlp is a Python program: make it print UTF-8, whatever the code
	// page of the Windows console would be.
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8")
	cmd.WaitDelay = 5 * time.Second // pipes kept open by a grandchild must not hang Wait
	prepareCmd(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("start %s: %w", exe, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("start %s: %w", exe, err)
	}
	// Cancel runs on exec's own goroutine, possibly before afterStart is
	// done; the lock makes it wait for the job object.
	var killMu sync.Mutex
	var kill func() error
	cmd.Cancel = func() error {
		killMu.Lock()
		defer killMu.Unlock()
		if kill != nil {
			return kill()
		}
		return cmd.Process.Kill()
	}
	killMu.Lock()
	if err := cmd.Start(); err != nil {
		killMu.Unlock()
		return fmt.Errorf("start %s: %w", exe, err)
	}
	k, cleanup := afterStart(cmd)
	kill = k
	killMu.Unlock()
	defer cleanup()

	var mu sync.Mutex // onLine is called from two readers
	var wg sync.WaitGroup
	read := func(r io.Reader, isErr bool) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), maxLineBytes)
		for sc.Scan() {
			mu.Lock()
			onLine(isErr, sc.Text())
			mu.Unlock()
		}
	}
	wg.Add(2)
	go read(stdout, false)
	go read(stderr, true)
	wg.Wait()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s ended with exit code %d", exe, exitErr.ExitCode())
	}
	return err
}
