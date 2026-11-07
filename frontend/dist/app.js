// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// The whole logic of the page. Plain JavaScript: no framework, no npm, no
// build step. Go does the work (app.go); this file asks Go and shows what Go
// reports. Checked without a browser by ../app_check.js.
"use strict";

// formatSize turns bytes into "1.5 MB".
function formatSize(bytes) {
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

const ui = {
  // The Go side; null when the page runs outside the program.
  backend() {
    return (window.go && window.go.main && window.go.main.App) || null;
  },

  el(id) {
    return document.getElementById(id);
  },

  // showError shows an error from Go: first line is the message, the rest
  // is the hint (see uiError in errors.go). Never an empty box, never only
  // a console.log: an error nobody sees did not get reported.
  showError(err) {
    const text = String(err && err.message ? err.message : err || "Unknown error");
    const lines = text.split("\n");
    this.el("error-msg").textContent = lines[0];
    this.el("error-hint").textContent = lines.slice(1).join(" ");
    this.el("error").hidden = false;
  },

  clearError() {
    this.el("error").hidden = true;
    this.el("error-msg").textContent = "";
    this.el("error-hint").textContent = "";
  },

  showResult(r) {
    this.el("result-path").textContent = r.path;
    this.el("result-size").textContent = formatSize(r.size);
    this.el("result-sha").textContent = r.sha256;
    this.el("result").hidden = false;
  },

  // check: choose a file, let Go check it, show the result or the error.
  // The button is disabled while it runs and always enabled again.
  async check() {
    const app = this.backend();
    if (!app) {
      this.showError("The window is not connected to the program.\nPlease start VideoCatcher.exe again.");
      return;
    }
    this.clearError();
    const button = this.el("pick");
    button.disabled = true;
    try {
      const path = await app.PickFile();
      if (!path) return; // cancelled: not an error
      this.el("status").textContent = "Checking ...";
      this.showResult(await app.CheckFile(path));
    } catch (err) {
      this.el("result").hidden = true;
      this.showError(err);
    } finally {
      button.disabled = false;
      this.el("status").textContent = "";
    }
  },

  async init() {
    this.el("pick").addEventListener("click", () => this.check());
    const app = this.backend();
    if (app) {
      try {
        this.el("version").textContent = await app.Version();
      } catch (err) {
        this.showError(err);
      }
    }
  },
};

// The checks read these; in the window they are simply unused globals.
globalThis.kitUI = ui;
globalThis.kitFormatSize = formatSize;

document.addEventListener("DOMContentLoaded", () => ui.init());
