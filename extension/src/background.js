// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Watches what the pages load and keeps, per tab, the videos it saw: HLS
// and DASH streams and video files, also those inside embedded players
// (iframes), which a page link alone would not reveal. What the requests
// do not show comes from the pages themselves: page.js reads the answers a
// player gets (a stream behind a link and a type that say nothing),
// content.js the videos the page names. Nothing is sent
// anywhere until the user clicks: the list stays in the browser's session
// storage and is gone when the browser closes.
//
// Chrome and Edge run this as a service worker (common.js comes with
// importScripts); Firefox as a background page with both scripts listed in
// the manifest. A service worker may be stopped at any time, so the lists
// live in storage.session, not in variables.
"use strict";

if (typeof globalThis.vc === "undefined" && typeof importScripts === "function") {
  importScripts("common.js");
}
const { api, classify, addItem, pageItems, sendToApp, withLogin, withPass, buildMessage, totalSize, header, sentHeaders,
  traceLine, pageNotes, reportText, maxTrace, maxNotes } = globalThis.vc;

const tabKey = (tabId) => "tab:" + tabId;

// What a request was sent with, by request id, until its answer comes: the
// Referer, and the headers a site may want to see again with the download
// (sentHeaders in common.js). Lost when the worker stops, which only costs
// the headers of a request that was running at that moment (the frame's
// address is the referer then).
const sent = new Map();

// For "Copy a report" in the popup: the last requests of each tab,
// shortened (traceLine), and what page.js noted about the player. Only in
// memory, never stored and never sent: gone when the worker stops, the tab
// closes or goes to another page.
const traces = new Map();
function traceOf(tabId) {
  if (!traces.has(tabId)) traces.set(tabId, { requests: [], notes: [] });
  return traces.get(tabId);
}

// Writes to storage are queued per tab: two answers arriving at the same
// time must not overwrite each other's item.
const queues = new Map();
function withTab(tabId, fn) {
  const next = (queues.get(tabId) || Promise.resolve()).then(fn, fn);
  queues.set(tabId, next.catch(() => {}));
  return next;
}

async function readTab(tabId) {
  const got = await api.storage.session.get(tabKey(tabId));
  return got[tabKey(tabId)] || { items: [] };
}

function showCount(tabId, n) {
  const text = n > 0 ? String(Math.min(n, 99)) : "";
  api.action.setBadgeText({ tabId, text }).catch(() => {}); // the tab may be gone
  api.action.setBadgeBackgroundColor({ tabId, color: "#f2b705" }).catch(() => {});
  if (api.action.setBadgeTextColor) api.action.setBadgeTextColor({ tabId, color: "#0f2a33" }).catch(() => {});
}

function remember(details, kind, was) {
  const referer = was && was.referer;
  // Firefox names the frame's page (documentUrl); Chrome only its origin
  // (initiator). The Referer header, where there is one, says it best.
  const frameUrl = details.frameId > 0 ? details.documentUrl || details.initiator || "" : "";
  const item = {
    url: details.url,
    kind,
    via: "net",
    size: totalSize(details.responseHeaders),
    referer: referer || details.documentUrl || "",
    headers: (was && was.headers) || {},
    frameUrl,
    time: Date.now(),
  };
  return withTab(details.tabId, async () => {
    const tab = await readTab(details.tabId);
    tab.items = addItem(tab.items, item);
    await api.storage.session.set({ [tabKey(details.tabId)]: tab });
    showCount(details.tabId, tab.items.length);
  });
}

function onSendHeaders(details) {
  if (details.tabId < 0) return; // not from a tab (the extension itself, the browser)
  if (sent.size > 2000) sent.clear(); // answers that never came
  sent.set(details.requestId, { referer: header(details.requestHeaders, "referer"), headers: sentHeaders(details.requestHeaders) });
}

function onHeadersReceived(details) {
  const was = sent.get(details.requestId);
  sent.delete(details.requestId);
  if (details.tabId < 0) return;
  if (details.type !== "stylesheet" && details.type !== "font") {
    const t = traceOf(details.tabId);
    t.requests.push(traceLine(details));
    if (t.requests.length > maxTrace) t.requests.shift();
  }
  const kind = classify(details.url, details.type, details.responseHeaders);
  if (kind) remember(details, kind, was);
}

// What a page reports about itself (content.js): streams found in the
// answers, videos named in the markup, a video playing from memory (blob),
// a player in a frame.
function onPageMessage(message, sender, sendResponse) {
  if (!message || sender.id !== api.runtime.id) return;
  if (message.type === "vc-report" && !sender.tab && Number.isInteger(message.tabId)) {
    // The popup asks for the report of its tab.
    readTab(message.tabId).then((tab) => {
      const t = traces.get(message.tabId) || { requests: [], notes: [] };
      sendResponse({ text: reportText({ version: api.runtime.getManifest().version, browser: globalThis.navigator && globalThis.navigator.userAgent,
        pageUrl: message.pageUrl, blob: tab.blob === true, items: tab.items, notes: t.notes, requests: t.requests }) });
    }, () => sendResponse({ text: "" }));
    return true; // the answer comes later
  }
  if (message.type !== "vc-found" || !sender.tab) return;
  const tabId = sender.tab.id;
  for (const n of pageNotes(message)) {
    const t = traceOf(tabId);
    if (t.notes.length < maxNotes && !t.notes.includes(n)) t.notes.push(n);
  }
  const items = pageItems(message, { url: sender.url || "", frameId: sender.frameId || 0 }, Date.now());
  const blob = message.blob === true;
  if (items.length === 0 && !blob) return;
  withTab(tabId, async () => {
    const tab = await readTab(tabId);
    for (const item of items) tab.items = addItem(tab.items, item);
    if (blob) tab.blob = true;
    await api.storage.session.set({ [tabKey(tabId)]: tab });
    showCount(tabId, tab.items.length);
  });
}
api.runtime.onMessage.addListener(onPageMessage);

// A new page in the tab starts a new list.
function onBeforeRequest(details) {
  if (details.type !== "main_frame" || details.tabId < 0) return;
  traces.delete(details.tabId);
  withTab(details.tabId, async () => {
    await api.storage.session.remove(tabKey(details.tabId));
    showCount(details.tabId, 0);
  });
}

const filter = { urls: ["<all_urls>"] };
// Chrome shows the Referer, Origin and Authorization to extensions only
// with "extraHeaders"; Firefox
// does not know the option and refuses the listener with it.
try {
  api.webRequest.onSendHeaders.addListener(onSendHeaders, filter, ["requestHeaders", "extraHeaders"]);
} catch (e) {
  api.webRequest.onSendHeaders.addListener(onSendHeaders, filter, ["requestHeaders"]);
}
api.webRequest.onHeadersReceived.addListener(onHeadersReceived, filter, ["responseHeaders"]);
api.webRequest.onBeforeRequest.addListener(onBeforeRequest, { urls: ["<all_urls>"], types: ["main_frame"] });

api.tabs.onRemoved.addListener((tabId) => {
  traces.delete(tabId);
  api.storage.session.remove(tabKey(tabId)).catch(() => {}); // already gone is fine
});

// --- right-click menu ---

api.runtime.onInstalled.addListener(() => {
  api.contextMenus.removeAll(() => {
    const m = (id, contexts, key) => api.contextMenus.create({ id, contexts, title: api.i18n.getMessage(key) });
    m("vc-page", ["page"], "menuPage");
    m("vc-link", ["link"], "menuLink");
    m("vc-media", ["video", "audio"], "menuMedia");
  });
});

api.contextMenus.onClicked.addListener(async (info, tab) => {
  if (!tab) return;
  let msg = buildMessage(tab, null);
  if (info.menuItemId === "vc-link" && info.linkUrl) {
    msg = { type: "add", url: info.linkUrl, pageUrl: tab.url, title: "", referer: tab.url, fileName: "", headers: {} };
  } else if (info.menuItemId === "vc-media" && /^https?:/i.test(info.srcUrl || "")) {
    msg = buildMessage(tab, { url: info.srcUrl, referer: info.frameUrl || tab.url });
  }
  // A blob: video (the page builds it from a stream) has no link of its
  // own; the page goes instead, and the popup's list has the stream.
  const settings = await api.storage.local.get("withCookies");
  if (settings.withCookies) await withLogin(msg, tab, { frameUrl: info.frameUrl });
  else await withPass(msg, tab, { frameUrl: info.frameUrl });
  const reply = await sendToApp(msg);
  if (!reply.ok) {
    showCount(tab.id, 0);
    api.action.setBadgeText({ tabId: tab.id, text: "!" }).catch(() => {});
    api.action.setBadgeBackgroundColor({ tabId: tab.id, color: "#b3261e" }).catch(() => {});
  }
});
