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
// document), nothing of its content, and makes no request of its own. For
// the report a user can copy when nothing is found, it also notes in a few
// words how the player gets its video (a list built in memory, a worker). The
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

  // --- putting it in place ---

  // A player can ask for its video with the fetch of another window: a
  // frame of its own without an address, made only for that. So the same
  // goes into every window of this page that can be reached, at the moment
  // the page reaches for it (watchFrames), before it can use what is in it.
  let hooked;
  try {
    hooked = new WeakSet();
  } catch (e) {
    hooked = { has: () => false, add: () => {} };
  }

  function hookFetch(win) {
    const pageFetch = win.fetch;
    if (typeof pageFetch !== "function") return;
    win.fetch = function fetch(...args) {
      const promise = pageFetch.apply(this, args);
      try {
        promise.then(onResponse).catch(() => {}); // a failed request is the page's business
      } catch (e) {
        // Not a promise (a page that replaced fetch itself): left alone.
      }
      return promise;
    };
  }

  function hookXhr(win) {
    const Xhr = win.XMLHttpRequest;
    if (!Xhr || !Xhr.prototype || typeof Xhr.prototype.open !== "function") return;
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

  // --- notes for the report ---

  // When nothing is found, the user can copy a report (popup). These notes
  // say how the player gets its video where the requests do not show it: a
  // list it builds in memory, a worker that does the asking. Short words
  // about the page, never a link with its keys and nothing of an answer.
  const noted = new Set();
  function note(text) {
    if (noted.size >= 30 || noted.has(text)) return;
    noted.add(text);
    tell({ note: text.replace(/[^\x20-\x7e]/g, "?").slice(0, 200) });
  }

  // where says of a link only its kind, its host and how its name ends.
  function where(link) {
    try {
      const u = new URL(link, location.href);
      if (!/^https?:$/.test(u.protocol)) return u.protocol;
      const end = /\.[A-Za-z0-9]{1,6}$/.exec(u.pathname);
      return u.host + " " + (end ? end[0] : "(no ending)") + (u.search ? " ?" : "");
    } catch (e) {
      return "(no link)";
    }
  }

  // fromData reads a list that is written into a link itself ("data:"),
  // which is how a list built in memory names its qualities.
  function fromData(link) {
    const m = /^data:([^,]*),([\s\S]*)$/i.exec(link);
    if (!m || m[2].length > maxChars * 2) return null;
    try {
      return /;base64/i.test(m[1]) ? atob(m[2]) : decodeURIComponent(m[2]);
    } catch (e) {
      return null;
    }
  }

  // describe says what a list built in memory looks like: how many parts,
  // where the first one points, and how the parts are locked (the method
  // and where the key comes from, never the key or its link). Of a list of
  // qualities that are lists in memory too, the first one is described.
  function describe(text, found, inner) {
    if (found.kind !== "hls") return found.kind;
    const lines = text.split("\n").map((l) => l.trim()).filter((l) => l && !l.startsWith("#"));
    const key = /#EXT-X-KEY:[^\n]*METHOD=([A-Z0-9-]+)/.exec(text);
    const keyAt = /#EXT-X-KEY:[^\n]*URI="([^"]*)"/.exec(text);
    const iv = /#EXT-X-KEY:[^\n]*IV=/.test(text);
    let out = "hls " + (found.master ? "master" : "one quality, " + found.duration + " s") + ", " + lines.length + " entries, first: " +
      (lines.length > 0 ? (/^[a-z][a-z0-9+.-]*:/i.test(lines[0]) ? "" : "relative ") + where(lines[0]) : "none") +
      (key ? ", key " + key[1] + (keyAt ? " at " + where(keyAt[1]) : "") + (iv ? ", iv" : "") : found.master ? "" : ", no key");
    if (/#EXT-X-MAP:/.test(text)) out += ", map";
    if (found.master && !inner && lines.length > 0) {
      const first = fromData(lines[0]);
      const sub = first && inspect(first);
      if (sub) out += " -> " + describe(first, sub, true);
    }
    return out;
  }

  function hookNotes(win) {
    const urls = win.URL;
    if (urls && typeof urls.createObjectURL === "function") {
      const pageCreate = urls.createObjectURL;
      urls.createObjectURL = function createObjectURL(...args) {
        try {
          const b = args[0];
          if (b && typeof b.slice === "function" && typeof b.size === "number") {
            if (b.size > 0 && b.size <= maxChars && !/^(image|video|audio|font)\//.test(b.type || "")) {
              b.slice(0, maxChars).text().then((text) => {
                const found = inspect(text);
                if (!found) return;
                tell({ blob: true });
                note("list built in memory: " + describe(text, found));
              }).catch(() => {}); // unreadable: nothing to note
            }
          } else if (b) {
            note("video fed from memory (MediaSource)");
          }
        } catch (e) {
          // Whatever the page passed: its own call decides.
        }
        return pageCreate.apply(this, args);
      };
    }
    for (const name of ["Worker", "SharedWorker"]) {
      const Page = win[name];
      if (typeof Page !== "function" || typeof Proxy !== "function") continue;
      win[name] = new Proxy(Page, {
        construct(target, args, newTarget) {
          try {
            note(name + " started: " + where(String(args[0])));
          } catch (e) {
            // A worker without a readable address.
          }
          return Reflect.construct(target, args, newTarget);
        },
      });
    }
  }

  // watchFrames makes the page's way to another window (a frame's
  // contentWindow or contentDocument) pass through install first.
  function watchFrames(win) {
    for (const name of ["HTMLIFrameElement", "HTMLFrameElement", "HTMLObjectElement", "HTMLEmbedElement"]) {
      const proto = win[name] && win[name].prototype;
      if (!proto) continue;
      for (const prop of ["contentWindow", "contentDocument"]) {
        const d = Object.getOwnPropertyDescriptor(proto, prop);
        if (!d || typeof d.get !== "function" || d.configurable === false) continue;
        Object.defineProperty(proto, prop, {
          ...d,
          get() {
            const got = d.get.call(this);
            try {
              install(prop === "contentWindow" ? got : got && got.defaultView);
            } catch (e) {
              // A frame of another site: its own copy of this script watches it.
            }
            return got;
          },
        });
      }
    }
  }

  function install(win) {
    if (!win || hooked.has(win)) return;
    // A window of another site throws here, before anything is changed.
    void win.fetch;
    hooked.add(win);
    for (const part of [hookFetch, hookXhr, hookNotes, watchFrames]) {
      try {
        part(win);
      } catch (e) {
        // A page that locked one of them: the others still go in.
      }
    }
  }

  install(window);
})();
