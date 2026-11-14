# Code signing policy

> **Status:** `VideoCatcher.exe` is not code signed yet. Free code signing
> through the [SignPath Foundation](https://signpath.org) is planned as soon as
> VideoCatcher is known well enough to qualify. Everything for it is prepared, and
> this policy already describes how it will work. Every release says in its
> text whether its files are signed.
>
> Once signing is active: free code signing provided by
> [SignPath.io](https://about.signpath.io), certificate by
> [SignPath Foundation](https://signpath.org).

## What is signed

Only `VideoCatcher.exe`, built from the source code in this repository by the
[release workflow](.github/workflows/release.yml) on GitHub-hosted runners.
Nothing that was built elsewhere is signed with this certificate. Every
signing request is approved by hand, release by release.

VideoCatcher downloads other programs only when they are needed:
[yt-dlp](https://github.com/yt-dlp/yt-dlp), [FFmpeg](https://ffmpeg.org) and
[Deno](https://deno.com). They are never signed by this project; they stay
exactly as their projects published them. yt-dlp is accepted only if the
checksum list of its release carries a valid signature by the yt-dlp release
key, which is built into VideoCatcher; FFmpeg and Deno are pinned to one
version with size and SHA-256.

The file properties of the signed exe name the product `VideoCatcher` and the
version of the release; SignPath refuses to sign a file that does not match.

## Team roles

| Role | Who |
| ---- | --- |
| Committers and reviewers | [burnersen](https://github.com/burnersen) |
| Approvers | [burnersen](https://github.com/burnersen) |

Changes from anybody else (pull requests) are reviewed by a committer before
they are merged. All team members use multi-factor authentication for GitHub
and for SignPath.

## Privacy policy

This program will not transfer any information to other networked systems
unless specifically requested by the user or the person installing or
operating it.

What VideoCatcher does on the network, and only for that:

- The pages and media you ask it to download, through yt-dlp.
- GitHub, to find the newest yt-dlp release and download it with its signed
  checksum list: at start (can be switched off in the settings) and when you
  click "Check for updates".
- GitHub, to download FFmpeg and Deno once, when they are missing.

The browser extension makes no requests of its own. To find videos it sees
the requests of the pages, looks at the start of the answers a player gets
and at the videos a page names; of all that it keeps only the links. It
keeps the list of videos a page loaded in the browser's memory and sends a link (and, only if
you tick it, the site's cookies) to VideoCatcher on the same PC when you
click, through the browser's native messaging, never over the network.

There is no telemetry, no account and no advertising. VideoCatcher never
sends your links, your history or your cookies anywhere except to the site
the link points to.

## Changes to your files

Downloads are written as new files into the folder you choose. VideoCatcher
never overwrites an existing file there and never deletes or changes any of
your other files.

## Verifying a download

- **Checksums:** every release has `SHA256SUMS.txt`. In PowerShell:
  `Get-FileHash VideoCatcher.exe` and compare.
- **Signature** (once active): right-click `VideoCatcher.exe` > *Properties* >
  *Digital Signatures*. A signed release shows *SignPath Foundation*.
- **Build it yourself:** see [Building it yourself](README.md#-building-it-yourself).

## Reporting a problem

If you think a signed file of this project does something it should not,
please open an [issue](https://github.com/burnersen/VideoCatcher/issues).
Signed files that break these rules can also be reported to the
[SignPath Foundation](https://signpath.org).
