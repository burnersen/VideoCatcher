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

> **Status:** early versions (0.x), in use and growing. The window is built and tested by CI on
> every push.

## ⚡ 30 seconds, no manual

1. Download `VideoCatcher.exe` from the [latest release](https://github.com/burnersen/VideoCatcher/releases/latest), a single file with nothing to install.
2. Start it. On the first start it fetches yt-dlp, FFmpeg and Deno (about 260 MB, once).
3. Paste a link, click **Look up**, then **Add to downloads**. The video lands in `Downloads\VideoCatcher`.

## ✨ What it does

- **Almost any site:** everything [yt-dlp supports](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md), from YouTube to small video portals, including videos you only see when logged in (with the logins of your browser).
- **See before you download:** title, length, every format with its size, subtitles in all languages, the entries of a playlist, and the exact file name it will get.
- **The settings that matter, in plain words:** quality (best, 4K to 360p, audio only), file format, audio format and quality, subtitles (in the video, as a file, or both), playlists (all or only some entries), file name patterns or a name of your own for one video, cover picture, chapters, YouTube sponsor segments, speed limit.
- **A real download manager:** a list with progress, speed and time left, several downloads at the same time, stop and try again (one, or all failed ones together), put a waiting download in front with **Next**, search the list, and the list survives a restart. The window title, and with it the taskbar, shows how far the downloads are.
- **What you downloaded, one click away:** a finished download shows its size, how long it took and when it ended. **Play** opens it, **Show in folder** finds it, a playlist lists all its files, and a click on the site's name copies the link.
- **Tries again by itself:** when the connection drops, a download waits half a minute, then two minutes, and goes on with what it has, instead of failing at once. Trouble with the video itself (login, DRM, deleted) is not tried again.
- **Playlists that are not perfect:** when single videos of a playlist cannot be loaded (private, deleted, blocked), the playlist is still done, says how many are missing and why.
- **Stays awake:** while downloads run or wait, Windows does not go to sleep by itself. You can still send the PC to sleep.
- **Pause and continue:** pause a download (or all of them) and continue it later where it stopped, also after a restart. Closing Video-Catcher during a download pauses it; it goes on at the next start. Whether a half download can be continued depends on the site; if a link has expired, the download starts again from the beginning.
- **Speed slider:** 1 to 100 % of your line, for all downloads together, and it works at once, also on downloads that are running. 100 % is the fastest speed Video-Catcher measured while there was no limit ([how it works](#-what-it-does-on-the-network)).
- **Keeps running in the notification area:** with downloads running, the close button puts the window into the notification area and the downloads go on (setting **Close button of the window**). A click on the icon brings it back, its menu has **Quit**. While the window is away, the icon tells you when a download failed and when all are done.
- **When all are done:** quit Video-Catcher, put the PC to sleep, hibernate or shut it down, after a 60 second countdown you can cancel. It holds for this run only.
- **Tidy file names:** letters and digits stay, everything else becomes a dot, never two in a row: `Holiday 2026 (Part #2) - Sea.mp4` becomes `Holiday.2026.Part.Nr2.Sea.mp4`. Letters of every script stay. Subtitles and cover pictures next to the video are renamed with it. On by default, one tick to turn it off.
- **Drag and drop, or Ctrl+V:** drag a link from the browser onto the window, or press Ctrl+V anywhere in it. One link is looked up, several are added straight away; dropped `.txt` files with links and Internet shortcuts (`.url`) work too. A link that is already in the list is named before it is added a second time.
- **yt-dlp stays up to date by itself,** and safely: a new yt-dlp is only used if its release is signed by the yt-dlp release key built into Video-Catcher. When a newer Video-Catcher is out, the window says so at the bottom, with a link to the release; it installs nothing by itself.
- **Errors in plain words:** "The site only shows this video to logged-in users" instead of a Python traceback, with what to do about it. What yt-dlp said is one click away, and **Copy details** puts everything a bug report needs into the clipboard.
- **Straight from the browser:** the Video-Catcher extension for Firefox, Chrome and Edge sees which videos a page loads, also inside embedded players, and sends them with one click ([below](#-browser-extension)).
- **Links from outside:** `VideoCatcher.exe --add <link>`, `videocatcher://` links and a bookmarklet ([how it works](docs/browser-bridge.md)).
- **English and German,** light or dark like Windows.

### What it never does

- It never overwrites a file that is already in your download folder, and never writes outside the folder you chose.
- It never leaves half downloads in your folder: unfinished parts live in Video-Catcher's own folder. They are kept while a download is paused and removed when it ends, however it ends.
- A link from a web page never starts a download by itself. It waits in the list until you click **Download**.
- Your browser's logins (cookies) are never saved in the list, the log or a command line. A cookie file made for one download is deleted when that download ends.
- No telemetry, no account, no advertising.

## 🧩 Browser extension

Some pages show a video without a link yt-dlp can use: the player is built by the page's scripts, often inside an embedded frame from another site, and often only after you click a play button. The extension watches what the page really loads and lists the videos it saw (HLS, DASH and Smooth Streaming streams, video files) in its toolbar button.

It finds them in four ways, because sites hide their videos differently: by the address and type of what the page loads; by looking at the start of the answers the player gets, for streams behind an address that says nothing (`/get.php?id=5`); by the videos the page names itself, also before they are loaded; and by the embedded player's own address, which yt-dlp often knows where the page around it says nothing (listed as **PLAYER**). The best one is on top: a stream with all its qualities before a single quality (shown with its length, so the film is easy to tell from an advert), streams before files. When a video plays but the page keeps its stream out of sight, the button says so instead of "nothing found".

**How to use it:** open the page, start the video (if the page offers several players or hosts, click one), then click the Video-Catcher button in the toolbar and **Download** next to the video. **Send this page** sends the address of the page itself, which is best for the sites yt-dlp knows (YouTube and many more). There is also a right-click menu for pages, links and videos. A video from the extension gets the page title as its file name, and it is downloaded with the settings of the window.

**Install it once:**

1. In Video-Catcher, open **Settings** and turn on **Allow the browser extension**. This tells the browsers where Video-Catcher is (only for your Windows user, no administrator rights).
2. Download the extension from the [latest release](https://github.com/burnersen/VideoCatcher/releases/latest):
   - **Firefox:** `VideoCatcher-extension-firefox.xpi`, open it in Firefox (or drag it into the window) and confirm. If the release has only `VideoCatcher-extension-firefox.zip` (not signed by Mozilla yet), Firefox can load it until the next restart: `about:debugging` > This Firefox > Load Temporary Add-on > choose the zip.
   - **Chrome and Edge:** unpack `VideoCatcher-extension-chromium.zip` into a folder that stays (for example `Documents\VideoCatcher-extension`). Open `chrome://extensions` (Edge: `edge://extensions`), turn on **Developer mode**, click **Load unpacked** and choose that folder.
3. Click the puzzle piece in the toolbar and pin Video-Catcher. The button should say "Connected to Video-Catcher".

**What it may do, and why:** see the requests of the pages and look into the pages themselves (to find videos; it reads and changes nothing on them), keep a list per tab in the browser's memory until the tab or browser closes, add a right-click menu, read a site's cookies only when you tick "Send the site's login along", and talk to Video-Catcher on this PC. It sends nothing to the internet and nothing anywhere before you click. If you moved `VideoCatcher.exe`, start it once from its new place; it updates what the browsers know. After installing or updating the extension, reload the pages that were already open.

The extension cannot get around DRM, and it cannot download what a site keeps from you.

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
| `checkUpdatesAtStart` | `true` | Look for a new yt-dlp and a newer Video-Catcher at every start |
| `language` | `auto` | `auto`, `en`, `de` |
| `watchClipboard` | `false` | Offer links as soon as they are copied |
| `tidyFileNames` | `true` | Rename downloaded files: letters and digits stay, everything else becomes one dot |
| `closeButton` | `tray-if-running` | `quit` (running downloads pause and go on at the next start), `tray-if-running` (keep running in the notification area while downloads run), `tray` (always) |
| `speedPercent` | `100` | The speed slider, 1 to 100; 100 means no limit |
| `measuredSpeed` | `0` | 100 % of the slider in bytes per second, measured by Video-Catcher itself; set it to `0` to measure again |
| `window` | empty | Size of the window when it was last closed, e.g. `1240x820` or `1240x820,max`; written by Video-Catcher itself, empty means the default size |

An invalid value is reported in the activity log, and its default is used instead.

**Logins from the browser:** for videos you only see when logged in, log in to the site in your browser and choose that browser under **More options**. Firefox works best; Chrome and Edge often lock their cookies on Windows, then close the browser completely and try again.

## 📁 Where its files live

| What | Where |
|---|---|
| Settings | `%APPDATA%\VideoCatcher\VideoCatcher.ini` |
| yt-dlp, FFmpeg, Deno | `%LOCALAPPDATA%\VideoCatcher\bin\` |
| Download list | `%LOCALAPPDATA%\VideoCatcher\queue.json` |
| Unfinished parts | `%LOCALAPPDATA%\VideoCatcher\partial\` (kept while a download is paused, removed when it ends) |
| Log | `%LOCALAPPDATA%\VideoCatcher\logs\videocatcher.log` (the helper of the extension: `native-host.log` next to it) |
| What the browsers know about the extension's helper | `%LOCALAPPDATA%\VideoCatcher\native\` and `HKEY_CURRENT_USER\Software\...\NativeMessagingHosts\de.burnersen.videocatcher` (Chrome, Chromium, Edge, Mozilla) |

**Portable:** put an empty file `portable.txt` next to the exe, and everything stays in that folder. The log never grows past 2 MB; three older parts are kept.

## 🌐 What it does on the network

- The pages and media you ask it to download, through yt-dlp.
- GitHub, to find the newest yt-dlp and download it with its signed checksum list: at start (can be switched off) and when you click **Check for updates**.
- GitHub, at the same two moments, to see whether a newer Video-Catcher was released. It asks for the address of the newest release and sends nothing about you or your version. Nothing is downloaded; the link in the window opens the release page in your browser.
- GitHub, to download FFmpeg and Deno once, when they are missing. Both are pinned to one version by size and SHA-256 ([runtime.json](runtime.json)).
- The preview pictures of the videos you look up, from the site itself.

For the speed slider, yt-dlp sends its downloads through a small relay inside Video-Catcher, which passes on only as many bytes per second as the slider allows. The relay listens on `127.0.0.1` only (not reachable from other PCs), on a port Windows picks, with a password that is new at every start and that only yt-dlp gets; without it, it answers nothing. Encrypted (https) connections pass through it encrypted, and it adds nothing to the requests. A proxy set up in Windows itself is not used for downloads then: the relay connects to the sites directly.

The browser extension itself makes no requests of its own; it only talks to Video-Catcher on this PC. Nothing else. Details in [CODE_SIGNING.md](CODE_SIGNING.md#privacy-policy).

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
node extension/extension_check.js
node extension/build.js 1.2.3      # the extension for Chrome/Edge and Firefox in extension/dist
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
wails build -platform windows/amd64 -skipbindings -trimpath
```

The window lands in `build/bin/VideoCatcher.exe`. A local build calls itself version `dev`; releases get their version from the tag.

## 📜 License

GPL-3.0-only, see [LICENSE](LICENSE). Free to use, at home and at work. yt-dlp (Unlicense), FFmpeg (GPL) and Deno (MIT) are separate programs from their own projects; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## 💬 Feedback

Found a bug or have an idea? [Open an issue](https://github.com/burnersen/VideoCatcher/issues). Please attach the log file (see above) and say what you did and what happened. [docs/troubleshooting.md](docs/troubleshooting.md) explains the messages of the window.
