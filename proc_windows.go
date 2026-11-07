// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// prepareCmd hides the console window yt-dlp would otherwise open.
func prepareCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// afterStart puts the new process into a job object of its own that kills
// everything in it when it is closed. Only this child goes in, never
// VideoCatcher itself (08, Stolperfalle "Job-Objekt nur für Kindprozesse").
// yt-dlp.exe starts its Python child only after unpacking itself, which
// takes far longer than the few lines here, so the child lands in the job
// too. If the job cannot be made, the download still runs; stopping it then
// ends only yt-dlp.exe itself.
func afterStart(cmd *exec.Cmd) (kill func() error, cleanup func()) {
	fallback := func() (func() error, func()) { return cmd.Process.Kill, func() {} }
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fallback()
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job) // ignore: cleanup, the fallback is used
		return fallback()
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job) // ignore: cleanup, the fallback is used
		return fallback()
	}
	defer windows.CloseHandle(proc) // ignore: the job keeps its own reference
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(job) // ignore: cleanup, the fallback is used
		return fallback()
	}
	return func() error { return windows.TerminateJobObject(job, 1) },
		func() { windows.CloseHandle(job) } // ignore: closing ends what is left, which is the point
}
