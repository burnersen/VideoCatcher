// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Checks of the extension without a browser: node extension/extension_check.js
// Loads the shipped src/common.js (not a copy) and checks what decides
// which requests are videos, the cookie format, the messages to the helper,
// the manifests and the texts.
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
  for (const f of [manifest.background.service_worker, manifest.action.default_popup, ...Object.values(manifest.icons)]) {
    check(`file ${f} exists`, fs.existsSync(path.join(src, f)));
  }
  check("only the permissions it needs", JSON.stringify([...manifest.permissions].sort()) ===
    JSON.stringify(["contextMenus", "cookies", "nativeMessaging", "storage", "webRequest"]), manifest.permissions);
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
      runtime: { onInstalled: ev(), sendNativeMessage: async () => ({ ok: true }) },
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
