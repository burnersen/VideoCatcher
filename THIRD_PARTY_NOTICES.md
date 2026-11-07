# Third-party software

## Libraries built into VideoCatcher.exe

The Go libraries in [`go.mod`](go.mod) and their licenses, read from the
LICENSE file of each project on 2026-11-07. Their license texts are in their
source code; the Go module cache (`go mod download`) has them all.

| Library | License |
|---|---|
| `git.sr.ht/~jackmordaunt/go-toast/v2` | not checked yet (source not reachable from the build session), see https://git.sr.ht/~jackmordaunt/go-toast |
| `github.com/bep/debounce` | MIT |
| `github.com/gabriel-vasile/mimetype` | MIT |
| `github.com/go-ole/go-ole` | MIT |
| `github.com/godbus/dbus/v5` | BSD-2-Clause |
| `github.com/google/uuid` | BSD-3-Clause |
| `github.com/gorilla/websocket` | BSD-2-Clause |
| `github.com/jchv/go-winloader` | ISC |
| `github.com/labstack/echo/v4` | MIT |
| `github.com/labstack/gommon` | MIT |
| `github.com/leaanthony/go-ansi-parser` | MIT |
| `github.com/leaanthony/gosod` | MIT |
| `github.com/leaanthony/slicer` | MIT |
| `github.com/leaanthony/u` | MIT |
| `github.com/mattn/go-colorable` | MIT |
| `github.com/mattn/go-isatty` | MIT |
| `github.com/pkg/browser` | BSD-2-Clause |
| `github.com/pkg/errors` | BSD-2-Clause |
| `github.com/rivo/uniseg` | MIT |
| `github.com/samber/lo` | MIT |
| `github.com/tkrajina/go-reflector` | Apache-2.0 |
| `github.com/valyala/bytebufferpool` | MIT |
| `github.com/valyala/fasttemplate` | MIT |
| `github.com/wailsapp/go-webview2` | MIT |
| `github.com/wailsapp/wails/v2` | MIT |
| `golang.org/x/crypto` | BSD-3-Clause |
| `golang.org/x/net` | BSD-3-Clause |
| `golang.org/x/sys` | BSD-3-Clause |
| `golang.org/x/text` | BSD-3-Clause |

## Programs VideoCatcher downloads

Not part of VideoCatcher.exe. They are downloaded from their own projects
(or an unchanged copy in this repository's release `runtime-v1`) when they
are needed, and run as separate programs.

| Program | License | Source |
|---|---|---|
| [yt-dlp](https://github.com/yt-dlp/yt-dlp), newest stable release | Unlicense | https://github.com/yt-dlp/yt-dlp |
| FFmpeg, build by [BtbN](https://github.com/BtbN/FFmpeg-Builds), version in [runtime.json](runtime.json) | GPL-3.0-or-later | the `source` and `build` links in [runtime.json](runtime.json) |
| [Deno](https://deno.com), version in [runtime.json](runtime.json) | MIT | https://github.com/denoland/deno |
