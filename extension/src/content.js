// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Runs next to every page and every frame (in the extension's own world,
// the page cannot reach it) and tells background.js what the requests alone
// do not show:
//
//   - the streams page.js found in the answers the player got, and a list
//     the player built in memory, which has no link,
//   - the videos the page names in its markup (<video>, <audio>, <source>),
//     also before they are loaded or when they came from the cache,
//   - that a video plays from memory (blob), so the popup can say why
//     there is nothing to list,
//   - that this frame holds a player: its address is worth a try,
//   - what page.js noted about how the player gets its video, for the
//     report a user can copy when nothing is found.
//
// It reads the page and changes nothing on it.
(() => {
  "use strict";

  const api = globalThis.browser || globalThis.chrome;
  const eventName = "videocatcher-found";
  const againAfterMs = 15000;
  const minSeconds = 20; // shorter clips are previews and ads
  const minWidth = 300; // a video drawn smaller is a preview tile
  const maxListChars = 3 * 1024 * 1024; // a list from memory, as JSON text

  let isTop = true;
  try {
    isTop = window.top === window;
  } catch (e) {
    isTop = false;
  }
  const webFrame = /^https?:/i.test(location.href);

  const told = new Map(); // link -> when it was last reported
  const watched = new WeakSet(); // media inside shadow trees that have listeners
  let items = [];
  let notes = [];
  let lists = [];
  let blob = false;
  let embed = false;
  let blobTold = false;
  let embedTold = false;
  let timer = null;

  function flush() {
    timer = null;
    const message = { type: "vc-found", items, notes, lists, blob: blob && !blobTold, embed: embed && !embedTold };
    items = [];
    notes = [];
    lists = [];
    if (message.items.length === 0 && message.notes.length === 0 && message.lists.length === 0 && !message.blob && !message.embed) return;
    blobTold = blobTold || blob;
    embedTold = embedTold || embed;
    try {
      const sent = api.runtime.sendMessage(message);
      if (sent && sent.catch) sent.catch(() => {}); // nobody answers, and nobody has to
    } catch (e) {
      // The extension was reloaded or removed while the page stayed open.
    }
  }

  function later() {
    if (timer === null) timer = setTimeout(flush, 300);
  }

  function add(item) {
    const now = Date.now();
    if (now - (told.get(item.url) || 0) < againAfterMs) return;
    if (told.size > 500) told.clear();
    told.set(item.url, now);
    items.push(item);
    later();
  }

  // What page.js found. The event comes out of the page's world: only a
  // short text is read, and background.js checks every field again.
  document.addEventListener(eventName, (e) => {
    let d;
    try {
      if (typeof e.detail !== "string") return;
      // Short, except for a list the player built in memory.
      if (e.detail.length > 8192 && !(e.detail.length <= maxListChars && e.detail.startsWith('{"list":"'))) return;
      d = JSON.parse(e.detail);
    } catch (err) {
      return;
    }
    if (!d || typeof d !== "object") return;
    if (typeof d.list === "string") {
      // A list without a link (background.js checks it again). The newest
      // counts: a player that starts over builds a new one.
      lists = [{ text: d.list, master: d.master === true, duration: d.duration }];
      later();
    } else if (typeof d.note === "string") {
      // For the report only (background.js checks it again).
      if (notes.length < 10 && d.note.length <= 200) notes.push(d.note);
      later();
    } else if (d.blob === true) {
      blob = true;
      later();
    } else if (typeof d.url === "string") {
      add({ url: d.url, kind: d.kind, master: d.master, duration: d.duration, via: "sniff" });
    }
  });

  // worth says whether a video or audio element is the thing a visitor came
  // for: long enough when its length is known, else big enough or playing.
  function worth(el, isVideo) {
    const d = el.duration;
    if (Number.isFinite(d)) return d >= minSeconds;
    if (d === Infinity) return true; // a live stream
    if (!el.paused) return true;
    return isVideo ? el.clientWidth >= minWidth : el.controls;
  }

  function look(el) {
    if (!el || (el.tagName !== "VIDEO" && el.tagName !== "AUDIO")) return;
    const isVideo = el.tagName === "VIDEO";
    if (!worth(el, isVideo)) return;
    const links = new Set([el.currentSrc, el.src]);
    for (const s of el.querySelectorAll("source[src]")) links.add(s.src);
    for (const link of links) {
      if (!link) continue;
      if (/^https?:/i.test(link)) add({ url: link, kind: isVideo ? "video" : "audio", via: "dom" });
      else if (/^(blob|mediastream):/i.test(link) && isVideo) blob = true;
    }
    if (isVideo && !isTop && webFrame) embed = true;
    if ((blob && !blobTold) || (embed && !embedTold)) later();
  }

  // Media events do not bubble, but they can be caught on the way down.
  const mediaEvents = ["loadedmetadata", "durationchange", "playing"];
  for (const name of mediaEvents) document.addEventListener(name, (e) => look(e.target), true);

  // scan looks at every video and audio element, also inside open shadow
  // trees (players built as web components), whose events do not come out.
  function scan(root, depth) {
    for (const el of root.querySelectorAll("video, audio")) {
      if (depth > 0 && !watched.has(el)) {
        watched.add(el);
        for (const name of mediaEvents) el.addEventListener(name, () => look(el));
      }
      look(el);
    }
    if (depth >= 3) return;
    let seen = 0;
    for (const el of root.querySelectorAll("*")) {
      if (++seen > 20000) break;
      if (el.shadowRoot) scan(el.shadowRoot, depth + 1);
    }
  }

  function scanPage() {
    try {
      scan(document, 0);
    } catch (e) {
      // A page that is going away while it is read.
    }
  }

  // Players are built late: when the page is read, when it is loaded, and
  // twice after that.
  document.addEventListener("DOMContentLoaded", scanPage);
  window.addEventListener("load", () => {
    scanPage();
    setTimeout(scanPage, 2000);
    setTimeout(scanPage, 6000);
  });
})();
