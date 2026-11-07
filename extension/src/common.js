// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// What background.js and popup.js share: which requests are videos, the
// cookie file format, and the way to Video-Catcher. Plain functions without
// browser state, so extension_check.js can test them without a browser.
// Everything stays inside this function: background.js and popup.js are
// loaded next to this file and share one global scope with it. A "const api"
// here and one there would stop the second script with a SyntaxError (found
// 2026-11-07 in Chrome: nothing was detected at all). Only globalThis.vc
// goes out.
(() => {
  "use strict";

  // The browser's API: Firefox has "browser" (promises), Chrome and Edge
  // "chrome" (promises too in Manifest V3).
  const api = globalThis.browser || globalThis.chrome;

  const nativeHost = "de.burnersen.videocatcher";
  const maxItemsPerTab = 40;

  // Streams and files by their link. Segments of a stream (.ts, .m4s) are not
  // a video of their own and are left out.
  const extKinds = [
    [/\.m3u8$/i, "hls"],
    [/\.mpd$/i, "dash"],
    [/\.(mp4|m4v|webm|mkv|mov|flv|avi|3gp)$/i, "video"],
    [/\.(mp3|m4a|ogg|opus|flac|wav)$/i, "audio"],
  ];
  const segmentPath = /\.(ts|m4s|aac|vtt|webvtt|srt|key)$/i;

  // Streams and files by their type, for links that say nothing.
  const typeKinds = [
    [/^(application\/(vnd\.apple\.mpegurl|x-mpegurl)|audio\/(x-)?mpegurl)\b/i, "hls"],
    [/^application\/dash\+xml\b/i, "dash"],
    [/^video\/(mp2t|iso\.segment)\b/i, null], // stream segments
    [/^video\//i, "video"],
    [/^audio\//i, "audio"],
  ];

  // Files smaller than this are mostly previews, ads or pieces of a stream.
  const minFileBytes = 1024 * 1024;

  // Request types that never carry a video. "media", "xmlhttprequest" and
  // "other" are where players load them; main_frame for a video opened
  // directly.
  const skipTypes = new Set(["image", "imageset", "stylesheet", "font", "script", "ping", "csp_report", "websocket", "beacon", "speculative", "web_manifest"]);

  function header(headers, name) {
    const n = name.toLowerCase();
    for (const h of headers || []) if (String(h.name).toLowerCase() === n) return h.value || "";
    return "";
  }

  // totalSize reads the size of the whole file, also from a range answer
  // ("Content-Range: bytes 0-1023/52428800").
  function totalSize(headers) {
    const range = /\/(\d+)\s*$/.exec(header(headers, "content-range"));
    if (range) return Number(range[1]);
    const len = Number(header(headers, "content-length"));
    return Number.isFinite(len) && len > 0 ? len : 0;
  }

  // classify says what a finished request is: "hls", "dash", "video",
  // "audio" or null for anything else.
  function classify(url, type, headers) {
    if (skipTypes.has(type)) return null;
    let u;
    try {
      u = new URL(url);
    } catch (e) {
      return null;
    }
    if (u.protocol !== "http:" && u.protocol !== "https:") return null;
    const path = u.pathname;
    if (segmentPath.test(path)) return null;
    const ctype = header(headers, "content-type").trim();
    let kind = null;
    for (const [re, k] of extKinds) {
      if (re.test(path)) {
        kind = k;
        break;
      }
    }
    if (!kind) {
      for (const [re, k] of typeKinds) {
        if (re.test(ctype)) {
          kind = k;
          break;
        }
      }
    }
    if (!kind) return null;
    // A file must be big enough to be a video; unknown size counts as big
    // (a stream of unknown length).
    if (kind === "video" || kind === "audio") {
      const size = totalSize(headers);
      if (size > 0 && size < minFileBytes) return null;
    }
    return kind;
  }

  // netscapeCookies writes cookies in the format yt-dlp reads (--cookies).
  function netscapeCookies(list) {
    const seen = new Set();
    const lines = ["# Netscape HTTP Cookie File", "# Written by Video-Catcher for one download."];
    for (const c of list || []) {
      const key = [c.domain, c.path, c.name].join("\u0000");
      if (seen.has(key) || /[\t\r\n]/.test(c.name + c.value + c.domain + c.path)) continue;
      seen.add(key);
      const domain = (c.httpOnly ? "#HttpOnly_" : "") + c.domain;
      const sub = c.domain.startsWith(".") ? "TRUE" : "FALSE";
      const expires = c.session || !c.expirationDate ? 0 : Math.floor(c.expirationDate);
      lines.push([domain, sub, c.path || "/", c.secure ? "TRUE" : "FALSE", expires, c.name, c.value].join("\t"));
    }
    return lines.join("\n") + "\n";
  }

  function hostOf(url) {
    try {
      return new URL(url).hostname.replace(/^www\./, "");
    } catch (e) {
      return "";
    }
  }

  // shortPath is the end of a link's path, for the list in the popup.
  function shortPath(url) {
    try {
      const parts = new URL(url).pathname.split("/").filter(Boolean);
      const last = parts.slice(-2).join("/");
      return last.length > 48 ? "..." + last.slice(-45) : last;
    } catch (e) {
      return "";
    }
  }

  function formatSize(bytes) {
    if (!bytes) return "";
    const units = ["KB", "MB", "GB"];
    let v = bytes / 1024;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024;
      i++;
    }
    return v.toFixed(v < 10 ? 1 : 0) + " " + units[i];
  }

  // addItem puts a found video into a tab's list: newest first, each link
  // once, at most maxItemsPerTab.
  function addItem(list, item) {
    const rest = (list || []).filter((x) => x.url !== item.url);
    return [item, ...rest].slice(0, maxItemsPerTab);
  }

  // --- talking to Video-Catcher ---

  // sendToApp sends one message to the helper (nativehost.go) and returns its
  // reply. A missing helper becomes { ok: false, code: "not-connected" }.
  async function sendToApp(message) {
    try {
      const reply = await api.runtime.sendNativeMessage(nativeHost, message);
      return reply || { ok: false, code: "bad-reply" };
    } catch (e) {
      return { ok: false, code: "not-connected", error: String((e && e.message) || e) };
    }
  }

  // cookiesFor collects the cookies for the given links (the video and the
  // page) in the Netscape format.
  async function cookiesFor(urls) {
    const all = [];
    for (const url of new Set(urls.filter((u) => /^https?:/i.test(u || "")))) {
      try {
        all.push(...(await api.cookies.getAll({ url })));
      } catch (e) {
        // A link the browser cannot read cookies for: the download goes
        // without them, which is what happens without the option too.
      }
    }
    return all.length > 0 ? netscapeCookies(all) : "";
  }

  // buildMessage makes the "add" message for a found video or for the page.
  // A video gets the page title as its file name, because a stream link has
  // no name of its own ("index-v1-a1.m3u8"); a page is named by yt-dlp.
  function buildMessage(tab, item) {
    if (!item) {
      return { type: "add", url: tab.url, pageUrl: tab.url, title: tab.title || "", referer: "", fileName: "" };
    }
    return {
      type: "add", url: item.url, pageUrl: tab.url, title: tab.title || "",
      referer: item.referer || item.frameUrl || tab.url, fileName: tab.title || "",
    };
  }

  globalThis.vc = {
    api, nativeHost, maxItemsPerTab, classify, totalSize, header, netscapeCookies, hostOf, shortPath,
    formatSize, addItem, sendToApp, cookiesFor, buildMessage,
  };
})();
