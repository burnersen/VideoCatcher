// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Checks of the extension without a browser: node extension/extension_check.js
// Loads the shipped src/common.js (not a copy) and checks what decides
// which requests are videos, the list of a tab, the cookie format, the
// messages to the helper, the manifests and the texts; and the two scripts
// that run in the pages (page.js, content.js) against a fake page.
"use strict";
const fs = require("fs");
const path = require("path");
const vm = require("vm");
const { execFileSync } = require("child_process");

const src = path.join(__dirname, "src");
let failures = 0;
function check(name, ok, detail) {
  console.log((ok ? "ok    " : "FAIL  ") + name + (ok || detail === undefined ? "" : "\n      " + JSON.stringify(detail)));
  if (!ok) failures++;
}

function load(api) {
  const context = vm.createContext({ URL, console, chrome: api || {} });
  vm.runInContext(fs.readFileSync(path.join(src, "common.js"), "utf8"), context, { filename: "common.js" });
  return context.vc;
}

(async () => {
  const vc = load();
  const h = (pairs) => Object.entries(pairs).map(([name, value]) => ({ name, value }));

  // Which requests are videos.
  const cases = [
    ["https://cdn.x.net/hls/master.m3u8?token=1", "xmlhttprequest", {}, "hls"],
    ["https://cdn.x.net/v/manifest.mpd", "xmlhttprequest", {}, "dash"],
    ["https://cdn.x.net/v/movie.mp4", "media", { "Content-Length": "52428800" }, "video"],
    ["https://cdn.x.net/v/movie.mp4", "media", { "Content-Range": "bytes 0-1023/52428800", "Content-Length": "1024" }, "video"],
    ["https://cdn.x.net/v/movie.mp4", "media", {}, "video"],
    ["https://cdn.x.net/ads/preroll.mp4", "media", { "Content-Length": "300000" }, null],
    ["https://cdn.x.net/get?id=5", "xmlhttprequest", { "Content-Type": "application/vnd.apple.mpegurl" }, "hls"],
    ["https://cdn.x.net/get?id=6", "xmlhttprequest", { "Content-Type": "application/x-mpegURL; charset=utf-8" }, "hls"],
    ["https://cdn.x.net/get?id=7", "other", { "Content-Type": "application/dash+xml" }, "dash"],
    ["https://cdn.x.net/stream", "media", { "Content-Type": "video/webm" }, "video"],
    ["https://cdn.x.net/seg/1.ts", "xmlhttprequest", { "Content-Type": "video/mp2t" }, null],
    ["https://cdn.x.net/get?seg=1", "xmlhttprequest", { "Content-Type": "video/mp2t" }, null],
    ["https://cdn.x.net/chunk-stream0-00001.m4s", "xmlhttprequest", { "Content-Type": "video/iso.segment" }, null],
    ["https://cdn.x.net/thumb.jpg", "image", { "Content-Type": "image/jpeg" }, null],
    ["https://cdn.x.net/player.js", "script", {}, null],
    ["https://cdn.x.net/a.m3u8", "image", {}, null],
    ["https://cdn.x.net/subs/de.vtt", "xmlhttprequest", {}, null],
    ["blob:https://x.net/1234", "media", {}, null],
    ["data:video/mp4;base64,AAAA", "media", {}, null],
    ["not a link", "media", {}, null],
    ["https://x.net/page.html", "main_frame", { "Content-Type": "text/html" }, null],
    ["https://x.net/song.mp3", "media", { "Content-Length": "4000000" }, "audio"],
    // Streams that hide: named behind the "?", by other types, by the
    // file name of a download.
    ["https://x.net/get.php?file=a%2Fmaster.m3u8&t=1", "xmlhttprequest", { "Content-Type": "application/octet-stream" }, "hls"],
    ["https://x.net/proxy?url=https%3A%2F%2Fcdn.x.net%2Fa.mpd", "xmlhttprequest", {}, "dash"],
    ["https://x.net/stats?page=https%3A%2F%2Fcdn.x.net%2Fa.m3u8", "xmlhttprequest", { "Content-Type": "application/json" }, null],
    ["https://x.net/watch?next=a.m3u8", "sub_frame", { "Content-Type": "text/html; charset=utf-8" }, null],
    ["https://x.net/get?file=a.m3u8&seg=4", "xmlhttprequest", { "Content-Type": "video/mp2t" }, null],
    ["https://x.net/get?id=8", "xmlhttprequest", { "Content-Type": "application/mpegurl" }, "hls"],
    ["https://x.net/get?id=9", "xmlhttprequest", { "Content-Type": "video/vnd.mpeg.dash.mpd" }, "dash"],
    ["https://x.net/v/film.ism/Manifest", "xmlhttprequest", { "Content-Type": "text/xml" }, "ism"],
    ["https://x.net/get?id=10", "xmlhttprequest", { "Content-Type": "application/vnd.ms-sstr+xml" }, "ism"],
    ["https://x.net/radio/list.m3u", "xmlhttprequest", {}, "hls"],
    ["https://x.net/dl?id=3", "other", { "Content-Type": "application/octet-stream", "Content-Disposition": 'attachment; filename="My film.mp4"', "Content-Length": "90000000" }, "video"],
    ["https://x.net/dl?id=4", "other", { "Content-Type": "application/octet-stream", "Content-Disposition": "attachment; filename*=UTF-8''film.mkv" }, "video"],
    ["https://x.net/dl?id=5", "other", { "Content-Type": "application/octet-stream", "Content-Disposition": 'attachment; filename="setup.zip"' }, null],
    ["https://x.net/dl?id=6", "xmlhttprequest", { "Content-Type": "application/octet-stream", "Content-Disposition": 'attachment; filename="seg-4.ts"' }, null],
  ];
  for (const [url, type, headers, want] of cases) {
    const got = vc.classify(url, type, h(headers));
    check(`classify ${type} ${url} ${JSON.stringify(headers)} -> ${want}`, got === want, got);
  }

  // The list of a tab: newest first, each link once, a limit.
  let list = [];
  for (let i = 0; i < 50; i++) list = vc.addItem(list, { url: "https://x.net/" + (i % 45) + ".m3u8" });
  check("list keeps each link once", new Set(list.map((x) => x.url)).size === list.length);
  check("list has a limit", list.length === vc.maxItemsPerTab);
  check("newest first", list[0].url === "https://x.net/4.m3u8");

  // The best on top: a stream with all qualities, then streams nobody
  // looked into, single qualities, files, players. Files go first when the
  // list is full, so the segments of a stream cannot push it out.
  {
    let l = [];
    const put = (x) => { l = vc.addItem(l, { via: "net", ...x }); };
    put({ url: "https://x.net/embed/1", kind: "embed", via: "dom" });
    put({ url: "https://x.net/a.mp4", kind: "video" });
    put({ url: "https://x.net/q/720", kind: "hls", master: false, via: "sniff" });
    put({ url: "https://x.net/b.m3u8", kind: "hls" });
    put({ url: "https://x.net/master", kind: "hls", master: true, via: "sniff" });
    put({ url: "https://x.net/song.mp3", kind: "audio" });
    check("list order: best first", l.map((x) => x.url.split("/").pop()).join(" ") === "master b.m3u8 720 a.mp4 1 song.mp3", l.map((x) => x.url));
    for (let i = 0; i < 60; i++) put({ url: "https://x.net/seg" + i + ".mp4", kind: "video" });
    check("a full list keeps the streams", l.length === vc.maxItemsPerTab && l[0].url === "https://x.net/master" && l[2].url === "https://x.net/q/720");

    // One link seen twice: the answer itself counts more than the link, the
    // Referer of the real request stays.
    let m = vc.addItem([], { url: "https://x.net/get.mp4", kind: "video", via: "net", size: 5000000, referer: "https://player.net/e/1", frameUrl: "https://player.net", time: 1 });
    m = vc.addItem(m, { url: "https://x.net/get.mp4", kind: "hls", via: "sniff", master: true, duration: 0, size: 0, referer: "https://player.net/", frameUrl: "https://player.net/e/1", time: 2 });
    check("merge: the read answer decides the kind", m.length === 1 && m[0].kind === "hls" && m[0].master === true && m[0].via === "sniff", m);
    check("merge: the Referer of the request stays", m[0].referer === "https://player.net/e/1" && m[0].time === 2, m);
    m = vc.addItem(m, { url: "https://x.net/get.mp4", kind: "video", via: "dom", size: 0, referer: "https://other.net/", frameUrl: "", time: 3 });
    check("merge: the markup does not overrule", m[0].kind === "hls" && m[0].master === true && m[0].referer === "https://player.net/e/1", m);
  }

  // What a page reports (content.js): nothing in it is trusted.
  {
    const sender = { url: "https://player.net/e/1", frameId: 3 };
    const got = vc.pageItems({
      items: [
        { url: "https://cdn.net/get?id=1", kind: "hls", master: true, duration: 0, via: "sniff" },
        { url: "https://cdn.net/q/1", kind: "hls", master: false, duration: 5400.4, via: "sniff" },
        { url: "https://cdn.net/film", kind: "video", via: "dom" },
        { url: "https://cdn.net/a/index.m3u8", kind: "video", via: "dom" },
        { url: "javascript:alert(1)", kind: "hls", via: "sniff" },
        { url: "file:///C:/x.m3u8", kind: "video", via: "dom" },
        { url: "https://cdn.net/x", kind: "embed", via: "sniff" },
        { url: "https://cdn.net/y", kind: "hls", via: "net" },
        { url: "https://cdn.net/" + "a".repeat(5000), kind: "hls", via: "sniff" },
        { url: "https://cdn.net/z", kind: "hls", master: "yes", duration: "<b>", via: "sniff" },
        null, "text",
      ],
      embed: true,
    }, sender, 7);
    check("page message: only web links and known kinds", got.map((x) => x.url.replace("https://", "")).join(" ") ===
      "cdn.net/get?id=1 cdn.net/q/1 cdn.net/film cdn.net/a/index.m3u8 cdn.net/z player.net/e/1", got.map((x) => x.url));
    check("page message: kinds", got.map((x) => x.kind).join(" ") === "hls hls video hls hls embed", got.map((x) => x.kind));
    check("page message: master and duration are checked", got[0].master === true && got[1].duration === 5400 && got[4].master === false && got[4].duration === 0, got);
    check("page message: referer and frame come from the browser", got[0].referer === sender.url && got[0].frameUrl === sender.url && got[0].time === 7);
    const top = vc.pageItems({ items: [{ url: "https://cdn.net/a", kind: "dash", via: "sniff" }], embed: true }, { url: "https://site.net/watch", frameId: 0 }, 1);
    check("page message: the page itself is no embedded player", top.length === 1 && top[0].frameUrl === "" && top[0].referer === "https://site.net/watch", top);
    const blank = vc.pageItems({ items: [{ url: "https://cdn.net/a", kind: "dash", via: "sniff" }], embed: true }, { url: "about:blank", frameId: 2 }, 1);
    check("page message: a frame without an address", blank.length === 1 && blank[0].referer === "" && blank[0].frameUrl === "", blank);
    check("page message: rubbish gives nothing", vc.pageItems({ items: "x" }, sender, 1).length === 0 && vc.pageItems({}, { url: "", frameId: 0 }, 1).length === 0);
    const many = vc.pageItems({ items: Array.from({ length: 100 }, (_, i) => ({ url: "https://cdn.net/" + i, kind: "dash", via: "sniff" })) }, sender, 1);
    check("page message: a limit per message", many.length === 20, many.length);
  }
  check("duration as text", vc.formatDuration(59) === "0:59" && vc.formatDuration(125) === "2:05" && vc.formatDuration(3723) === "1:02:03" && vc.formatDuration(0) === "");

  // page.js, which runs inside the pages: it finds a stream by the start
  // of an answer, tells only its link, and leaves the page's answers alone.
  {
    const master = "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nlow.m3u8\n";
    const media = "\uFEFF#EXTM3U\n" + "#EXTINF:10.0,\nseg.jpg\n".repeat(90) + "#EXT-X-ENDLIST\n";
    const live = "#EXTM3U\n" + "#EXTINF:6.0,\nseg.ts\n".repeat(5);
    const mpd = '<?xml version="1.0"?>\n<!-- c -->\n<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"></MPD>';
    const bodies = {
      "https://x.net/api/1": ["text/plain", master], "https://x.net/api/2": ["application/octet-stream", media],
      "https://x.net/api/3": ["text/xml", mpd], "https://x.net/api/4": ["application/json", '{"a":"#EXTM3U"}'],
      "https://x.net/api/5": ["text/html", "<html><body>#EXTM3U</body></html>" + " ".repeat(5000)],
      "https://x.net/api/6": ["image/png", master], "https://x.net/api/7": ["text/plain", live],
      "https://x.net/api/8": ["text/xml", '<SmoothStreamingMedia MajorVersion="2"></SmoothStreamingMedia>'],
      "blob:https://x.net/1-2-3": ["", master],
    };
    const events = [];
    let now = 1000000;
    const enc = new TextEncoder();
    class FakeXhr {
      constructor() { this.listeners = {}; this.responseType = ""; this.status = 0; }
      addEventListener(name, fn) { this.listeners[name] = fn; }
      open(method, url) { this.url = url; return "opened"; }
      finish() {
        const text = bodies[this.url][1];
        this.status = 200;
        this.responseURL = this.url;
        if (this.responseType === "arraybuffer") this.response = enc.encode(text).buffer;
        else if (this.responseType === "blob") this.response = new Blob([text]);
        else if (this.responseType === "json") this.response = {};
        else this.responseText = text;
        this.listeners.load.call(this);
      }
    }
    const pageFetch = async (url) => {
      const [type, text] = bodies[url];
      const r = new Response(text, { headers: type ? { "content-type": type } : {} });
      Object.defineProperty(r, "url", { value: url });
      const clone = r.clone.bind(r);
      r.clone = () => { const c = clone(); Object.defineProperty(c, "url", { value: url }); return c; };
      return r;
    };
    const win = { fetch: pageFetch, XMLHttpRequest: FakeXhr };
    const ctx = vm.createContext({
      window: win, location: { href: "https://x.net/watch" }, URL, TextDecoder, Uint8Array, Map, JSON, Math,
      Date: { now: () => now },
      CustomEvent: class { constructor(name, init) { this.name = name; this.detail = init.detail; } },
      document: { dispatchEvent: (e) => events.push(e) },
    });
    vm.runInContext(fs.readFileSync(path.join(src, "page.js"), "utf8"), ctx, { filename: "page.js" });
    const settle = () => new Promise((r) => setTimeout(r, 20));
    const found = () => events.map((e) => JSON.parse(e.detail));

    const r1 = await win.fetch("https://x.net/api/1");
    check("page.js: the page still gets its answer", (await r1.text()) === master);
    for (const n of [2, 3, 4, 5, 6, 7, 8]) await (await win.fetch("https://x.net/api/" + n)).text();
    await settle();
    const byUrl = Object.fromEntries(found().map((d) => [d.url, d]));
    check("page.js: an event of its own name, text only", events.length > 0 && events.every((e) => e.name === "videocatcher-found" && typeof e.detail === "string"));
    check("page.js fetch: HLS list of qualities as text/plain", byUrl["https://x.net/api/1"] && byUrl["https://x.net/api/1"].kind === "hls" && byUrl["https://x.net/api/1"].master === true, found());
    check("page.js fetch: one quality with its length", byUrl["https://x.net/api/2"] && byUrl["https://x.net/api/2"].master === false && byUrl["https://x.net/api/2"].duration === 900, found());
    check("page.js fetch: DASH behind a comment", byUrl["https://x.net/api/3"] && byUrl["https://x.net/api/3"].kind === "dash", found());
    check("page.js fetch: Smooth Streaming", byUrl["https://x.net/api/8"] && byUrl["https://x.net/api/8"].kind === "ism", found());
    check("page.js fetch: a live list has no length", byUrl["https://x.net/api/7"] && byUrl["https://x.net/api/7"].duration === 0, found());
    check("page.js fetch: JSON, a web page and a picture are no stream", !byUrl["https://x.net/api/4"] && !byUrl["https://x.net/api/5"] && !byUrl["https://x.net/api/6"], found());
    check("page.js: nothing of the answer leaves", events.every((e) => !/EXT-X|EXTINF|xmlns/.test(e.detail)));

    events.length = 0;
    await win.fetch("https://x.net/api/1");
    await settle();
    check("page.js: the same link is not told again at once", events.length === 0, found());
    now += 20000;
    await win.fetch("https://x.net/api/1");
    await settle();
    check("page.js: but again later (a cleared list)", events.length === 1, found());

    events.length = 0;
    await win.fetch("blob:https://x.net/1-2-3");
    await settle();
    check("page.js: a list kept in memory is told as hidden", events.length === 1 && found()[0].blob === true && !found()[0].url, found());

    events.length = 0;
    now += 20000;
    for (const [type, url] of [["", "https://x.net/api/1"], ["arraybuffer", "https://x.net/api/2"], ["blob", "https://x.net/api/3"], ["json", "https://x.net/api/8"], ["text", "https://x.net/api/4"]]) {
      const x = new win.XMLHttpRequest();
      x.responseType = type;
      const opened = x.open("GET", url);
      if (url.endsWith("/1")) check("page.js XHR: open still does its work", opened === "opened" && x.url === url);
      x.finish();
    }
    await settle();
    check("page.js XHR: text, bytes and blob are read, the rest left alone", found().map((d) => d.url.slice(-1) + d.kind).sort().join(" ") === "1hls 2hls 3dash", found());

    const bad = new win.XMLHttpRequest();
    bad.open("GET", "https://x.net/api/1");
    bad.status = 404;
    let threw = false;
    try { bad.listeners.load.call(bad); bad.listeners.load.call({}); } catch (e) { threw = true; }
    check("page.js: a failed or strange request does no harm", !threw);
    let rejected = false;
    win.fetch = undefined;
    const ctx2 = vm.createContext({ window: { fetch: () => Promise.reject(new Error("offline")) }, location: { href: "https://x.net/" }, URL, TextDecoder, Uint8Array, Map, JSON, Math, Date, CustomEvent: class {}, document: { dispatchEvent() {} } });
    vm.runInContext(fs.readFileSync(path.join(src, "page.js"), "utf8"), ctx2, { filename: "page.js" });
    await ctx2.window.fetch("https://x.net/").catch(() => { rejected = true; });
    check("page.js: the page's failed request stays the page's", rejected);
  }

  // content.js, next to the pages: the videos a page names, what page.js
  // found, a video from memory, a player in a frame.
  {
    const run = async (top, elements, events) => {
      const sent = [];
      const docListeners = {};
      const winListeners = {};
      const document = {
        addEventListener: (name, fn) => { docListeners[name] = fn; },
        querySelectorAll: (sel) => (sel === "video, audio" ? elements : []),
      };
      const window = { addEventListener: (name, fn) => { winListeners[name] = fn; } };
      window.top = top ? window : {};
      const ctx = vm.createContext({
        window, document, location: { href: "https://player.net/e/1" }, Date, Map, Set, WeakSet, JSON, Number,
        setTimeout: (fn, ms) => setTimeout(fn, Math.min(ms, 5)),
        chrome: { runtime: { sendMessage: (m) => { sent.push(JSON.parse(JSON.stringify(m))); return Promise.resolve(); } } },
      });
      vm.runInContext(fs.readFileSync(path.join(src, "content.js"), "utf8"), ctx, { filename: "content.js" });
      for (const detail of events) docListeners["videocatcher-found"]({ detail });
      docListeners.DOMContentLoaded();
      for (const el of elements) docListeners.playing({ target: el });
      await new Promise((r) => setTimeout(r, 40));
      return sent;
    };
    const el = (tag, o) => ({ tagName: tag, duration: NaN, paused: true, clientWidth: 640, controls: false, currentSrc: "", src: "", querySelectorAll: () => [], ...o });
    const elements = [
      el("VIDEO", { src: "https://cdn.net/film.mp4", querySelectorAll: () => [{ src: "https://cdn.net/film.webm" }] }),
      el("VIDEO", { src: "https://cdn.net/tile.mp4", clientWidth: 160 }),
      el("VIDEO", { src: "https://cdn.net/ad.mp4", duration: 8 }),
      el("VIDEO", { currentSrc: "blob:https://player.net/1", paused: false }),
      el("AUDIO", { src: "https://cdn.net/talk.mp3", duration: 1800 }),
      el("AUDIO", { src: "https://cdn.net/click.mp3" }),
      el("DIV", { src: "https://cdn.net/no.mp4" }),
    ];
    const events = [
      JSON.stringify({ url: "https://cdn.net/get?id=1", kind: "hls", master: true, duration: 0 }),
      JSON.stringify({ blob: true }), "not json", { url: "https://cdn.net/object" }, "x".repeat(9000), "null",
    ];
    const sent = await run(false, elements, events);
    const all = sent.flatMap((m) => m.items);
    check("content.js: one message for what it found", sent.length === 1 && sent[0].type === "vc-found", sent);
    check("content.js: the stream from page.js", all.some((x) => x.url === "https://cdn.net/get?id=1" && x.via === "sniff" && x.master === true), all);
    check("content.js: videos named by the page, each once", all.filter((x) => x.via === "dom").map((x) => x.url.split("/").pop() + ":" + x.kind).sort().join(" ") === "film.mp4:video film.webm:video talk.mp3:audio", all);
    check("content.js: a video from memory and a player in a frame", sent[0].blob === true && sent[0].embed === true, sent);
    const topSent = await run(true, [elements[0]], []);
    check("content.js: the page itself is no embedded player", topSent.length === 1 && topSent[0].embed === false && topSent[0].blob === false, topSent);
    const none = await run(true, [elements[1], elements[2]], ["not json"]);
    check("content.js: nothing found, nothing sent", none.length === 0, none);
  }

  // Cookies in the format yt-dlp reads.
  const text = vc.netscapeCookies([
    { domain: ".example.org", path: "/", secure: true, httpOnly: true, expirationDate: 1893456000.5, name: "sid", value: "abc" },
    { domain: "example.org", path: "/x", secure: false, session: true, name: "pref", value: "1" },
    { domain: ".example.org", path: "/", secure: true, httpOnly: true, expirationDate: 1893456000.5, name: "sid", value: "abc" },
    { domain: "evil.org", path: "/", name: "a\tb", value: "x" },
  ]);
  const lines = text.trim().split("\n");
  check("cookie file header", lines[0] === "# Netscape HTTP Cookie File", lines[0]);
  check("httpOnly cookie line", lines.includes("#HttpOnly_.example.org\tTRUE\t/\tTRUE\t1893456000\tsid\tabc"), lines);
  check("session cookie line", lines.includes("example.org\tFALSE\t/x\tFALSE\t0\tpref\t1"), lines);
  check("each cookie once, broken ones left out", lines.length === 4, lines);

  // The messages to the helper (fields as nativeMessage in nativehost.go).
  const tab = { url: "https://site.example/watch/1", title: "A title | Site" };
  const page = vc.buildMessage(tab, null);
  check("page message", page.type === "add" && page.url === tab.url && page.fileName === "" && page.referer === "");
  const item = vc.buildMessage(tab, { url: "https://cdn.example/x/index.m3u8", referer: "https://player.example/e/1", frameUrl: "https://player.example/e/1" });
  check("stream message: referer and name", item.url === "https://cdn.example/x/index.m3u8" && item.referer === "https://player.example/e/1" && item.fileName === "A title | Site" && item.pageUrl === tab.url);
  const player = vc.buildMessage(tab, { url: "https://player.example/e/1", kind: "embed", referer: "" });
  check("embedded player message: its page, the page around it as referer, named by yt-dlp", player.url === "https://player.example/e/1" && player.referer === tab.url && player.fileName === "" && player.pageUrl === tab.url);
  const noRef = vc.buildMessage(tab, { url: "https://cdn.example/x.mp4" });
  check("stream without referer: the page", noRef.referer === tab.url);
  const goFields = [...fs.readFileSync(path.join(__dirname, "..", "nativehost.go"), "utf8")
    .match(/type nativeMessage struct \{([\s\S]*?)\n\}/)[1].matchAll(/json:"([^"]+)"/g)].map((m) => m[1]);
  for (const k of Object.keys(item).concat("cookies")) check(`field "${k}" is known to the helper`, goFields.includes(k), goFields);

  // The way to the app: a missing helper is a reply, not an exception.
  {
    const v = load({ runtime: { sendNativeMessage: async () => { throw new Error("Specified native messaging host not found."); } } });
    const r = await v.sendToApp({ type: "ping" });
    check("missing helper: not-connected", r.ok === false && r.code === "not-connected");
    const w = load({ runtime: { sendNativeMessage: async (host, m) => ({ ok: true, version: "0.2.0", host, m }) } });
    const r2 = await w.sendToApp({ type: "ping" });
    check("helper name", r2.ok && r2.host === "de.burnersen.videocatcher");
    const hostGo = /nativeHostName = "([^"]+)"/.exec(fs.readFileSync(path.join(__dirname, "..", "nativehost.go"), "utf8"))[1];
    check("same helper name as nativehost.go", hostGo === vc.nativeHost);
  }

  // Manifest and build.
  const manifest = JSON.parse(fs.readFileSync(path.join(src, "manifest.json"), "utf8"));
  check("manifest version 3", manifest.manifest_version === 3);
  const pageScripts = manifest.content_scripts.flatMap((c) => c.js);
  for (const f of [manifest.background.service_worker, manifest.action.default_popup, ...Object.values(manifest.icons), ...pageScripts]) {
    check(`file ${f} exists`, fs.existsSync(path.join(src, f)));
  }
  check("only the permissions it needs", JSON.stringify([...manifest.permissions].sort()) ===
    JSON.stringify(["contextMenus", "cookies", "nativeMessaging", "storage", "webRequest"]), manifest.permissions);
  // The scripts in the pages: every frame, before the page's own scripts,
  // page.js inside the page (it must replace fetch before the player
  // calls it), content.js outside it, and web pages only.
  check("page scripts: page.js in the page, content.js beside it", manifest.content_scripts.length === 2 &&
    manifest.content_scripts[0].world === "MAIN" && manifest.content_scripts[0].js.join() === "page.js" &&
    manifest.content_scripts[1].world === undefined && manifest.content_scripts[1].js.join() === "content.js", manifest.content_scripts);
  for (const c of manifest.content_scripts) {
    check(`page script ${c.js}: every frame, at the start, web pages only`, c.all_frames === true && c.run_at === "document_start" &&
      JSON.stringify(c.matches) === '["http://*/*","https://*/*"]', c);
  }
  const pageJs = fs.readFileSync(path.join(src, "page.js"), "utf8");
  const contentJs = fs.readFileSync(path.join(src, "content.js"), "utf8");
  const eventNames = [pageJs, contentJs].map((t) => /const eventName = "([^"]+)"/.exec(t)[1]);
  check("page.js and content.js use the same event", eventNames[0] === eventNames[1], eventNames);
  check("page.js keeps no browser API and makes no request", !/chrome\.|browser\.|sendMessage|pageFetch\((?!\.)|new Xhr|sendBeacon|WebSocket/.test(pageJs));
  check("the page scripts write nothing into the page", !/innerHTML|document\.write|appendChild|insertAdjacent|\beval\(/.test(pageJs + contentJs));
  execFileSync(process.execPath, [path.join(__dirname, "build.js"), "1.2.3"], { stdio: "ignore" });
  const mc = JSON.parse(fs.readFileSync(path.join(__dirname, "dist", "chromium", "manifest.json"), "utf8"));
  const mf = JSON.parse(fs.readFileSync(path.join(__dirname, "dist", "firefox", "manifest.json"), "utf8"));
  check("chromium build: worker, key, no gecko", mc.background.service_worker === "background.js" && mc.key && !mc.browser_specific_settings && mc.version === "1.2.3");
  check("firefox build: scripts, gecko, no key", JSON.stringify(mf.background.scripts) === '["common.js","background.js"]' && !mf.key &&
    mf.browser_specific_settings.gecko.id === "videocatcher@burnersen.de" && mf.version === "1.2.3");

  // Texts: both languages have the same keys, and every key used exists.
  const en = JSON.parse(fs.readFileSync(path.join(src, "_locales", "en", "messages.json"), "utf8"));
  const de = JSON.parse(fs.readFileSync(path.join(src, "_locales", "de", "messages.json"), "utf8"));
  check("English and German have the same keys", JSON.stringify(Object.keys(en).sort()) === JSON.stringify(Object.keys(de).sort()));
  const used = new Set();
  for (const f of ["popup.html", "popup.js", "background.js", "manifest.json"]) {
    const s = fs.readFileSync(path.join(src, f), "utf8");
    for (const m of s.matchAll(/data-i18n="([^"]+)"|msg\("([^"]+)"|getMessage\(key\)|__MSG_([A-Za-z]+)__|m\("vc-[a-z]+", \[[^\]]*\], "([^"]+)"\)/g)) {
      const k = m[1] || m[2] || m[3] || m[4];
      if (k) used.add(k);
    }
  }
  check("texts are used at all", used.size > 15, [...used]);
  for (const k of used) check(`text "${k}" exists`, k in en);
  for (const [k, v] of Object.entries(de)) check(`German "${k}" without dashes`, !/[\u2013\u2014]/.test(v.message));

  // The scripts load together as the browser loads them: one global scope.
  // A name declared twice stops the second file (see common.js).
  {
    const ev = () => ({ addListener() {} });
    const fake = {
      runtime: { onInstalled: ev(), onMessage: ev(), sendNativeMessage: async () => ({ ok: true }) },
      webRequest: { onSendHeaders: ev(), onHeadersReceived: ev(), onBeforeRequest: ev() },
      tabs: { onRemoved: ev() }, contextMenus: { onClicked: ev() }, storage: { session: {}, local: {}, onChanged: ev() },
      action: {}, i18n: { getMessage: (k) => k },
    };
    const doc = { addEventListener() {}, getElementById: () => ({}), querySelectorAll: () => [] };
    for (const [name, files] of [["background (Chrome)", ["background.js"]], ["background (Firefox)", ["common.js", "background.js"]], ["popup", ["common.js", "popup.js"]]]) {
      const ctx = vm.createContext({ URL, console, chrome: fake, document: doc, importScripts: undefined });
      if (name === "background (Chrome)") ctx.importScripts = (f) => vm.runInContext(fs.readFileSync(path.join(src, f), "utf8"), ctx, { filename: f });
      let err = null;
      try {
        for (const f of files) vm.runInContext(fs.readFileSync(path.join(src, f), "utf8"), ctx, { filename: f });
      } catch (e) {
        err = String(e);
      }
      check(`${name} loads without error`, err === null, err);
    }
  }

  // No page content as markup in the popup.
  const popupJs = fs.readFileSync(path.join(src, "popup.js"), "utf8");
  check("popup never uses innerHTML", !/innerHTML/.test(popupJs));

  if (failures > 0) {
    console.log(failures + " check(s) failed");
    process.exit(1);
  }
})();
