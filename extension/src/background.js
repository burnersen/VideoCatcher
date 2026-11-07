// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Watches what the pages load and keeps, per tab, the videos it saw: HLS
// and DASH streams and video files, also those inside embedded players
// (iframes), which a page link alone would not reveal. Nothing is sent
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
const { api, classify, addItem, sendToApp, cookiesFor, buildMessage, totalSize, header } = globalThis.vc;

const tabKey = (tabId) => "tab:" + tabId;

// The Referer a request was sent with, by request id, until its answer
// comes. Lost when the worker stops, which only costs the referer of a
// request that was running at that moment (the frame's address is used
// then).
const referers = new Map();

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

function remember(details, kind, referer) {
  // Firefox names the frame's page (documentUrl); Chrome only its origin
  // (initiator). The Referer header, where there is one, says it best.
  const frameUrl = details.frameId > 0 ? details.documentUrl || details.initiator || "" : "";
  const item = {
    url: details.url,
    kind,
    size: totalSize(details.responseHeaders),
    referer: referer || details.documentUrl || "",
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
  const ref = header(details.requestHeaders, "referer");
  if (!ref) return;
  if (referers.size > 2000) referers.clear(); // answers that never came
  referers.set(details.requestId, ref);
}

function onHeadersReceived(details) {
  const ref = referers.get(details.requestId);
  referers.delete(details.requestId);
  if (details.tabId < 0) return; // not from a tab (the extension itself, the browser)
  const kind = classify(details.url, details.type, details.responseHeaders);
  if (kind) remember(details, kind, ref);
}

// A new page in the tab starts a new list.
function onBeforeRequest(details) {
  if (details.type !== "main_frame" || details.tabId < 0) return;
  withTab(details.tabId, async () => {
    await api.storage.session.remove(tabKey(details.tabId));
    showCount(details.tabId, 0);
  });
}

const filter = { urls: ["<all_urls>"] };
// Chrome shows the Referer to extensions only with "extraHeaders"; Firefox
// does not know the option and refuses the listener with it.
try {
  api.webRequest.onSendHeaders.addListener(onSendHeaders, filter, ["requestHeaders", "extraHeaders"]);
} catch (e) {
  api.webRequest.onSendHeaders.addListener(onSendHeaders, filter, ["requestHeaders"]);
}
api.webRequest.onHeadersReceived.addListener(onHeadersReceived, filter, ["responseHeaders"]);
api.webRequest.onBeforeRequest.addListener(onBeforeRequest, { urls: ["<all_urls>"], types: ["main_frame"] });

api.tabs.onRemoved.addListener((tabId) => {
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
    msg = { type: "add", url: info.linkUrl, pageUrl: tab.url, title: "", referer: tab.url, fileName: "" };
  } else if (info.menuItemId === "vc-media" && /^https?:/i.test(info.srcUrl || "")) {
    msg = buildMessage(tab, { url: info.srcUrl, referer: info.frameUrl || tab.url });
  }
  // A blob: video (the page builds it from a stream) has no link of its
  // own; the page goes instead, and the popup's list has the stream.
  const settings = await api.storage.local.get("withCookies");
  if (settings.withCookies) msg.cookies = await cookiesFor([msg.url, tab.url]);
  const reply = await sendToApp(msg);
  if (!reply.ok) {
    showCount(tab.id, 0);
    api.action.setBadgeText({ tabId: tab.id, text: "!" }).catch(() => {});
    api.action.setBadgeBackgroundColor({ tabId: tab.id, color: "#b3261e" }).catch(() => {});
  }
});
