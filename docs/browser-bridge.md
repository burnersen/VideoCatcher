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
`videocatcher@burnersen.de`); there is no open network port that a web page
could reach.

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

### Messages

Each message is 4 bytes of length (little endian) and then JSON, both ways.

| From the extension | Reply |
|---|---|
| `{"type":"ping"}` | `{"ok":true,"version":"0.2.0"}` |
| `{"type":"add","url":...,"pageUrl":...,"title":...,"referer":...,"fileName":...,"cookies":...}` | `{"ok":true}` or `{"ok":false,"code":"bad-link"\|"start"\|"bad-message"\|"inbox","error":"..."}` |

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
  "cookies": "# Netscape HTTP Cookie File\n..."
}
```

| Field | Meaning |
|---|---|
| `v` | Format version. Anything but 1 is refused. |
| `url` | The link for yt-dlp: a page or a media stream (m3u8, mpd, mp4). http and https only. |
| `pageUrl`, `title`, `referer` | Optional. The title is shown in the list, the referer is sent with the download (some sites need it for streams). |
| `fileName` | Optional. A name for the file without ending; characters Windows does not allow become spaces. The extension sends the page title for streams, whose links have no name of their own. Not used for a whole playlist. |
| `source` | `extension` starts the download like a link from the window. Anything else waits for the user's OK. |
| `cookies` | Optional. The cookies of the site in the Netscape cookie file format, for videos you only see when logged in. |

**Cookies** are logins. VideoCatcher writes them into a file of its own for
exactly one download (`%LOCALAPPDATA%\VideoCatcher\cookies\`, a folder only
your user can read), passes the file to yt-dlp and deletes it when the
download ends, however it ends. They never go into the log, the download list
or a command line. Leftover files from a crash are deleted after a day.
