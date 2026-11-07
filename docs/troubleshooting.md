# What the messages mean

The messages Video-Catcher shows when yt-dlp could not download something,
and what to do. "What yt-dlp said" below a failed download shows yt-dlp's
own words; please include them in a bug report.

| Message | What to do |
|---|---|
| The cookies of the chosen browser could not be read | Close that browser completely and try again. Chrome and Edge often block this on Windows; Firefox works best. |
| The site only shows this video to logged-in users | Log in to the site in your browser, then choose it at "Logins from browser" (More options) and try again. |
| The video has an age check | Log in to the site in your browser, then choose it at "Logins from browser" (More options) and try again. |
| The video is protected against copying (DRM) | yt-dlp cannot download protected videos. |
| The video is not available in your country | The site decides this by the location of your internet connection. |
| The video has not started yet | Try again when it is live or has been published. |
| yt-dlp does not know this site or this kind of link | Check the link. If the page plays a video, try the link of the video page itself. |
| The chosen quality or format is not available for this video | Choose "best" or another quality and try again. |
| FFmpeg is missing for this download | Click "Check for updates" so VideoCatcher can install it, then try again. |
| The site is limiting downloads right now | Wait a few minutes and try again, or download fewer videos at the same time. |
| The site refused the download | Click "Check for updates"; a new yt-dlp often fixes this. Cookies from your browser can help too. |
| The video is not available | It may have been deleted or made private. Check the link in your browser. |
| The disk is full | Free some space or choose another download folder. |
| VideoCatcher may not write into the download folder | Choose another folder, or check that no other program has the file open. |
| The site could not be reached | Check the internet connection and try again. |
| yt-dlp could not download this | The message shows what yt-dlp said. Click "Check for updates" and try again; if it stays, open an issue with the log. |

## Programs

| Message | What to do |
|---|---|
| yt-dlp \<version\> was not installed: its signature is not valid | Nothing was changed, the installed yt-dlp keeps working. If this stays for days, please report it: the release does not carry the signature of the yt-dlp key. |
| Cannot find out which yt-dlp is the newest | GitHub could not be reached. The installed yt-dlp keeps working; try again later. |
| The download of \<program\> does not match its checksum | Nothing was installed. The file was damaged on the way or changed on the server. Try again; if it stays, please report it. |
| Downloads are running right now | yt-dlp is only replaced when no download runs. Check for updates when they are done. |
| The download list was made by a newer version of VideoCatcher | Update VideoCatcher. The list is not changed by the older version. |
