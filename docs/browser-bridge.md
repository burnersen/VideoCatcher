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
| `page.js` (inside the page, world `MAIN`) | the start of what `fetch` and `XMLHttpRequest` bring: an answer that begins like an HLS list, a DASH or a Smooth Streaming manifest is a stream, whatever its address and type say. It also knows whether an HLS list holds all qualities or one, and how long that one is |
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
  "headers": { "User-Agent": "Mozilla/5.0 ...", "Origin": "https://example.org", "Authorization": "Bearer ..." }
}
```

| Field | Meaning |
|---|---|
| `v` | Format version. Anything but 1 is refused. |
| `url` | The link for yt-dlp: a page or a media stream (m3u8, mpd, mp4). http and https only. |
| `pageUrl`, `title`, `referer` | Optional. The title is shown in the list, the referer is sent with the download (some sites need it for streams). When the site refuses `url` (see "Links with a token" below), `pageUrl` is tried instead. |
| `fileName` | Optional. A name for the file without ending; characters Windows does not allow become spaces. The extension sends the page title for streams, whose links have no name of their own. Not used for a whole playlist. |
| `source` | `extension` starts the download like a link from the window. Anything else waits for the user's OK. |
| `cookies` | Optional. The cookies of the site in the Netscape cookie file format, for videos you only see when logged in. |
| `headers` | Optional. Request headers the browser sent for the video, name to value (see below). Added in 0.6.0; older versions ignore the field. |

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

None of this gets around DRM, and none of it shows a video the login in the
browser cannot play.
