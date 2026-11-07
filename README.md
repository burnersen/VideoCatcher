<div align="center">

# VideoCatcher

### Catch videos from almost any site

**A modern window and download manager for [yt-dlp](https://github.com/yt-dlp/yt-dlp).** Paste a link, pick quality and subtitles, and watch the queue. One EXE for Windows, nothing to install: VideoCatcher fetches yt-dlp itself and keeps it up to date.

[![CI](https://github.com/burnersen/VideoCatcher/actions/workflows/ci.yml/badge.svg)](https://github.com/burnersen/VideoCatcher/actions/workflows/ci.yml)
[![Windows x64 | ARM64](https://img.shields.io/badge/Windows-10%2F11%20x64%20%7C%20ARM64-0078D6?logo=windows)](#requirements)
[![Built with Wails](https://img.shields.io/badge/Built%20with-Wails%20v2-DF0000)](#building-it-yourself)
[![Made with Go](https://img.shields.io/badge/Made%20with-Go-00ADD8?logo=go)](#building-it-yourself)
[![License: GPLv3](https://img.shields.io/badge/License-GPLv3-blue)](#license)
[![Ko-fi](https://img.shields.io/badge/Ko--fi-Support%20me-FF5E5B?logo=kofi&logoColor=white)](https://ko-fi.com/burnersen)

**[⬇️ Download the latest release](https://github.com/burnersen/VideoCatcher/releases/latest)** · **[🌐 Website](https://tech.burnersen.de/en/tools/videocatcher-en/)** · **[☕ Buy me a coffee](https://ko-fi.com/burnersen)**

<sub>🇩🇪 Auf Deutsch: [tech.burnersen.de/tools/videocatcher](https://tech.burnersen.de/tools/videocatcher/)</sub>

*Free for everyone, at home or at work. Open source under the [GPLv3](#license).*

</div>

> 💜 **Using VideoCatcher and happy with it?** A ⭐ on GitHub is the easiest way to let me know,
> and it does more than you'd think: stars are how small projects like this one qualify for
> free code signing, which makes the Windows warning go away for everyone. Or
> [tell me what you use it for](https://github.com/burnersen/VideoCatcher/issues/new), I read every message.
> Thanks for being here!

---

## ⚡ 30 seconds, no manual

1. Download `VideoCatcher.exe` from the [latest release](https://github.com/burnersen/VideoCatcher/releases/latest), a single file with nothing to install.
2. Start it and click **Choose a file**.
3. Done. The video lands in your download folder.

## ✨ What it does

- **First point:** what the user gets, not how it works.
- **Second point.**
- **Your files are safe:** what the tool never does (overwrite, delete, upload).

## 📁 Where its files live

| What | Where |
|---|---|
| Log | `%LOCALAPPDATA%\VideoCatcher\logs\videocatcher.log` |

**Portable:** put an empty file `portable.txt` next to the exe, and everything stays in that folder. The log never grows past 2 MB; three older parts are kept.

## 💻 Requirements

Windows 10 or 11 (x64 or ARM64). The window uses the Microsoft Edge WebView2 runtime, which is part of current Windows.

<a id="smartscreen"></a>
**Windows SmartScreen** may warn because the files are not digitally signed yet. Choose "More info", then "Run anyway". Every release is built by GitHub Actions from this repository, never on a private PC, and `SHA256SUMS.txt` in the release lists the checksum of every file.

## 🔧 Building it yourself

You need Go (the version in [`go.mod`](go.mod)) and, for the page checks, Node.js. There is no npm and no build step for the page: it is plain HTML, CSS and JavaScript in [`frontend/dist`](frontend/dist).

```
go test ./...
node frontend/app_check.js
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
wails build -platform windows/amd64 -skipbindings -trimpath
```

The window lands in `build/bin/VideoCatcher.exe`.

A local build calls itself version `dev`; releases get their version from the tag.

## 📜 License

GPL-3.0-only, see [LICENSE](LICENSE). Free to use, at home and at work.

## 💬 Feedback

Found a bug or have an idea? [Open an issue](https://github.com/burnersen/VideoCatcher/issues). Please attach the log file (see above) and say what you did and what happened.
