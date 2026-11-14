// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// The window behind the toolbar button: send the page, or one of the videos
// background.js found on it. Everything from a page (links, titles) goes in
// as text (textContent), never as markup.
"use strict";

const { api, sendToApp, cookiesFor, buildMessage, hostOf, shortPath, formatSize, formatDuration } = globalThis.vc;

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
    return {
      hls: msg("kindHls"), dash: msg("kindDash"), ism: msg("kindIsm"), video: msg("kindVideo"), audio: msg("kindAudio"),
      embed: msg("kindEmbed"),
    }[kind] || kind;
  },

  // details says what is known about a found video, after its host: for an
  // HLS stream whether it holds all qualities or one, how long it is, and
  // for a file its size (for a stream that would be the size of its list).
  details(item) {
    const parts = [hostOf(item.url)];
    if (item.kind === "hls" && item.master === true) parts.push(msg("allQualities"));
    if (item.kind === "hls" && item.master === false) parts.push(msg("oneQuality"));
    parts.push(formatDuration(item.duration));
    if (item.kind === "video" || item.kind === "audio") parts.push(formatSize(item.size));
    return parts.filter(Boolean).join(", ");
  },

  itemRow(item) {
    const li = document.createElement("li");
    li.className = "item";
    const what = document.createElement("span");
    what.className = "what";
    const kind = document.createElement("span");
    kind.className = "kind";
    kind.textContent = item.kind === "embed" ? "PLAYER" : item.kind.toUpperCase();
    kind.title = this.kindLabel(item.kind);
    what.append(kind, document.createTextNode(this.details(item)));
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
    const tab = got["tab:" + this.tab.id] || { items: [] };
    const items = tab.items || [];
    // A video that plays from memory and no stream to it: say so instead of
    // "nothing found", the page itself or a listed player may still work.
    const hidden = tab.blob === true && !items.some((x) => x.kind !== "embed");
    $("hidden-video").hidden = !hidden;
    $("nothing").hidden = items.length > 0 || hidden;
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
