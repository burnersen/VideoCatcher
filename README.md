<div align="center">

# Video-Catcher

### Catch videos from almost any site

**A modern window and download manager for [yt-dlp](https://github.com/yt-dlp/yt-dlp).** Paste a link, pick quality and subtitles, and watch the queue. One EXE for Windows, nothing to install: Video-Catcher fetches yt-dlp itself and keeps it up to date.

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

> **Status:** in development, no release yet. The window is built and tested by CI on every
> push; the first version follows once it has been tried on real PCs.

## ⚡ 30 seconds, no manual

1. Download `VideoCatcher.exe` from the [latest release](https://github.com/burnersen/VideoCatcher/releases/latest), a single file with nothing to install.
2. Start it. On the first start it fetches yt-dlp, FFmpeg and Deno (about 260 MB, once).
3. Paste a link, click **Look up**, then **Add to downloads**. The video lands in `Downloads\VideoCatcher`.

## ✨ What it does

- **Almost any site:** everything [yt-dlp supports](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md), from YouTube to small video portals, including videos you only see when logged in (with the logins of your browser).
- **See before you download:** title, length, every format with its size, subtitles in all languages, the entries of a playlist, and the exact file name it will get.
- **The settings that matter, in plain words:** quality (best, 4K to 360p, audio only), file format, audio format and quality, subtitles (in the video, as a file, or both), playlists (all or only some entries), file name patterns or a name of your own for one video, cover picture, chapters, YouTube sponsor segments, speed limit.
- **A real download manager:** a list with progress, speed and time left, several downloads at the same time, stop and try again, and the list survives a restart.
- **yt-dlp stays up to date by itself,** and safely: a new yt-dlp is only used if its release is signed by the yt-dlp release key built into Video-Catcher.
- **Errors in plain words:** "The site only shows this video to logged-in users" instead of a Python traceback, with what to do about it. What yt-dlp said is one click away.
- **Links from outside:** `VideoCatcher.exe --add <link>`, `videocatcher://` links and a bookmarklet, and a browser extension is planned ([how it works](docs/browser-bridge.md)).
- **English and German,** light or dark like Windows.

### What it never does

- It never overwrites a file that is already in your download folder, and never writes outside the folder you chose.
- It never leaves half downloads in your folder: unfinished parts live in Video-Catcher's own folder and are removed when a download ends, however it ends.
- A link from a web page never starts a download by itself. It waits in the list until you click **Download**.
- Your browser's logins (cookies) are never saved in the list, the log or a command line. A cookie file made for one download is deleted when that download ends.
- No telemetry, no account, no advertising.

## 🔧 Settings

Everything can be set in the window (**Settings**, and "Use the current download options as default"). The same settings live in a text file you can edit too: `%APPDATA%\VideoCatcher\VideoCatcher.ini`. Your own comments in that file stay when the window saves.

| Setting | Default | Meaning |
|---|---|---|
| `downloadFolder` | empty | Folder for downloads; empty means `Downloads\VideoCatcher` |
| `fileName` | `%(title)s [%(id)s].%(ext)s` | File name pattern of yt-dlp, below the download folder |
| `quality` | `best` | `best`, `2160`, `1440`, `1080`, `720`, `480`, `360`, `audio` |
| `container` | `auto` | `auto`, `mp4`, `mkv`, `webm` |
| `audioFormat`, `audioQuality` | `best` | For audio only: `mp3`, `m4a`, `opus`, `flac`, `wav`; 128 to 320 kbit/s |
| `subtitles`, `subtitleLanguages`, `autoSubtitles` | `none`, `de,en`, `false` | `embed`, `file` or `both`; languages separated by commas, or `all` |
| `playlist` | `single` | `single` (only the video) or `all` (the whole playlist) |
| `embedThumbnail`, `embedMetadata`, `embedChapters` | `false`, `true`, `true` | What goes into the file |
| `sponsorBlock` | `off` | YouTube sponsor segments: `mark` as chapters or `remove` |
| `speedLimit`, `fragments` | empty, `4` | e.g. `5M`; parts of a stream loaded at once (1 to 16) |
| `cookiesFromBrowser` | `none` | `firefox`, `chrome`, `edge`, `brave`, `opera`, `vivaldi`, `chromium` |
| `parallelDownloads` | `2` | 1 to 5 |
| `checkUpdatesAtStart` | `true` | Look for a new yt-dlp at every start |
| `language` | `auto` | `auto`, `en`, `de` |
| `watchClipboard` | `false` | Offer links as soon as they are copied |

An invalid value is reported in the activity log, and its default is used instead.

**Logins from the browser:** for videos you only see when logged in, log in to the site in your browser and choose that browser under **More options**. Firefox works best; Chrome and Edge often lock their cookies on Windows, then close the browser completely and try again.

## 📁 Where its files live

| What | Where |
|---|---|
| Settings | `%APPDATA%\VideoCatcher\VideoCatcher.ini` |
| yt-dlp, FFmpeg, Deno | `%LOCALAPPDATA%\VideoCatcher\bin\` |
| Download list | `%LOCALAPPDATA%\VideoCatcher\queue.json` |
| Unfinished parts | `%LOCALAPPDATA%\VideoCatcher\partial\` (removed when a download ends) |
| Log | `%LOCALAPPDATA%\VideoCatcher\logs\videocatcher.log` |

**Portable:** put an empty file `portable.txt` next to the exe, and everything stays in that folder. The log never grows past 2 MB; three older parts are kept.

## 🌐 What it does on the network

- The pages and media you ask it to download, through yt-dlp.
- GitHub, to find the newest yt-dlp and download it with its signed checksum list: at start (can be switched off) and when you click **Check for updates**.
- GitHub, to download FFmpeg and Deno once, when they are missing. Both are pinned to one version by size and SHA-256 ([runtime.json](runtime.json)).
- The preview pictures of the videos you look up, from the site itself.

Nothing else. Details in [CODE_SIGNING.md](CODE_SIGNING.md#privacy-policy).

## 💻 Requirements

Windows 10 or 11 (x64 or ARM64). The window uses the Microsoft Edge WebView2 runtime, which is part of current Windows.

<a id="smartscreen"></a>
**Windows SmartScreen** may warn because the files are not digitally signed yet. Choose "More info", then "Run anyway". Every release is built by GitHub Actions from this repository, never on a private PC, and `SHA256SUMS.txt` in the release lists the checksum of every file.

Only download what you are allowed to: videos you made, that are free, or that the site and the law let you keep. Video-Catcher cannot download videos protected by DRM.

## 🔧 Building it yourself

You need Go (the version in [`go.mod`](go.mod)) and, for the page checks, Node.js. There is no npm and no build step for the page: it is plain HTML, CSS and JavaScript in [`frontend/dist`](frontend/dist).

```
go test ./...
node frontend/app_check.js
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
wails build -platform windows/amd64 -skipbindings -trimpath
```

The window lands in `build/bin/VideoCatcher.exe`. A local build calls itself version `dev`; releases get their version from the tag.

## 📜 License

GPL-3.0-only, see [LICENSE](LICENSE). Free to use, at home and at work. yt-dlp (Unlicense), FFmpeg (GPL) and Deno (MIT) are separate programs from their own projects; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## 💬 Feedback

Found a bug or have an idea? [Open an issue](https://github.com/burnersen/VideoCatcher/issues). Please attach the log file (see above) and say what you did and what happened. [docs/troubleshooting.md](docs/troubleshooting.md) explains the messages of the window.
