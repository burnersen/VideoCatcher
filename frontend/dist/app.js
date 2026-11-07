// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// The logic of the page. Plain JavaScript: no framework, no npm, no build
// step. Go does the work (app.go); this file asks Go and shows what Go
// reports. Texts come from i18n.js. Checked without a browser by
// ../app_check.js.
//
// Everything that comes from a web site (titles, names, links) is put into
// the page with esc(): a title is text, never markup. The page can call Go,
// so a title that turned into a script could do anything VideoCatcher can.
"use strict";

// t (the text of a key) comes from i18n.js, which index.html loads first.
/* global t */

function esc(s) {
  return String(s === undefined || s === null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

// formatSize turns bytes into "1.5 MB".
function formatSize(bytes) {
  if (!bytes || bytes < 0) return "";
  if (bytes < 1024) return bytes + " bytes";
  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return value.toFixed(value < 10 ? 1 : 0) + " " + units[unit];
}

// formatDuration turns seconds into "3:07" or "1:02:03".
function formatDuration(sec) {
  sec = Math.round(Number(sec) || 0);
  if (sec <= 0) return "";
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = String(sec % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`;
}

function hostOf(link) {
  const m = /^https?:\/\/([^/?#]+)/i.exec(String(link || ""));
  return m ? m[1].replace(/^www\./, "") : "";
}

// clipLong shortens every very long word (in practice: a link) to its
// start and end, so a 900-character link fits in a line of the list or the
// log. The full link stays in the row's tooltip.
function clipLong(text) {
  return String(text === undefined || text === null ? "" : text)
    .replace(/\S{120,}/g, (w) => w.slice(0, 70) + "\u2026" + w.slice(-35));
}

// linksIn finds the web links in dropped text (a link, a list of links, a
// .txt file, an Internet shortcut "URL=..."). Each link once, in order.
const maxDroppedLinks = 200;
function linksIn(text) {
  const seen = new Set();
  for (const m of String(text || "").matchAll(/https?:\/\/[^\s"'<>]+/gi)) {
    const link = m[0].replace(/[),.;]+$/, "");
    if (!seen.has(link)) seen.add(link);
    if (seen.size >= maxDroppedLinks) break;
  }
  return [...seen];
}

// Only https pictures are shown; anything else stays empty.
function safeImage(link) {
  return /^https:\/\/[^\s"'<>]+$/i.test(String(link || "")) ? link : "";
}

const presets = [
  { key: "fn.titleId", value: "%(title)s [%(id)s].%(ext)s" },
  { key: "fn.title", value: "%(title)s.%(ext)s" },
  { key: "fn.uploader", value: "%(uploader)s/%(title)s.%(ext)s" },
  { key: "fn.playlist", value: "%(playlist)s/%(playlist_index)03d %(title)s.%(ext)s" },
];

const maxLogLines = 300;

// baseName is the file name of a path without folder and ending:
// "D:\\V\\Holiday [abc].mp4" -> "Holiday [abc]".
function baseName(p) {
  const name = String(p || "").split(/[\\/]/).pop();
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(0, dot) : name;
}

// clock is the time of day for lines the page adds to the log itself.
function clock(d) {
  d = d || new Date();
  return [d.getHours(), d.getMinutes(), d.getSeconds()].map((n) => String(n).padStart(2, "0")).join(":");
}

const ui = {
  init: null, // what Initial() returned
  info: null, // what is behind the last looked-up link
  infoLink: "", // the link as it was typed when it was looked up
  tools: [],
  browserLinks: false,
  browserExt: false,
  formatId: "", // a format chosen by hand, or ""
  jobs: new Map(),
  logLines: [],
  previewTimer: null,
  autoName: "", // the name the pattern gives the looked-up video
  ownNameEdited: false, // the user typed a name of their own
  jobShapes: new Map(), // id -> what a row looks like apart from the numbers (see upsertJob)
  speed: null, // SpeedState from Go
  speedDragging: false, // the slider is being moved: Go's numbers must not jump it back
  speedTimer: null,
  afterAll: null, // AfterAllState from Go
  afterLeft: 0, // seconds of the countdown shown
  afterRun: 0, // number of the running countdown; a new state stops the old one

  backend() {
    return (window.go && window.go.main && window.go.main.App) || null;
  },

  el(id) {
    return document.getElementById(id);
  },

  // showError shows an error from Go: first line is the message, the rest
  // is the hint (see uiError in errors.go). Never an empty box, never only
  // a console.log: an error nobody sees did not get reported.
  showError(err, box) {
    box = box || "error";
    const text = String(err && err.message ? err.message : err || "Unknown error");
    const lines = text.split("\n");
    this.el(box + "-msg").textContent = lines[0];
    this.el(box + "-hint").textContent = lines.slice(1).join(" ");
    this.el(box).hidden = false;
  },

  clearError(box) {
    box = box || "error";
    this.el(box).hidden = true;
    this.el(box + "-msg").textContent = "";
    this.el(box + "-hint").textContent = "";
  },

  applyTexts() {
    for (const node of document.querySelectorAll("[data-i18n]")) node.textContent = t(node.dataset.i18n);
    for (const node of document.querySelectorAll("[data-i18n-ph]")) node.placeholder = t(node.dataset.i18nPh);
  },

  fillSelect(id, values, label) {
    this.el(id).innerHTML = values.map((v) => `<option value="${esc(v)}">${esc(label(v))}</option>`).join("");
  },

  fillSelects() {
    const c = this.init.choices;
    this.fillQuality([]);
    this.fillSelect("container", c.container, (v) => (v === "auto" ? t("c.auto") : v.toUpperCase()));
    this.fillSelect("audio-format", c.audioFormat, (v) => (v === "best" ? t("a.best") : v.toUpperCase()));
    this.fillSelect("audio-quality", c.audioQuality, (v) => (v === "best" ? t("aq.best") : v + " kbit/s"));
    this.fillSelect("sub-mode", c.subMode, (v) => t("s." + v));
    this.fillSelect("playlist", c.playlist, (v) => t("p." + v));
    this.fillSelect("sponsor", c.sponsorBlock, (v) => t("sb." + v));
    this.fillSelect("cookies", c.cookies, (v) => (v === "none" ? t("ck.none") : v.charAt(0).toUpperCase() + v.slice(1)));
    this.el("template-preset").innerHTML = presets.map((p) => `<option value="${esc(p.value)}">${esc(t(p.key))}</option>`).join("") +
      `<option value="">${esc(t("fn.custom"))}</option>`;
    this.fillSelect("set-language", ["auto", "en", "de"], (v) => (v === "auto" ? t("set.lang.auto") : v === "de" ? "Deutsch" : "English"));
    this.fillSelect("set-close-button", c.closeButton || [], (v) => t("set.close." + v));
    this.fillSelect("after-all", c.afterAll || ["none"], (v) => t("after." + v));
  },

  // fillQuality marks the heights the looked-up video really has.
  fillQuality(heights) {
    const keep = this.el("quality").value;
    this.fillSelect("quality", this.init.choices.quality, (v) => {
      const name = t("q." + v);
      return heights.includes(Number(v)) ? t("q.available", { q: name }) : name;
    });
    if (keep) this.el("quality").value = keep;
  },

  setOptions(o) {
    const v = (id, x) => { this.el(id).value = x; };
    const c = (id, x) => { this.el(id).checked = !!x; };
    v("quality", o.quality); v("container", o.container); v("audio-format", o.audioFormat);
    v("audio-quality", o.audioQuality); v("sub-mode", o.subMode); v("sub-langs", o.subLangs);
    c("auto-subs", o.autoSubs); v("playlist", o.playlist); v("items", o.playlistItems || "");
    v("folder", o.outputDir); v("template", o.template); v("sponsor", o.sponsorBlock);
    v("cookies", o.cookies); v("rate", o.rateLimit || ""); v("fragments", o.fragments);
    c("embed-thumbnail", o.embedThumbnail); c("embed-metadata", o.embedMetadata); c("embed-chapters", o.embedChapters);
    c("clean-names", o.cleanNames);
    this.syncPreset();
    this.updateVisibility();
  },

  // options reads the form into the shape of DownloadOptions in options.go.
  options() {
    const v = (id) => this.el(id).value;
    const c = (id) => !!this.el(id).checked;
    return {
      quality: v("quality"), formatId: this.formatId, container: v("container"),
      audioFormat: v("audio-format"), audioQuality: v("audio-quality"),
      subLangs: v("sub-langs").trim(), subMode: v("sub-mode"), autoSubs: c("auto-subs"),
      outputDir: v("folder"), template: v("template").trim(), playlist: v("playlist"),
      playlistItems: v("items").trim(), embedThumbnail: c("embed-thumbnail"),
      embedMetadata: c("embed-metadata"), embedChapters: c("embed-chapters"),
      sponsorBlock: v("sponsor"), rateLimit: v("rate").trim(),
      fragments: Number(v("fragments")) || 1, cookies: v("cookies"),
      fileName: this.ownName(), cleanNames: c("clean-names"),
    };
  },

  // ownName is the name typed for this one download, or "" for the
  // pattern. It counts only while the field is shown.
  ownName() {
    if (this.el("opt-own-name").hidden || !this.ownNameEdited) return "";
    const n = this.el("own-name").value.trim();
    return n === this.autoName ? "" : n;
  },

  resetOwnName() {
    this.ownNameEdited = false;
    this.el("own-name").value = this.autoName;
    this.schedulePreview();
  },

  updateVisibility() {
    const audio = this.el("quality").value === "audio" && !this.formatId;
    this.el("opt-audio-format").hidden = !audio;
    this.el("opt-audio-quality").hidden = !audio;
    this.el("opt-container").hidden = audio;
    this.el("opt-sub-langs").hidden = this.el("sub-mode").value === "none";
    this.el("opt-items").hidden = this.el("playlist").value !== "all";
    this.el("opt-own-name").hidden = !this.info || !!this.info.isPlaylist || this.el("playlist").value === "all";
  },

  syncPreset() {
    const tpl = this.el("template").value;
    this.el("template-preset").value = presets.some((p) => p.value === tpl) ? tpl : "";
  },

  async paste() {
    const app = this.backend();
    if (!app) return;
    const link = await app.Clipboard();
    if (link) this.el("link").value = link;
  },

  async lookup() {
    const app = this.backend();
    if (!app) return this.showError(t("err.noBackend"));
    this.clearError();
    const button = this.el("lookup");
    button.disabled = true;
    button.textContent = t("link.looking");
    try {
      const o = this.options();
      this.infoLink = this.el("link").value.trim();
      const info = await app.Probe(this.infoLink, o.playlist, o.cookies);
      this.showInfo(info);
    } catch (err) {
      this.showInfo(null);
      this.showError(err);
    } finally {
      button.disabled = false;
      button.textContent = t("link.lookup");
    }
  },

  showInfo(m) {
    if (m !== this.info) {
      this.ownNameEdited = false;
      this.autoName = "";
      this.el("own-name").value = "";
    }
    this.info = m;
    this.formatId = "";
    this.el("info").hidden = !m;
    this.el("info-empty").hidden = !!m;
    if (!m) {
      this.fillQuality([]);
      this.updateVisibility();
      this.schedulePreview();
      return;
    }
    const thumb = safeImage(m.thumbnail);
    this.el("info-thumb").hidden = !thumb;
    if (thumb) this.el("info-thumb").src = thumb;
    this.el("info-title").textContent = m.title || m.url;
    const meta = [m.uploader, m.site, formatDuration(m.duration)].filter(Boolean);
    this.el("info-meta").textContent = meta.join(", ");
    let extra = "";
    if (m.isPlaylist) extra = t("info.playlist", { n: m.count });
    else if (m.live) extra = t("info.live");
    this.el("info-extra").textContent = extra;
    if (m.isPlaylist && this.el("playlist").value !== "all") {
      this.el("playlist").value = "all";
    }
    this.fillQuality(m.heights || []);
    this.renderFormats(m);
    this.renderSubChips(m);
    this.updateVisibility();
    this.schedulePreview();
  },

  renderFormats(m) {
    const box = this.el("formats-box");
    const list = m.formats || [];
    box.hidden = list.length === 0;
    const row = (f) => {
      const kind = f.hasVideo ? `${f.height ? f.height + "p" : "video"}${f.fps > 30 ? " " + Math.round(f.fps) + "fps" : ""}` : "audio";
      const codecs = [f.vcodec, f.acodec].filter(Boolean).join(" + ");
      const size = f.size ? (f.approx ? "~" : "") + formatSize(f.size) : "";
      return `<label><input type="radio" name="format" value="${esc(f.id)}" data-video="${f.hasVideo ? 1 : 0}" data-audio="${f.hasAudio ? 1 : 0}">` +
        `<span>${esc(kind)} ${esc(f.ext)} <span class="muted">${esc(codecs)} ${esc(f.note)}</span></span><span class="size">${esc(size)}</span></label>`;
    };
    this.el("formats").innerHTML = `<div class="format-list"><label><input type="radio" name="format" value="" checked><span>${esc(t("info.formatAuto"))}</span><span></span></label>` +
      list.map(row).join("") + "</div>";
  },

  // chooseFormat: a video stream without sound gets the best sound added.
  chooseFormat(id, hasVideo, hasAudio) {
    this.formatId = !id ? "" : hasVideo && !hasAudio ? `${id}+ba/${id}` : id;
    this.updateVisibility();
    this.schedulePreview();
  },

  renderSubChips(m) {
    const langs = [...new Set([...(m.subtitles || []), ...(m.autoSubs || [])])];
    const chosen = this.el("sub-langs").value.split(",").map((s) => s.trim());
    this.el("sub-chips").innerHTML = langs.map((l) =>
      `<button type="button" class="ghost chip${chosen.includes(l) ? " on" : ""}" data-lang="${esc(l)}">${esc(l)}</button>`).join("");
  },

  toggleLang(lang) {
    const list = this.el("sub-langs").value.split(",").map((s) => s.trim()).filter(Boolean);
    const i = list.indexOf(lang);
    if (i >= 0) list.splice(i, 1);
    else list.push(lang);
    this.el("sub-langs").value = list.join(",");
    if (this.info) this.renderSubChips(this.info);
  },

  schedulePreview() {
    clearTimeout(this.previewTimer);
    this.previewTimer = setTimeout(() => this.preview(), 350);
  },

  async preview() {
    const app = this.backend();
    if (!app) return;
    const info = this.info;
    try {
      const name = await app.Preview(info ? info.probeId : "", this.options());
      this.el("preview").textContent = name;
      // The field starts with the name the pattern gives, so changing a
      // word is enough. Once the user typed, the field is theirs.
      if (info && info === this.info && !this.ownNameEdited) {
        this.autoName = baseName(name);
        this.el("own-name").value = this.autoName;
      }
    } catch (err) {
      this.el("preview").textContent = String(err && err.message ? err.message : err).split("\n")[0];
    }
  },

  async add() {
    const app = this.backend();
    if (!app) return this.showError(t("err.noBackend"));
    this.clearError();
    const link = this.el("link").value.trim() || (this.info ? this.info.url : "");
    const title = this.info && this.infoLink === link ? this.info.title : "";
    const button = this.el("add");
    button.disabled = true;
    try {
      const job = await app.Add(link, this.options(), title || "");
      this.upsertJob(job);
      this.el("add-status").textContent = t("add.added", { t: job.title || hostOf(job.url) });
      this.el("link").value = "";
      this.showInfo(null);
    } catch (err) {
      this.showError(err);
    } finally {
      button.disabled = false;
    }
  },

  async chooseFolder(target) {
    const app = this.backend();
    if (!app) return;
    try {
      const dir = await app.ChooseFolder(this.el(target).value);
      if (dir) {
        this.el(target).value = dir;
        this.schedulePreview();
      }
    } catch (err) {
      this.showError(err);
    }
  },

  renderTools(list) {
    this.tools = list || [];
    this.el("tools").innerHTML = this.tools.map((s) => {
      let state = t("tools." + (s.state === "checking" ? "downloading" : s.state));
      if (s.state === "checking") state = t("tools.checking");
      if (s.state === "downloading" && s.total > 0) state += " " + Math.floor((s.done * 100) / s.total) + " %";
      const ver = s.state === "ready" ? s.version.replace(/^(FFmpeg|Deno) /, "").replace(/-.*$/, "") : "";
      const tip = [s.purpose, s.error].filter(Boolean).join("\n");
      return `<span class="tool ${esc(s.state)}" title="${esc(tip)}"><span class="dot"></span>${esc(s.tool)} ` +
        `<span class="ver">${esc(ver || state)}</span></span>`;
    }).join("");
  },

  // --- downloads ---

  jobParts(j) {
    const pct = j.status === "failed" ? "!" : j.status === "cancelled" ? "x" : Math.floor(j.percent || 0) + "%";
    // A paused job keeps its numbers: they say how far it got.
    const meta = [];
    const item = (text, cls) => meta.push(`<span${cls ? ` class="${cls}"` : ""}>${esc(text)}</span>`);
    // The stage comes from Go in English (queue.go, postStepLabel) and is
    // shown in the page's language when i18n.js knows it.
    const stage = (x) => ("st." + x in globalThis.vcTexts.en ? t("st." + x) : x);
    if (j.status === "running") {
      item(stage(j.stage));
      if (j.items > 0) item(t("job.item", { i: j.item, n: j.items }));
      if (j.total > 0) item(t("job.of", { a: formatSize(j.done), b: (j.estimated ? "~" : "") + formatSize(j.total) }), "num");
      if (j.speed > 0) item(formatSize(j.speed) + "/s", "num");
      if (j.eta > 0) item(t("job.eta", { t: formatDuration(j.eta) }), "num");
    } else {
      item(t("job." + j.status));
      if (j.status === "done" && j.stage && j.stage !== "done") item(stage(j.stage));
    }
    item(hostOf(j.url));

    const btn = (action, label, extra) => `<button type="button" class="ghost small" data-action="${action}" data-id="${esc(j.id)}"${extra || ""}>${esc(t(label))}</button>`;
    const actions = [];
    if (j.status === "running" || j.status === "waiting") actions.push(btn("pause", "job.pause"), btn("cancel", "job.stop"));
    if (j.status === "paused") actions.push(btn("resume", "job.resume"), btn("cancel", "job.stop"));
    if (j.status === "confirm") actions.push(btn("confirm", "job.yes"), btn("cancel", "job.stop"));
    if (j.status === "failed" || j.status === "cancelled") actions.push(btn("retry", "job.retry"));
    if (j.status === "done" && (j.files || []).length > 0) actions.push(btn("show", "job.show", ' data-index="0"'));
    if (j.status !== "running") actions.push(btn("remove", "job.remove"));

    let below = "";
    if (j.status === "confirm") below += `<p class="confirm-text">${esc(t("job.confirm"))} ${esc(clipLong(j.url))}</p>`;
    if (j.status === "failed" && j.error) {
      const [msg, ...hint] = String(j.error).split("\n");
      below += `<p class="error-text"><b>${esc(clipLong(msg))}</b>${esc(clipLong(hint.join(" ")))}</p>`;
    }
    if ((j.details || []).length > 0 && (j.status === "failed" || j.status === "done")) {
      below += `<details><summary>${esc(t("job.details"))}</summary><pre>${esc(j.details.join("\n"))}</pre></details>`;
    }
    const id = esc(j.id);
    const parts = {
      pct,
      title: j.title || j.url,
      meta: meta.join(""),
      width: Math.min(100, Math.max(0, Number(j.percent) || 0)) + "%",
      // Everything else: when it stays the same, an update changes only
      // the numbers (upsertJob).
      shape: [j.status, j.url, actions.join(""), below].join("\u0000"),
    };
    parts.html = `<li id="job-${id}" class="job ${esc(j.status)}">` +
      `<span id="job-${id}-pct" class="pct">${esc(pct)}</span>` +
      `<span id="job-${id}-title" class="title" title="${esc(j.url)}">${esc(parts.title)}</span>` +
      `<span class="actions">${actions.join("")}</span>` +
      `<span id="job-${id}-meta" class="meta">${parts.meta}</span>` +
      `<span class="track"><span id="job-${id}-fill" class="fill" style="width:${parts.width}"></span></span>` +
      below + "</li>";
    return parts;
  },

  jobRow(j) {
    return this.jobParts(j).html;
  },

  renderJobs() {
    const list = [...this.jobs.values()].sort((a, b) => String(b.added).localeCompare(String(a.added)));
    this.jobShapes = new Map();
    this.el("jobs").innerHTML = list.map((j) => {
      const parts = this.jobParts(j);
      this.jobShapes.set(j.id, parts.shape);
      return parts.html;
    }).join("");
    this.el("jobs-empty").hidden = list.length > 0;
    this.renderCounts();
  },

  renderCounts() {
    let r = 0;
    let w = 0;
    let p = 0;
    for (const j of this.jobs.values()) {
      if (j.status === "running") r++;
      if (j.status === "waiting" || j.status === "confirm") w++;
      if (j.status === "paused") p++;
    }
    this.el("jobs-counts").textContent = p > 0 ? t("jobs.countsPaused", { r, w, p }) : r + w > 0 ? t("jobs.counts", { r, w }) : "";
  },

  // upsertJob changes one row in place; a new job draws the whole list.
  // While only the numbers change (progress comes four times a second), the
  // row stays and only its numbers and bar change: a new row each time
  // would swallow a click on "Stop" that starts on the old row and ends on
  // the new one, and the bar could not glide.
  upsertJob(j) {
    const known = this.jobs.has(j.id);
    this.jobs.set(j.id, j);
    const row = known ? this.el("job-" + j.id) : null;
    if (!row) {
      this.renderJobs();
      return;
    }
    const parts = this.jobParts(j);
    if (this.jobShapes.get(j.id) === parts.shape) {
      this.el("job-" + j.id + "-pct").textContent = parts.pct;
      this.el("job-" + j.id + "-title").textContent = parts.title;
      this.el("job-" + j.id + "-meta").innerHTML = parts.meta;
      this.el("job-" + j.id + "-fill").style.width = parts.width;
    } else {
      row.outerHTML = parts.html;
      this.jobShapes.set(j.id, parts.shape);
    }
    this.renderCounts();
  },

  async onJobsClick(e) {
    const target = e.target && e.target.closest ? e.target.closest("[data-action]") : null;
    if (!target) return;
    const app = this.backend();
    if (!app) return;
    const id = target.dataset.id;
    try {
      switch (target.dataset.action) {
        case "cancel": await app.Cancel(id); break;
        case "pause": await app.Pause(id); break;
        case "resume": await app.Resume(id); break;
        case "retry": await app.Retry(id); break;
        case "confirm": await app.Confirm(id); break;
        case "show": await app.ShowFile(id, Number(target.dataset.index) || 0); break;
        case "remove":
          await app.Remove(id);
          this.jobs.delete(id);
          this.renderJobs();
          break;
      }
    } catch (err) {
      this.showError(err);
    }
  },

  async clearFinished() {
    const app = this.backend();
    if (!app) return;
    const jobs = await app.ClearFinished();
    this.jobs = new Map((jobs || []).map((j) => [j.id, j]));
    this.renderJobs();
  },

  // --- speed slider ---

  // renderSpeed shows what Go measured. While the slider is moved, its own
  // value counts, not the one Go sent a moment ago.
  renderSpeed(st) {
    if (!st) return;
    this.speed = st;
    const slider = this.el("speed");
    if (!this.speedDragging) slider.value = st.percent;
    slider.disabled = !st.active;
    this.el("speed-pct").textContent = (this.speedDragging ? slider.value : st.percent) + " %";
    const parts = [];
    if (!st.active) {
      parts.push(t("speed.off"));
    } else {
      if (st.rate > 0) parts.push(t("speed.now", { rate: formatSize(st.rate) }));
      if (st.limit > 0) parts.push(t("speed.limit", { limit: formatSize(st.limit) }));
      if (st.peak > 0) parts.push(t("speed.full", { peak: formatSize(st.peak) }));
      else if (st.percent < 100) parts.push(t("speed.notMeasured"));
    }
    this.el("speed-info").textContent = parts.join(", ");
  },

  // moveSpeed: while the slider moves, Go hears about it a few times a
  // second, so the downloads follow the hand.
  moveSpeed(final) {
    this.speedDragging = !final;
    this.el("speed-pct").textContent = this.el("speed").value + " %";
    if (final) {
      clearTimeout(this.speedTimer);
      this.speedTimer = null;
      return this.sendSpeed();
    } else if (!this.speedTimer) {
      this.speedTimer = setTimeout(() => { this.speedTimer = null; return this.sendSpeed(); }, 150);
    }
    return null;
  },

  async sendSpeed() {
    const app = this.backend();
    if (!app || !app.SetSpeed) return;
    try {
      this.renderSpeed(await app.SetSpeed(Number(this.el("speed").value) || 100));
    } catch (err) {
      this.showError(err);
    }
  },

  // --- when all downloads are done ---

  renderAfterAll(st) {
    if (!st) return;
    this.afterAll = st;
    this.el("after-all").value = st.action;
    this.el("after-waiting").hidden = !st.waiting;
    this.afterRun++;
    this.afterLeft = st.seconds || 0;
    this.tickAfterAll(this.afterRun);
  },

  tickAfterAll(run) {
    if (run !== this.afterRun) return; // a newer state took over
    const st = this.afterAll;
    const counting = !!st && st.seconds > 0 && st.action !== "none";
    this.el("after-banner").hidden = !counting;
    if (!counting) return;
    this.el("after-text").textContent = t("after.done") + " " + t("after.count." + st.action, { s: Math.max(0, this.afterLeft) });
    if (this.afterLeft > 0) {
      setTimeout(() => { this.afterLeft--; this.tickAfterAll(run); }, 1000);
    }
  },

  async setAfterAll() {
    const app = this.backend();
    if (!app || !app.SetAfterAll) return;
    try {
      this.renderAfterAll(await app.SetAfterAll(this.el("after-all").value));
    } catch (err) {
      this.showError(err);
      this.renderAfterAll(this.afterAll);
    }
  },

  async abortAfterAll() {
    const app = this.backend();
    if (!app || !app.AbortAfterAll) return;
    this.renderAfterAll(await app.AbortAfterAll());
  },

  // --- links dropped on the window ---

  // One link goes into the field and is looked up, like a pasted one.
  // Several are added straight away with the options in the form.
  async onDrop(dt) {
    if (!dt) return;
    let text = [dt.getData ? dt.getData("text/uri-list") : "", dt.getData ? dt.getData("text/plain") : ""].join("\n");
    for (const f of Array.from(dt.files || [])) {
      if (/\.(txt|url)$/i.test(f.name || "") && f.size < 1 << 20 && f.text) text += "\n" + (await f.text());
    }
    const links = linksIn(text);
    this.clearError();
    if (links.length === 0) return this.showError(t("drop.none"));
    if (links.length === 1) {
      this.el("link").value = links[0];
      return this.lookup();
    }
    const app = this.backend();
    if (!app) return this.showError(t("err.noBackend"));
    let n = 0;
    for (const link of links) {
      try {
        this.upsertJob(await app.Add(link, Object.assign(this.options(), { fileName: "" }), ""));
        n++;
      } catch (err) {
        this.showError(err);
      }
    }
    this.el("add-status").textContent = t("drop.added", { n });
  },

  // wireDrop: the page takes every drop itself. Left to the web view, a
  // dropped link or file would replace the whole window with that page.
  wireDrop() {
    let depth = 0;
    const zone = this.el("drop-zone");
    document.addEventListener("dragenter", (e) => { e.preventDefault(); depth++; zone.hidden = false; });
    document.addEventListener("dragover", (e) => {
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
    });
    document.addEventListener("dragleave", () => {
      depth = Math.max(0, depth - 1);
      if (depth === 0) zone.hidden = true;
    });
    document.addEventListener("drop", (e) => {
      e.preventDefault();
      depth = 0;
      zone.hidden = true;
      return this.onDrop(e.dataTransfer);
    });
  },

  // sendTrayTexts gives Go the words for the tray icon, in the page's language.
  sendTrayTexts() {
    const app = this.backend();
    if (!app || !app.SetTrayTexts) return;
    app.SetTrayTexts({ open: t("tray.open"), quit: t("tray.quit"), running: t("tray.running"),
      balloonTitle: t("tray.balloonTitle"), balloonText: t("tray.balloonText") }).catch(() => {}); // only words; the tray has English ones
  },

  // --- activity log ---

  // A line with a key is shown in the page's language (and again in the
  // new one after a switch); the text is the English line from Go.
  logText(l) {
    return clipLong(l.key && l.key in globalThis.vcTexts.en ? t(l.key, l.vars) : l.text);
  },

  log(entry) {
    if (entry) {
      this.logLines.push(entry);
      if (this.logLines.length > maxLogLines) this.logLines.shift();
    }
    // lvl-: a plain "info" would also be the class of the video info box.
    this.el("log").innerHTML = this.logLines.map((l) =>
      `<li class="lvl-${esc(l.level)}"><span class="time">${esc(l.time)}</span>${esc(this.logText(l))}</li>`).join("");
    const last = this.logLines[this.logLines.length - 1];
    this.el("log-last").textContent = last ? this.logText(last) : "";
  },

  // --- settings ---

  openSettings() {
    const s = this.init.settings;
    this.clearError("set-error");
    this.el("set-parallel").value = s.parallel;
    this.el("set-language").value = s.language;
    this.el("set-folder").value = s.downloadDir || this.init.defaultDir;
    this.el("set-check-updates").checked = s.checkUpdates;
    this.el("set-watch-clipboard").checked = s.watchClipboard;
    this.el("set-close-button").value = s.closeButton || "tray-if-running";
    this.el("opt-close-button").hidden = !this.init.hasTray;
    this.el("set-use-current").checked = false;
    this.el("set-browser-links").checked = false;
    this.el("set-browser-ext").checked = false;
    const app = this.backend();
    if (app) {
      app.BrowserLinks().then((on) => { this.el("set-browser-links").checked = on; this.browserLinks = on; })
        .catch((err) => this.showError(err, "set-error"));
      app.BrowserExtension().then((on) => { this.el("set-browser-ext").checked = on; this.browserExt = on; })
        .catch((err) => this.showError(err, "set-error"));
    }
    this.el("settings").showModal();
  },

  async saveSettings() {
    const app = this.backend();
    if (!app) return;
    const s = this.init.settings;
    const folder = this.el("set-folder").value;
    const next = {
      defaults: this.el("set-use-current").checked ? this.options() : s.defaults,
      downloadDir: folder === this.init.defaultDir ? "" : folder,
      parallel: Number(this.el("set-parallel").value) || 1,
      checkUpdates: this.el("set-check-updates").checked,
      language: this.el("set-language").value,
      watchClipboard: this.el("set-watch-clipboard").checked,
      closeButton: this.el("set-close-button").value || s.closeButton,
    };
    next.defaults = Object.assign({}, next.defaults, { formatId: "", playlistItems: "", fileName: "" });
    try {
      await app.SaveSettings(next);
      const links = this.el("set-browser-links").checked;
      if (links !== !!this.browserLinks) await app.SetBrowserLinks(links);
      const ext = this.el("set-browser-ext").checked;
      if (ext !== !!this.browserExt) {
        await app.SetBrowserExtension(ext);
        this.browserExt = ext;
      }
      const langChanged = next.language !== s.language;
      this.init.settings = Object.assign({}, next, { defaults: Object.assign({}, next.defaults, { outputDir: folder }) });
      this.el("settings").close();
      if (folder !== s.downloadDir) this.el("folder").value = folder;
      if (langChanged) this.setLanguage(next.language);
      this.log({ level: "ok", key: "set.saved", text: "Settings saved.", time: clock() });
    } catch (err) {
      this.showError(err, "set-error");
    }
  },

  setLanguage(setting) {
    globalThis.vcSetLanguage(globalThis.vcPickLanguage(setting, (globalThis.navigator || {}).language));
    const o = this.options();
    this.applyTexts();
    this.fillSelects();
    this.setOptions(o);
    if (this.info) this.showInfo(this.info);
    this.renderTools(this.tools);
    this.renderJobs();
    this.log(null);
    this.renderSpeed(this.speed);
    this.renderAfterAll(this.afterAll);
    this.sendTrayTexts();
  },

  async checkUpdates() {
    const app = this.backend();
    if (!app) return;
    this.clearError();
    try {
      await app.CheckUpdates();
    } catch (err) {
      this.showError(err);
    }
  },

  wire() {
    const on = (id, type, fn) => this.el(id).addEventListener(type, fn);
    on("paste", "click", () => this.paste());
    on("lookup", "click", () => this.lookup());
    on("link", "keydown", (e) => { if (e.key === "Enter") this.lookup(); });
    on("add", "click", () => this.add());
    on("choose-folder", "click", () => this.chooseFolder("folder"));
    on("set-choose-folder", "click", () => this.chooseFolder("set-folder"));
    on("template-preset", "change", () => {
      const v = this.el("template-preset").value;
      if (v) this.el("template").value = v;
      this.schedulePreview();
    });
    on("template", "input", () => { this.syncPreset(); this.schedulePreview(); });
    on("own-name", "input", () => { this.ownNameEdited = true; this.schedulePreview(); });
    on("own-name-reset", "click", () => this.resetOwnName());
    on("clean-names", "change", () => this.schedulePreview());
    on("pause-all", "click", () => this.backend() && this.backend().PauseAll());
    on("resume-all", "click", () => this.backend() && this.backend().ResumeAll());
    on("speed", "input", () => this.moveSpeed(false));
    on("speed", "change", () => this.moveSpeed(true));
    on("after-all", "change", () => this.setAfterAll());
    on("after-abort", "click", () => this.abortAfterAll());
    this.wireDrop();
    for (const id of ["quality", "container", "audio-format", "sub-mode", "playlist"]) {
      on(id, "change", () => { this.updateVisibility(); this.schedulePreview(); });
    }
    on("formats", "change", (e) => {
      const r = e.target;
      if (r && r.name === "format") this.chooseFormat(r.value, r.dataset.video === "1", r.dataset.audio === "1");
    });
    on("sub-chips", "click", (e) => {
      const b = e.target && e.target.closest ? e.target.closest("[data-lang]") : null;
      if (b) this.toggleLang(b.dataset.lang);
    });
    on("jobs", "click", (e) => this.onJobsClick(e));
    on("clear-done", "click", () => this.clearFinished());
    on("open-folder", "click", () => this.backend() && this.backend().OpenFolder("downloads").catch((err) => this.showError(err)));
    on("open-log", "click", () => this.backend() && this.backend().OpenFolder("log").catch((err) => this.showError(err)));
    on("check-updates", "click", () => this.checkUpdates());
    on("open-settings", "click", () => this.openSettings());
    on("set-cancel", "click", () => this.el("settings").close());
    on("set-save", "click", () => this.saveSettings());
    on("clip-use", "click", () => {
      this.el("link").value = this.el("clip-url").textContent;
      this.el("clip-banner").hidden = true;
      this.lookup();
    });
    on("clip-dismiss", "click", () => { this.el("clip-banner").hidden = true; });
  },

  listen() {
    const rt = window.runtime;
    if (!rt || !rt.EventsOn) return;
    rt.EventsOn("tools", (list) => this.renderTools(list));
    rt.EventsOn("job", (j) => this.upsertJob(j));
    rt.EventsOn("log", (l) => this.log(l));
    rt.EventsOn("speed", (st) => this.renderSpeed(st));
    rt.EventsOn("afterall", (st) => this.renderAfterAll(st));
    rt.EventsOn("clipboard", (link) => {
      this.el("clip-url").textContent = link;
      this.el("clip-banner").hidden = false;
    });
  },

  async start() {
    this.wire();
    const app = this.backend();
    if (!app) {
      globalThis.vcSetLanguage(globalThis.vcPickLanguage("auto", (globalThis.navigator || {}).language));
      this.applyTexts();
      this.showError(t("err.noBackend"));
      return;
    }
    try {
      this.init = await app.Initial();
      globalThis.vcSetLanguage(globalThis.vcPickLanguage(this.init.settings.language, (globalThis.navigator || {}).language));
      this.applyTexts();
      this.fillSelects();
      this.setOptions(this.init.settings.defaults);
      this.renderTools(this.init.tools);
      this.jobs = new Map((this.init.jobs || []).map((j) => [j.id, j]));
      this.renderJobs();
      for (const l of this.init.log || []) this.log(l);
      this.renderSpeed(this.init.speed);
      this.renderAfterAll(this.init.afterAll);
      this.sendTrayTexts();
      this.el("version").textContent = this.init.version;
      this.listen();
      this.schedulePreview();
    } catch (err) {
      this.showError(err);
    }
  },
};

// The checks read these; in the window they are simply unused globals.
globalThis.kitUI = ui;
globalThis.kitFormatSize = formatSize;
globalThis.vcEsc = esc;
globalThis.vcFormatDuration = formatDuration;
globalThis.vcBaseName = baseName;
globalThis.vcClipLong = clipLong;
globalThis.vcLinksIn = linksIn;

document.addEventListener("DOMContentLoaded", () => ui.start());
