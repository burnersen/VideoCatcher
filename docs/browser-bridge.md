# How links reach VideoCatcher from outside

VideoCatcher is built to be the one download manager for links from
everywhere: its own window, the command line, web pages and the browser
extension. All ways end in the same place (`App.accept` in `app.go`) and use
the settings of the window for quality, folder and file name.

## 1. Command line

```
VideoCatcher.exe --add "https://www.youtube.com/watch?v=..."
```

If VideoCatcher is already running, the new start hands the link to the
running window and ends at once. If not, VideoCatcher starts and takes the
link. The download starts by itself.

## 2. `videocatcher://` links

After "Open videocatcher:// links from web pages" is turned on in the
settings, a link like this opens VideoCatcher:

```
videocatcher://add?url=<link, URL-encoded>&title=<title>&referer=<page>
```

A bookmarklet that sends the page you are on:

```
javascript:location.href='videocatcher://add?url='+encodeURIComponent(location.href)+'&title='+encodeURIComponent(document.title)
```

**Links from web pages never start by themselves.** Any web page can create
such a link, so the job waits in the list with "A web page sent this link.
Download it?" until you click "Download". The registration lives only in
your user's part of the registry (`HKEY_CURRENT_USER\Software\Classes\videocatcher`)
and needs no administrator rights; turning the setting off removes it.

## 3. The browser extension

The extension (`extension/`, for Firefox, Chrome and Edge) talks to a helper
through the browser's [native messaging](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging).
The helper is `VideoCatcher.exe` itself, started by the browser without a
window (`nativehost.go`). The browser starts it only for the extension named
in the helper's manifest (Chrome and Edge: ID `lmloindgceipinokjbodjgcbfmihmlhd`,
fixed by the `key` in the extension's manifest; Firefox:
`videocatcher@burnersen.de`); the extension needs no network port. (The only
port Video-Catcher opens is the relay for the speed slider, on 127.0.0.1 and
for yt-dlp only: it answers nothing without its password, which is new at
every start. README, "What it does on the network".)

"Allow the browser extension" in the settings writes the two manifests to
`%LOCALAPPDATA%\VideoCatcher\native\` and points these keys at them, all
below `HKEY_CURRENT_USER`:

```
Software\Google\Chrome\NativeMessagingHosts\de.burnersen.videocatcher
Software\Chromium\NativeMessagingHosts\de.burnersen.videocatcher
Software\Microsoft\Edge\NativeMessagingHosts\de.burnersen.videocatcher
Software\Mozilla\NativeMessagingHosts\de.burnersen.videocatcher
```

Turning it off removes the keys. A copy of `VideoCatcher.exe` in another
folder takes the keys over at its start.

### How the extension finds videos

| Where | What it sees |
|---|---|
| `background.js` (`webRequest`) | every request of a tab: streams and files by the end of the address, the type of the answer, a stream named behind the `?`, the file name of a download; with the Referer the request really had |
| `page.js` (inside the page, world `MAIN`) | the start of what `fetch` and `XMLHttpRequest` bring: an answer that begins like an HLS list, a DASH or a Smooth Streaming manifest is a stream, whatever its address and type say. It also knows whether an HLS list holds all qualities or one, and how long that one is. A list the player builds in the page's memory has no address; `page.js` passes on the list itself (see "A list built in memory" below) |
| `content.js` (next to the page) | the videos the page names (`<video>`, `<audio>`, `<source>`, also in open shadow trees), a video playing from memory (`blob:`), and a player inside a frame, whose address is listed as a page to send |

`page.js` tells `content.js` only the link (an event on the document),
`content.js` tells `background.js`, which checks every field again
(`pageItems` in `common.js`): a page can make such events up, and all that
may come of it is a link in a list the user reads. One link found in several
ways is one entry; what read the answer counts more than the address.

### Messages

Each message is 4 bytes of length (little endian) and then JSON, both ways.

| From the extension | Reply |
|---|---|
| `{"type":"ping"}` | `{"ok":true,"version":"0.2.0"}` |
| `{"type":"add","url":...,"pageUrl":...,"title":...,"referer":...,"fileName":...,"cookies":...,"headers":{...}}` | `{"ok":true}` or `{"ok":false,"code":"bad-link"\|"start"\|"bad-message"\|"inbox","error":"..."}` |

`code` `start` means the window could not be started; the request stays in
the inbox and is taken at the next start.

### Request files

For an `add`, the helper:

1. writes the request as a file into `%LOCALAPPDATA%\VideoCatcher\inbox\`,
   named `<8 to 64 letters, digits or dashes>.json`,
2. starts `VideoCatcher.exe --request <that name>`.

VideoCatcher reads the file, removes it and takes the link. Only plain names
inside the inbox are accepted, never a path. Request files that were left
behind are picked up within a few seconds while VideoCatcher runs.

The file holds, in version 1:

```json
{
  "v": 1,
  "url": "https://example.org/watch/123",
  "pageUrl": "https://example.org/watch/123",
  "title": "Title of the page",
  "referer": "https://example.org/",
  "fileName": "Title of the page",
  "source": "extension",
  "cookies": "# Netscape HTTP Cookie File\n...",
  "headers": { "User-Agent": "Mozilla/5.0 ...", "Origin": "https://example.org", "Authorization": "Bearer ..." },
  "list": "#EXTM3U\n#EXT-X-STREAM-INF:...\ndata:application/vnd.apple.mpegurl;base64,...\n"
}
```

| Field | Meaning |
|---|---|
| `v` | Format version. Anything but 1 is refused. |
| `url` | The link for yt-dlp: a page or a media stream (m3u8, mpd, mp4). http and https only. |
| `pageUrl`, `title`, `referer` | Optional. The title is shown in the list, the referer is sent with the download (some sites need it for streams). When the site refuses `url` (see "Links with a token" below), `pageUrl` is tried instead. |
| `fileName` | Optional. A name for the file without ending; characters Windows does not allow become spaces. The extension sends the page title for streams, whose links have no name of their own. Not used for a whole playlist. |
| `source` | `extension` starts the download like a link from the window. Anything else waits for the user's OK. |
| `cookies` | Optional. The cookies of the site in the Netscape cookie file format, for videos you only see when logged in. Without that option, only the proof of a bot check (see "Sites behind a bot check" below). |
| `headers` | Optional. Request headers the browser sent for the video, name to value (see below). Added in 0.6.0; older versions ignore the field. |
| `list` | Optional. An HLS list the player built in the page's memory, as text; `url` is then the page of the player (see "A list built in memory" below). Added in 0.8.0; older versions ignore the field and try the page. |

**Cookies** are logins. VideoCatcher writes them into a file of its own for
exactly one download (`%LOCALAPPDATA%\VideoCatcher\cookies\`, a folder only
your user can read), passes the file to yt-dlp and deletes it when the
download ends, however it ends. They never go into the log, the download list
or a command line. Leftover files from a crash are deleted after a day.

The extension takes the cookies of the video's address, of the page and of
the frame the player sits in, from the cookie store of the tab (a private
window and a Firefox container have their own). It also asks for the cookies
the browser keeps apart for an embedded player (partitioned cookies): that
player is often the one that holds the login.

### Links with a token

Many sites hand the player a link that is only good for a short time, for
one login or for one browser: a token in the address, a login in a request
header, a check of where the request comes from. Three things help such a
download through:

1. **Request headers.** For every video it lists, the extension remembers
   the headers its request had in the browser, and sends them along:
   `User-Agent`, `Origin`, `Authorization` and the headers a player adds of
   its own (`X-...`). Headers yt-dlp sets itself for each request (`Host`,
   `Range`, `Accept` ...), `Cookie`, `Referer` and the browser's `Sec-...`
   headers are left out; the list is `skipHeaders` in `headers.go`, and the
   window checks every header again (name, printable ASCII, length, at most
   12). The `User-Agent` is kept with the job and passed to yt-dlp as an
   argument. All others are logins like cookies: they are written into a
   file next to the cookie file, in yt-dlp's configuration format, which
   yt-dlp reads with `--config-locations`; the file is deleted with the
   cookie file. yt-dlp sends these headers with every request of the
   download, also to another host that serves the parts of the stream.
2. **The browser's name.** With "Send the site's login along", the
   `User-Agent` goes with a page too: many sites accept a login only from
   the browser it was made in.
3. **The page instead.** When the site refuses the link of a video (HTTP
   401, 403 or 410, or it calls the link expired), the download tries
   `pageUrl` once, with the same cookies and `User-Agent` but without the
   referer and headers of the refused request. The list says "link refused,
   trying the page instead". If the page does not work either, the download
   fails with "The site no longer accepts the link of this video": send it
   again from the browser, and it starts with a fresh link.

### Sites behind a bot check

Many sites sit behind a wall (mostly Cloudflare) that looks at who is asking
and lets only browsers in. yt-dlp then gets "HTTP Error 403", whatever the
link. Three things help such a download through, in this order:

1. **The proof that you got in.** The wall hands a browser it let in a
   cookie that says so (`cf_clearance`, `__cf_bm`, `_cfuvid`, `__cflb`, and
   DDoS-Guard's `__ddg...`). The extension sends these cookies with every
   video and page, also without "Send the site's login along", together with
   the `User-Agent`: the proof is only good for the browser it was given to.
   They are no login; all other cookies stay in the browser unless the
   option is ticked. The list is `passCookie` in `common.js`.
2. **Answering like a browser.** When yt-dlp is turned away with a 403 or
   the wall is named in its error, the download runs once more with
   `--impersonate chrome` (or `firefox`, by the `User-Agent` of the browser
   the link came from): yt-dlp then answers the way that browser does. The
   list says "site wants a browser, trying as one". Same link, same cookies,
   same headers. This comes before "the page instead" above, and the page is
   then tried the same way.
3. **A clear word at the end.** If the wall stays shut, the download fails
   with "The site checks for a real browser and did not let Video-Catcher
   in", and "What yt-dlp said" keeps the lines of every try. A yt-dlp build
   without the part for this (possible on Windows on ARM) is told apart:
   "The site wants a real browser, and this yt-dlp cannot answer like one".

### A list built in memory

Some players never load a list of their stream: they build it in the page's
memory (the list of qualities as a `blob:`, each quality written into it as
a `data:` link) and load only the parts, often under names that say nothing
(`4800.html`). Such a stream has no link to send. `page.js` sees the list
when the player hands it to the browser and passes its text on; the popup
lists it as an HLS stream "built by the player", and it is sent as `list`.

The text comes out of a web page, so `memlist.go` trusts nothing in it. It
writes the list into files of the cookie folder, one per list, and what
comes out names only two kinds of links: web links (`http`, `https`), and
the files written for this one job (the qualities, and a key that was
written into the list itself). A list that names anything else, such as a
file of this PC, is refused as a whole. yt-dlp gets the first file with
`--enable-file-urls`; that option is on for such a job only, where every
`file:` link is one Video-Catcher wrote itself. The files are deleted with
the job's cookies, and their folder is taken out of what yt-dlp says before
it reaches the details. The parts are asked for the way the player asks:
with the player's page as `Referer` and `Origin`, as the same browser.

What this does not do: take a key out of a player. If the parts are locked
and the key is not a link a downloader can ask for (a `blob:` link), the
window says "The site keeps the key of this video in its player" and
nothing is downloaded. If the site hands the key only to its own script,
yt-dlp asks for it, is refused, and the download fails with the site's
answer.

### When a video is not found

"Copy a report" in the extension's window puts on the clipboard what the
extension saw on the page, for an issue: the videos it found, the last 300
requests of the tab (kind, status, type, size, top or frame) and a few notes
of `page.js` on how the player gets its video (a list it builds in memory, a
worker that does the asking, a video fed from memory). Links are shortened
by `brief` in `common.js`: host and path stay, every value behind the `?`
and every long name in the path becomes its length. No cookie, no header and
nothing of an answer is in it. The requests are kept in memory only, never
stored, and are gone when the tab closes or goes to another page.

`page.js` also watches the `fetch` and `XMLHttpRequest` of the frames a page
makes for itself: a player can ask for its video with the functions of an
empty frame to get around a page script like this one.

None of this gets around DRM, and none of it shows a video the login in the
browser cannot play.
