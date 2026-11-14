// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Runs inside the page itself (world "MAIN"), before the page's own
// scripts. Many sites hand their player a stream behind a link and a type
// that say nothing ("/get.php?id=5" as text/plain), so background.js, which
// sees only links and headers, cannot know it. This script looks at the
// start of what fetch and XMLHttpRequest bring: an answer that begins like
// an HLS list, a DASH or a Smooth Streaming manifest is one.
//
// It only tells content.js the link of such an answer (an event on the
// document), nothing of its content, and makes no request of its own. The
// page gets its answers unchanged; whatever goes wrong in here is
// swallowed, so a page never breaks because of it. The page can see this
// script and could send made-up events: that only puts a link into the
// list the user reads, and background.js checks what arrives.
(() => {
  "use strict";

  const eventName = "videocatcher-found";
  const headChars = 2048; // a manifest shows itself in its first lines
  const maxChars = 512 * 1024; // no list of a stream is longer
  const againAfterMs = 15000;
  const told = new Map(); // link -> when it was last reported

  // inspect says what a text is: { kind, master, duration } or null.
  // "master" is an HLS list of qualities (the one worth downloading);
  // "duration" the length in seconds of a single quality that has an end.
  function inspect(text) {
    const head = text.slice(0, headChars);
    if (/^\uFEFF?\s*#EXTM3U/.test(head)) {
      const master = /#EXT-X-STREAM-INF/.test(text);
      let duration = 0;
      if (!master && /#EXT-X-ENDLIST/.test(text)) {
        for (const m of text.matchAll(/#EXTINF:\s*([\d.]+)/g)) duration += Number(m[1]) || 0;
      }
      return { kind: "hls", master, duration: Math.round(duration) };
    }
    if (/<MPD[\s>]/.test(head)) return { kind: "dash" };
    if (/<SmoothStreamingMedia[\s>]/.test(head)) return { kind: "ism" };
    return null;
  }

  // worthReading says, from the first characters, whether more of an answer
  // has to be read: an HLS list to its end (for the duration), XML until the
  // manifest's first element must have come.
  function worthReading(text) {
    if (/^\uFEFF?\s*#EXTM3U/.test(text)) return true;
    if (!/^\uFEFF?\s*</.test(text)) return false;
    return text.length < headChars;
  }

  function tell(detail) {
    document.dispatchEvent(new CustomEvent(eventName, { detail: JSON.stringify(detail) }));
  }

  function report(url, text) {
    const found = inspect(text);
    if (!found) return;
    let link;
    try {
      link = new URL(url, location.href).href;
    } catch (e) {
      return;
    }
    // A list the page built itself and keeps in memory has no link a
    // downloader could open; the popup says so.
    if (!/^https?:/i.test(link)) {
      tell({ blob: true });
      return;
    }
    const now = Date.now();
    if (now - (told.get(link) || 0) < againAfterMs) return;
    if (told.size > 500) told.clear();
    told.set(link, now);
    tell({ url: link, kind: found.kind, master: found.master, duration: found.duration });
  }

  // --- fetch ---

  async function readStart(response) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let text = "";
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (value) text += decoder.decode(value, { stream: true });
        if (done || text.length > maxChars) break;
        if (text.length >= 16 && !worthReading(text)) break;
      }
    } finally {
      reader.cancel().catch(() => {}); // already at its end is fine
    }
    return text;
  }

  function onResponse(response) {
    // The copy must be taken now, before the page reads the answer.
    let copy;
    try {
      if (!response || !response.ok || response.type === "opaque" || !response.body || response.bodyUsed) return;
      const type = (response.headers.get("content-type") || "").toLowerCase();
      if (/^(image|video|audio|font)\//.test(type) && !/mpegurl|dash/.test(type)) return;
      copy = response.clone();
    } catch (e) {
      return;
    }
    const url = response.url;
    readStart(copy).then((text) => report(url, text)).catch(() => {}); // an answer cut off: nothing found
  }

  const pageFetch = window.fetch;
  if (typeof pageFetch === "function") {
    window.fetch = function fetch(...args) {
      const promise = pageFetch.apply(this, args);
      try {
        promise.then(onResponse).catch(() => {}); // a failed request is the page's business
      } catch (e) {
        // Not a promise (a page that replaced fetch itself): left alone.
      }
      return promise;
    };
  }

  // --- XMLHttpRequest ---

  function onLoad() {
    try {
      if (this.status < 200 || this.status >= 300) return;
      const url = this.responseURL;
      const type = this.responseType;
      if (type === "" || type === "text") {
        report(url, this.responseText || "");
      } else if (type === "arraybuffer" && this.response) {
        const bytes = new Uint8Array(this.response, 0, Math.min(this.response.byteLength, maxChars));
        const head = new TextDecoder().decode(bytes.subarray(0, headChars));
        if (worthReading(head) || inspect(head)) report(url, new TextDecoder().decode(bytes));
      } else if (type === "blob" && this.response) {
        this.response.slice(0, maxChars).text().then((text) => report(url, text)).catch(() => {}); // unreadable: nothing found
      }
    } catch (e) {
      // An answer that cannot be read as text is no manifest.
    }
  }

  const Xhr = window.XMLHttpRequest;
  if (Xhr && Xhr.prototype && typeof Xhr.prototype.open === "function") {
    const pageOpen = Xhr.prototype.open;
    Xhr.prototype.open = function open(...args) {
      try {
        this.addEventListener("load", onLoad);
      } catch (e) {
        // Not a real request object: the page's own call decides.
      }
      return pageOpen.apply(this, args);
    };
  }
})();
