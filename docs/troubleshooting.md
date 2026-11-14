# What the messages mean

The messages Video-Catcher shows when yt-dlp could not download something,
and what to do. "What yt-dlp said" below a failed download shows yt-dlp's
own words; **Copy details** puts them into the clipboard together with the
message, the link and the version, for a bug report.

| Message | What to do |
|---|---|
| The cookies of the chosen browser could not be read | Close that browser completely and try again. Chrome and Edge often block this on Windows; Firefox works best. |
| The site only shows this video to logged-in users | Log in to the site in your browser, then choose it at "Logins from browser" (More options) and try again. Or send the video with the browser extension and tick "Send the site's login (cookies) along". |
| The video has an age check | Log in to the site in your browser, then choose it at "Logins from browser" (More options) and try again. Or send the video with the browser extension and tick "Send the site's login (cookies) along". |
| The video is protected against copying (DRM) | yt-dlp cannot download protected videos. |
| The video is not available in your country | The site decides this by the location of your internet connection. |
| The video has not started yet | Try again when it is live or has been published. |
| yt-dlp does not know this site and found no video on this page | A page or a player of a site yt-dlp has no reader for cannot be downloaded by its address. In the browser extension, send a stream from its list (HLS, DASH) instead of the page or the player. If the list shows none, the site keeps its stream out of sight: click "Copy a report" in the extension's window and add it to an issue. |
| The chosen quality or format is not available for this video | Choose "best" or another quality and try again. |
| FFmpeg is missing for this download | Click "Check for updates" so VideoCatcher can install it, then try again. |
| The site is limiting downloads right now | Wait a few minutes and try again, or download fewer videos at the same time. |
| The site checks for a real browser and did not let Video-Catcher in | A bot wall (mostly Cloudflare) stands in front of the site. Open the page in your browser until it plays, then send the video again with the browser extension right away: it passes along the proof that you got in. If that fails too, tick "Send the site's login (cookies) along" there. Video-Catcher has tried once more answering like a browser before it says this; while it does, the download says "site wants a browser, trying as one". |
| The site wants a real browser, and this yt-dlp cannot answer like one | Click "Check for updates" to get the newest yt-dlp, then try again. The yt-dlp for Windows on ARM may come without the part for this. |
| The site refused the download | Click "Check for updates"; a new yt-dlp often fixes this. Cookies from your browser can help too. |
| The site no longer accepts the link of this video | Such links are only good for a short time, and often only for the player in the browser. Open the page again, start the video and send it once more; tick "Send the site's login (cookies) along" if you are logged in there. Video-Catcher has tried the page the video was found on before it says this; while it does, the download says "link refused, trying the page instead". |
| The video is not available | It may have been deleted or made private. Check the link in your browser. |
| The disk is full | Free some space or choose another download folder. |
| VideoCatcher may not write into the download folder | Choose another folder, or check that no other program has the file open. |
| The site could not be reached | Check the internet connection and try again. Video-Catcher has tried twice more by itself before it says this (after 30 seconds and after 2 minutes); while it does, the download says "site not reached, trying again soon". |
| Done, with "1 not downloaded" (a playlist) | The other videos are there. The message below and "What yt-dlp said" name the ones that are missing and why (private, deleted, blocked). "Try again" loads the playlist again; what is already in the folder is skipped. |
| The file is no longer there | **Play** could not find the file: it was moved, renamed or deleted after the download. |
| yt-dlp could not download this | The message shows what yt-dlp said. Click "Check for updates" and try again; if it stays, open an issue with the log. |

## Programs

| Message | What to do |
|---|---|
| yt-dlp \<version\> was not installed: its signature is not valid | Nothing was changed, the installed yt-dlp keeps working. If this stays for days, please report it: the release does not carry the signature of the yt-dlp key. |
| Cannot find out which yt-dlp is the newest | GitHub could not be reached. The installed yt-dlp keeps working; try again later. |
| The download of \<program\> does not match its checksum | Nothing was installed. The file was damaged on the way or changed on the server. Try again; if it stays, please report it. |
| Downloads are running right now | yt-dlp is only replaced when no download runs. Check for updates when they are done. |
| The download list was made by a newer version of VideoCatcher | Update VideoCatcher. The list is not changed by the older version. |
