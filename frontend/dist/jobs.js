// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// The list of downloads: what a row shows, its buttons, the search. This
// file adds its part to the "ui" object of app.js, which index.html loads
// first; both share one name space (esc, t, formatSize ... come from there).
//
// Everything that comes from a web site (titles, file names, links) is put
// into the page with esc(), like in app.js.
"use strict";

/* global t, esc, ui, formatSize, formatDuration, hostOf, clipLong */

// formatWhen is the day and time a download ended, written the way Windows
// is set up to write dates ("14.11.26, 16:40"). No date gives "".
function formatWhen(iso) {
  const d = new Date(iso || "");
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return "";
  return d.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" });
}

// fileName is the name of a path without its folder.
function fileName(p) {
  return String(p || "").split(/[\\/]/).pop();
}

// Files of one job listed in its row; a channel can have thousands.
const maxFilesShown = 300;

Object.assign(ui, {
  jobShapes: new Map(), // id -> what a row looks like apart from the numbers (see upsertJob)
  filter: "", // what is typed into the search field, in lower case
  shown: new Set(), // ids of the jobs that have a row (the search hides the others)

  jobParts(j) {
    const pct = j.status === "failed" ? "!" : j.status === "cancelled" ? "x" : Math.floor(j.percent || 0) + "%";
    const files = j.files || [];
    // A playlist that is done, but some of its videos could not be loaded.
    const skipped = j.status === "done" ? Number(j.skipped) || 0 : 0;
    // A paused job keeps its numbers: they say how far it got.
    const meta = [];
    const item = (text, cls) => { if (text) meta.push(`<span${cls ? ` class="${cls}"` : ""}>${esc(text)}</span>`); };
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
      // Why it waits, when there is more to say than "waiting": next in
      // line, continues after a restart, tries again after a lost connection.
      const why = j.status === "waiting" && j.stage && j.stage !== "waiting" && "st." + j.stage in globalThis.vcTexts.en;
      item(why ? stage(j.stage) : t("job." + j.status));
      if (j.status === "done") {
        if (skipped > 0) item(t("job.skipped", { n: skipped }), "warn");
        else if (j.stage && j.stage !== "done") item(stage(j.stage));
        if (files.length > 1) item(t("job.files", { n: files.length }));
        item(formatSize(j.size), "num");
        if (j.elapsed >= 1) item(t("job.took", { t: formatDuration(j.elapsed) }), "num");
        item(formatWhen(j.ended), "num");
      }
    }

    const id = esc(j.id);
    const btn = (action, label, extra) => `<button type="button" class="ghost small" data-action="${action}" data-id="${id}"${extra || ""}>${esc(t(label))}</button>`;
    const actions = [];
    if (j.status === "waiting" && !j.next) actions.push(btn("next", "job.next"));
    if (j.status === "running" || j.status === "waiting") actions.push(btn("pause", "job.pause"), btn("cancel", "job.stop"));
    if (j.status === "paused") actions.push(btn("resume", "job.resume"), btn("cancel", "job.stop"));
    if (j.status === "confirm") actions.push(btn("confirm", "job.yes"), btn("cancel", "job.stop"));
    if (j.status === "failed" || j.status === "cancelled") actions.push(btn("retry", "job.retry"));
    if (j.status === "done" && files.length === 1) actions.push(btn("open", "job.play", ' data-index="0"'));
    if (j.status === "done" && files.length > 0) actions.push(btn("show", "job.show", ' data-index="0"'));
    if (j.status !== "running") actions.push(btn("remove", "job.remove"));

    let below = "";
    if (j.status === "confirm") below += `<p class="confirm-text">${esc(t("job.confirm"))} ${esc(clipLong(j.url))}</p>`;
    if ((j.status === "failed" || skipped > 0) && j.error) {
      const [msg, ...hint] = String(j.error).split("\n");
      below += `<p class="error-text"><b>${esc(clipLong(msg))}</b>${esc(clipLong(hint.join(" ")))}</p>`;
    }
    if (j.status === "done" && files.length > 1) {
      // Every file of a playlist: a click plays it (or shows it in its
      // folder, when it is no video or audio file).
      const rows = files.slice(0, maxFilesShown).map((f, i) =>
        `<li><button type="button" class="file" data-action="open" data-id="${id}" data-index="${i}" title="${esc(f)}">${esc(fileName(f))}</button></li>`);
      below += `<details class="files"><summary>${esc(t("job.files", { n: files.length }))}</summary><ul>${rows.join("")}</ul></details>`;
    }
    const hasDetails = (j.details || []).length > 0 && (j.status === "failed" || j.status === "done");
    if (hasDetails) {
      below += `<details><summary>${esc(t("job.details"))}</summary><pre>${esc(j.details.join("\n"))}</pre></details>`;
    }
    if (j.status === "failed" || (hasDetails && skipped > 0)) {
      below += `<p class="job-extra">${btn("copy-details", "job.copyDetails")}</p>`;
    }
    // The site: a click copies the link. It stands next to the numbers but
    // is no part of them, so it stays in place while they change.
    const host = hostOf(j.url);
    const hostButton = host ? `<button type="button" class="host" data-action="copy-link" data-id="${id}" title="${esc(t("job.copyLink"))}">${esc(host)}</button>` : "";
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
      `<span class="sub"><span id="job-${id}-meta" class="meta">${parts.meta}</span>${hostButton}</span>` +
      `<span class="track"><span id="job-${id}-fill" class="fill" style="width:${parts.width}"></span></span>` +
      below + "</li>";
    return parts;
  },

  jobRow(j) {
    return this.jobParts(j).html;
  },

  // matches says whether a job fits what is typed into the search field:
  // a part of its title, link, state or of one of its file names.
  matches(j) {
    if (!this.filter) return true;
    const hay = [j.title, j.url, j.itemTitle, t("job." + j.status)].concat((j.files || []).map(fileName));
    return hay.join("\n").toLowerCase().includes(this.filter);
  },

  renderJobs() {
    const list = [...this.jobs.values()].filter((j) => this.matches(j))
      .sort((a, b) => String(b.added).localeCompare(String(a.added)));
    this.jobShapes = new Map();
    this.shown = new Set(list.map((j) => j.id));
    this.el("jobs").innerHTML = list.map((j) => {
      const parts = this.jobParts(j);
      this.jobShapes.set(j.id, parts.shape);
      return parts.html;
    }).join("");
    this.el("jobs-empty").hidden = this.jobs.size > 0;
    this.el("jobs-nomatch").hidden = this.jobs.size === 0 || list.length > 0;
    this.renderCounts();
  },

  renderCounts() {
    let r = 0;
    let w = 0;
    let p = 0;
    let f = 0;
    for (const j of this.jobs.values()) {
      if (j.status === "running") r++;
      if (j.status === "waiting" || j.status === "confirm") w++;
      if (j.status === "paused") p++;
      if (j.status === "failed") f++;
    }
    this.el("jobs-counts").textContent = p > 0 ? t("jobs.countsPaused", { r, w, p }) : r + w > 0 ? t("jobs.counts", { r, w }) : "";
    this.el("retry-failed").hidden = f === 0;
  },

  // upsertJob changes one row in place; a new job draws the whole list.
  // While only the numbers change (progress comes four times a second), the
  // row stays and only its numbers and bar change: a new row each time
  // would swallow a click on "Stop" that starts on the old row and ends on
  // the new one, and the bar could not glide.
  upsertJob(j) {
    const known = this.jobs.has(j.id);
    this.jobs.set(j.id, j);
    const want = this.matches(j);
    if (known && !want && !this.shown.has(j.id)) {
      this.renderCounts(); // hidden by the search, and it stays hidden
      return;
    }
    const row = known && want && this.shown.has(j.id) ? this.el("job-" + j.id) : null;
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
        case "open": await app.OpenFile(id, Number(target.dataset.index) || 0); break;
        case "next": await app.StartNext(id); break;
        case "copy-link": await app.CopyLink(id); this.sayCopied(target); break;
        case "copy-details": await app.CopyDetails(id); this.sayCopied(target); break;
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

  // sayCopied shows on the button itself that the text is in the clipboard.
  sayCopied(button) {
    const before = button.textContent;
    button.textContent = t("job.copied");
    setTimeout(() => { button.textContent = before; }, 1500);
  },

  async retryFailed() {
    const app = this.backend();
    if (!app) return;
    try {
      await app.RetryFailed(); // every job that waits again reports itself
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
});

globalThis.vcFormatWhen = formatWhen;
