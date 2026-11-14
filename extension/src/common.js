// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// What background.js and popup.js share: which requests are videos, how the
// list of a tab is kept, the login that goes with a download (cookies and
// request headers), and the way to Video-Catcher. Plain functions without
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
    [/\.m3u8?$/i, "hls"],
    [/\.mpd$/i, "dash"],
    [/\.ism\/manifest$/i, "ism"],
    [/\.(mp4|m4v|webm|mkv|mov|flv|avi|3gp)$/i, "video"],
    [/\.(mp3|m4a|ogg|opus|flac|wav)$/i, "audio"],
  ];
  const segmentPath = /\.(ts|m4s|aac|vtt|webvtt|srt|key)$/i;

  // Streams and files by their type, for links that say nothing.
  const typeKinds = [
    [/^(application\/(vnd\.apple\.mpegurl|x-mpegurl|mpegurl)|audio\/(x-)?mpegurl)\b/i, "hls"],
    [/^(application\/dash\+xml|video\/vnd\.mpeg\.dash\.mpd)\b/i, "dash"],
    [/^application\/vnd\.ms-sstr\+xml\b/i, "ism"],
    [/^video\/(mp2t|iso\.segment)\b/i, null], // stream segments
    [/^video\//i, "video"],
    [/^audio\//i, "audio"],
  ];

  // A stream named only behind the "?" ("/get.php?file=master.m3u8"). Such
  // a link is also what a counter or an API call looks like, so an answer
  // of one of these types is not taken for a stream.
  const queryKinds = [
    [/\.m3u8(?=$|[&#?\/])/i, "hls"],
    [/\.mpd(?=$|[&#?\/])/i, "dash"],
  ];
  const notAStream = /json|html|javascript|css|^image\/|^font\//i;

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

  // Request headers that are not passed on with a download: yt-dlp sets
  // them itself for each request, they have their own way (Cookie, Referer),
  // or they describe the browser and not the login. The same list as in
  // headers.go, which checks again.
  const skipHeaders = new Set(["accept", "accept-encoding", "accept-language", "cache-control", "connection",
    "content-length", "content-type", "cookie", "dnt", "expect", "host", "keep-alive", "pragma", "priority", "range",
    "referer", "te", "trailer", "transfer-encoding", "upgrade", "upgrade-insecure-requests", "via", "x-client-data"]);
  const skipHeaderPrefix = /^(sec-|proxy-|if-)/;
  const maxHeaders = 12;
  const maxHeaderValue = 4096;

  // sentHeaders picks, from the headers of a request, what a site may want
  // to see again when Video-Catcher asks for the same video: the browser's
  // name (User-Agent), where the request came from (Origin), a login in a
  // header (Authorization) and the headers a player adds of its own
  // ("X-Token"). Sites that work with tokens check these.
  function sentHeaders(headers) {
    const out = {};
    let n = 0;
    for (const h of headers || []) {
      const name = String(h.name || "");
      const low = name.toLowerCase();
      const value = String(h.value || "").trim();
      if (!/^[A-Za-z0-9_-]{1,64}$/.test(name) || skipHeaders.has(low) || skipHeaderPrefix.test(low)) continue;
      if (!value || value.length > maxHeaderValue || !/^[\x20-\x7e]+$/.test(value)) continue;
      if (low !== "user-agent" && ++n > maxHeaders) continue;
      out[name] = value;
    }
    return out;
  }

  function kindOfPath(path) {
    for (const [re, k] of extKinds) if (re.test(path)) return k;
    return null;
  }

  // kindOfUrl says what a link looks like by itself, without an answer:
  // for the videos a page names in its markup.
  function kindOfUrl(url) {
    try {
      const u = new URL(url);
      if (u.protocol !== "http:" && u.protocol !== "https:") return null;
      return kindOfPath(u.pathname);
    } catch (e) {
      return null;
    }
  }

  // The file name a server gives for a download
  // (Content-Disposition: attachment; filename="movie.mp4").
  function dispositionName(headers) {
    const m = /filename\*?=(?:UTF-8'')?"?([^";]+)/i.exec(header(headers, "content-disposition"));
    return m ? m[1].trim() : "";
  }

  // classify says what a finished request is: "hls", "dash", "ism",
  // "video", "audio" or null for anything else. In this order: the end of
  // the path, the type of the answer, a stream named behind the "?", the
  // file name of a download.
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
    let kind = kindOfPath(path);
    if (!kind) {
      for (const [re, k] of typeKinds) {
        if (re.test(ctype)) {
          if (!k) return null; // a segment
          kind = k;
          break;
        }
      }
    }
    if (!kind && !notAStream.test(ctype)) {
      let query = u.search;
      try {
        query = decodeURIComponent(u.search);
      } catch (e) {
        // A "%" that is no escape: the raw text is searched.
      }
      for (const [re, k] of queryKinds) {
        if (re.test(query)) {
          kind = k;
          break;
        }
      }
      if (!kind) {
        const name = dispositionName(headers);
        if (name && !segmentPath.test(name)) kind = kindOfPath(name);
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

  // formatDuration writes seconds as 1:02:03 or 2:03.
  function formatDuration(seconds) {
    const s = Math.round(Number(seconds) || 0);
    if (s <= 0) return "";
    const two = (n) => String(n).padStart(2, "0");
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    return h > 0 ? h + ":" + two(m) + ":" + two(s % 60) : m + ":" + two(s % 60);
  }

  // How much a find is worth, by where it comes from: "sniff" read the
  // answer itself (page.js), "net" saw the request (background.js), "dom"
  // only saw the page name the link (content.js).
  const viaRank = (via) => ({ dom: 0, net: 1, sniff: 2 })[via || "net"];

  // rank puts what downloads best to the top of the list: a stream with all
  // its qualities, then streams nobody looked into, then one quality of a
  // stream, files, embedded players.
  function rank(item) {
    switch (item.kind) {
      case "hls": return item.master === true ? 0 : item.master === false ? 2 : 1;
      case "dash": case "ism": return 0;
      case "video": return 3;
      case "embed": return 4;
    }
    return 5;
  }

  // mergeItem joins two finds of one link: what the better source says
  // counts, the Referer of the real request stays.
  function mergeItem(old, item) {
    const strong = viaRank(item.via) >= viaRank(old.via) ? item : old;
    const weak = strong === item ? old : item;
    const net = viaRank(item.via) === 1 ? item : viaRank(old.via) === 1 ? old : null;
    return {
      url: item.url,
      kind: strong.kind,
      via: strong.via,
      master: strong.master !== undefined ? strong.master : weak.master,
      duration: strong.duration || weak.duration || 0,
      size: strong.size || weak.size || 0,
      referer: (net && net.referer) || strong.referer || weak.referer || "",
      headers: (net && net.headers) || strong.headers || weak.headers,
      frameUrl: strong.frameUrl || weak.frameUrl || "",
      time: item.time,
    };
  }

  // addItem puts a found video into a tab's list: each link once, the best
  // on top (rank), the newest first among equals, at most maxItemsPerTab.
  function addItem(list, item) {
    const all = list || [];
    const old = all.find((x) => x.url === item.url);
    const rest = all.filter((x) => x.url !== item.url);
    const sorted = [old ? mergeItem(old, item) : item, ...rest];
    sorted.sort((a, b) => rank(a) - rank(b)); // stable: the order stays among equals
    return sorted.slice(0, maxItemsPerTab);
  }

  const isWeb = (url) => typeof url === "string" && url.length <= 4096 && /^https?:\/\//i.test(url);

  // pageItems turns a message of content.js into list items. The message
  // comes out of a web page, so nothing in it is trusted: only http(s)
  // links, only known kinds, and the frame's address is the one the browser
  // names (sender), not one from the message.
  function pageItems(message, sender, now) {
    const frame = isWeb(sender.url) ? sender.url : "";
    const frameUrl = sender.frameId > 0 ? frame : "";
    const out = [];
    for (const f of Array.isArray(message.items) ? message.items.slice(0, 20) : []) {
      if (!f || !isWeb(f.url)) continue;
      const item = { url: f.url, size: 0, referer: frame, frameUrl, time: now };
      if (f.via === "sniff" && ["hls", "dash", "ism"].includes(f.kind)) {
        item.via = "sniff";
        item.kind = f.kind;
        if (f.kind === "hls") item.master = f.master === true;
        const d = Number(f.duration);
        item.duration = Number.isFinite(d) && d > 0 && d < 1e6 ? Math.round(d) : 0;
      } else if (f.via === "dom") {
        item.via = "dom";
        item.kind = kindOfUrl(f.url) || (f.kind === "audio" ? "audio" : "video");
      } else {
        continue;
      }
      out.push(item);
    }
    // A player inside a frame: the frame's own address is something yt-dlp
    // often knows, where the page around it says nothing.
    if (message.embed === true && frameUrl) {
      out.push({ url: frameUrl, kind: "embed", via: "dom", size: 0, referer: "", frameUrl: "", time: now });
    }
    return out;
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

  // storeOf names the cookie store of a tab: a private window and a
  // Firefox container have cookies of their own. Firefox says it on the
  // tab; Chrome and Edge list the tabs of each store.
  async function storeOf(tab) {
    if (!tab) return undefined;
    if (tab.cookieStoreId) return tab.cookieStoreId;
    try {
      const stores = await api.cookies.getAllCookieStores();
      const store = stores.find((s) => (s.tabIds || []).includes(tab.id));
      return store ? store.id : undefined;
    } catch (e) {
      return undefined; // the browser's usual store
    }
  }

  // cookiesOf reads the cookies for one link. The browsers keep the
  // cookies of an embedded player apart from the ones the same site has as
  // a page of its own (partitioned, Firefox also by first party): asked
  // plainly, they are left out, and it is the player that has the login.
  // Each browser knows other words for "all of them", and refuses the ones
  // it does not know, so the questions go from the most complete to the
  // plain one.
  async function cookiesOf(url, storeId) {
    const store = storeId ? { storeId } : {};
    const questions = [
      { url, ...store, partitionKey: {}, firstPartyDomain: null }, // Firefox
      { url, ...store, partitionKey: {} }, // Chrome, Edge
      { url, ...store },
      { url },
    ];
    for (const q of questions) {
      try {
        return await api.cookies.getAll(q);
      } catch (e) {
        // Not a question for this browser: the next one.
      }
    }
    // A link the browser cannot read cookies for: the download goes
    // without them, which is what happens without the option too.
    return [];
  }

  // cookiesFor collects the cookies for the given links (the video, the
  // page, the player's frame) in the Netscape format, from the cookie store
  // of the tab.
  async function cookiesFor(urls, tab) {
    const all = [];
    const storeId = await storeOf(tab);
    for (const url of new Set(urls.filter((u) => /^https?:/i.test(u || "")))) {
      all.push(...(await cookiesOf(url, storeId)));
    }
    return all.length > 0 ? netscapeCookies(all) : "";
  }

  // withLogin adds the site's login to a message: the cookies of the
  // video, the page and, for a found video, of the frame it plays in; and
  // the browser's name, because a login is often only good for the browser
  // it was made in.
  async function withLogin(message, tab, item) {
    const urls = [message.url, tab && tab.url, item && item.frameUrl, item && item.referer];
    message.cookies = await cookiesFor(urls, tab);
    const ua = globalThis.navigator && globalThis.navigator.userAgent;
    const has = Object.keys(message.headers || {}).some((k) => k.toLowerCase() === "user-agent");
    if (ua && !has) message.headers = { ...message.headers, "User-Agent": ua };
    return message;
  }

  // buildMessage makes the "add" message for a found video or for the page.
  // A video gets the page title as its file name, because a stream link has
  // no name of its own ("index-v1-a1.m3u8"); a page is named by yt-dlp. An
  // embedded player is a page too, and its host mostly wants to see the page
  // it is embedded in. A video goes with the headers its request had in the
  // browser (sentHeaders); a page is asked for anew, so it has none.
  function buildMessage(tab, item) {
    if (!item) {
      return { type: "add", url: tab.url, pageUrl: tab.url, title: tab.title || "", referer: "", fileName: "", headers: {} };
    }
    if (item.kind === "embed") {
      return { type: "add", url: item.url, pageUrl: tab.url, title: tab.title || "", referer: tab.url, fileName: "", headers: {} };
    }
    return {
      type: "add", url: item.url, pageUrl: tab.url, title: tab.title || "",
      referer: item.referer || item.frameUrl || tab.url, fileName: tab.title || "", headers: { ...item.headers },
    };
  }

  globalThis.vc = {
    api, nativeHost, maxItemsPerTab, classify, totalSize, header, netscapeCookies, hostOf, shortPath,
    formatSize, formatDuration, addItem, rank, kindOfUrl, pageItems, sendToApp, cookiesFor, withLogin, buildMessage,
    sentHeaders,
  };
})();
