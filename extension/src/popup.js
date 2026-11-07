// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// The window behind the toolbar button: send the page, or one of the videos
// background.js found on it. Everything from a page (links, titles) goes in
// as text (textContent), never as markup.
"use strict";

const { api, sendToApp, cookiesFor, buildMessage, hostOf, shortPath, formatSize } = globalThis.vc;

const msg = (key, ...subs) => api.i18n.getMessage(key, subs.map(String)) || key;
const $ = (id) => document.getElementById(id);

const popup = {
  tab: null,

  texts() {
    for (const node of document.querySelectorAll("[data-i18n]")) node.textContent = msg(node.dataset.i18n);
  },

  async currentTab() {
    const [tab] = await api.tabs.query({ active: true, currentWindow: true });
    return tab || null;
  },

  // Firefox gives site access in Manifest V3 only when the user allows it;
  // without it nothing can be seen.
  async checkAccess() {
    const ok = await api.permissions.contains({ origins: ["<all_urls>"] });
    $("need-access").hidden = ok;
  },

  async checkConnection() {
    const reply = await sendToApp({ type: "ping" });
    $("not-connected").hidden = reply.ok;
    $("status").textContent = reply.ok ? msg("connected", reply.version || "") : "";
    return reply.ok;
  },

  // explain turns a reply of the helper into a sentence.
  explain(reply) {
    switch (reply.code) {
      case "not-connected": return msg("notConnected");
      case "bad-link": return msg("errBadLink");
      case "start": return msg("errStart");
    }
    return msg("errOther", reply.error || reply.code || "?");
  },

  showResult(el, reply) {
    el.hidden = false;
    el.className = "result " + (reply.ok ? "ok" : "bad");
    el.textContent = reply.ok ? msg("sent") : this.explain(reply);
    if (reply.code === "not-connected") $("not-connected").hidden = false;
  },

  async send(item, button, resultEl) {
    if (!this.tab) return;
    button.disabled = true;
    resultEl.hidden = false;
    resultEl.className = "result";
    resultEl.textContent = msg("sending");
    const m = buildMessage(this.tab, item);
    if ($("with-cookies").checked) m.cookies = await cookiesFor([m.url, this.tab.url]);
    const reply = await sendToApp(m);
    this.showResult(resultEl, reply);
    button.disabled = reply.ok;
  },

  kindLabel(kind) {
    return { hls: msg("kindHls"), dash: msg("kindDash"), video: msg("kindVideo"), audio: msg("kindAudio") }[kind] || kind;
  },

  itemRow(item) {
    const li = document.createElement("li");
    li.className = "item";
    const what = document.createElement("span");
    what.className = "what";
    const kind = document.createElement("span");
    kind.className = "kind";
    kind.textContent = item.kind.toUpperCase();
    kind.title = this.kindLabel(item.kind);
    // A size only for files: for a stream it is the size of its list.
    const size = item.kind === "video" || item.kind === "audio" ? formatSize(item.size) : "";
    what.append(kind, document.createTextNode([hostOf(item.url), size].filter(Boolean).join(", ")));
    const where = document.createElement("span");
    where.className = "where";
    where.textContent = [shortPath(item.url), item.frameUrl ? msg("inFrame", hostOf(item.frameUrl)) : ""].filter(Boolean).join("  ");
    where.title = item.url;
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = msg("send");
    const result = document.createElement("p");
    result.hidden = true;
    button.addEventListener("click", () => this.send(item, button, result));
    li.append(what, button, where, result);
    return li;
  },

  async showItems() {
    const list = $("items");
    list.textContent = "";
    if (!this.tab) return;
    const got = await api.storage.session.get("tab:" + this.tab.id);
    const items = (got["tab:" + this.tab.id] || { items: [] }).items;
    $("nothing").hidden = items.length > 0;
    for (const item of items) list.append(this.itemRow(item));
  },

  async start() {
    this.texts();
    this.tab = await this.currentTab();
    const settings = await api.storage.local.get("withCookies");
    $("with-cookies").checked = !!settings.withCookies;
    $("with-cookies").addEventListener("change", (e) => api.storage.local.set({ withCookies: e.target.checked }));
    $("send-page").disabled = !this.tab || !/^https?:/i.test(this.tab.url || "");
    $("send-page").addEventListener("click", () => this.send(null, $("send-page"), $("page-result")));
    $("allow-sites").addEventListener("click", async () => {
      // Must be called right in the click, or the browser refuses to ask.
      const ok = await api.permissions.request({ origins: ["<all_urls>"] });
      $("need-access").hidden = ok;
    });
    $("clear").addEventListener("click", async () => {
      if (!this.tab) return;
      await api.storage.session.remove("tab:" + this.tab.id);
      api.action.setBadgeText({ tabId: this.tab.id, text: "" }).catch(() => {}); // the tab may be gone
      this.showItems();
    });
    api.storage.onChanged.addListener((changes, area) => {
      if (area === "session" && this.tab && ("tab:" + this.tab.id) in changes) this.showItems();
    });
    await Promise.all([this.showItems(), this.checkAccess(), this.checkConnection()]);
  },
};

globalThis.vcPopup = popup;
document.addEventListener("DOMContentLoaded", () => popup.start());
