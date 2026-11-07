# How links reach VideoCatcher from outside

VideoCatcher is built to be the one download manager for links from
everywhere: its own window, the command line, web pages and, later, a browser
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

## 3. Request files (for the browser extension)

The browser extension (planned) talks to a small helper through the
browser's [native messaging](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging).
The browser starts the helper itself, and only for the extension named in
the helper's manifest; there is no open network port that a web page could
reach. The helper:

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
  "source": "extension",
  "cookies": "# Netscape HTTP Cookie File\n..."
}
```

| Field | Meaning |
|---|---|
| `v` | Format version. Anything but 1 is refused. |
| `url` | The link for yt-dlp: a page or a media stream (m3u8, mpd, mp4). http and https only. |
| `pageUrl`, `title`, `referer` | Optional. The title is shown in the list, the referer is sent with the download (some sites need it for streams). |
| `source` | `extension` starts the download like a link from the window. Anything else waits for the user's OK. |
| `cookies` | Optional. The cookies of the site in the Netscape cookie file format, for videos you only see when logged in. |

**Cookies** are logins. VideoCatcher writes them into a file of its own for
exactly one download (`%LOCALAPPDATA%\VideoCatcher\cookies\`, a folder only
your user can read), passes the file to yt-dlp and deletes it when the
download ends, however it ends. They never go into the log, the download list
or a command line. Leftover files from a crash are deleted after a day.
