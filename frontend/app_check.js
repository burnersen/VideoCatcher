// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Checks of the page logic, without a browser: node app_check.js
"use strict";
const fs = require("fs");
const path = require("path");
const { loadPage, check, done, dist } = require("./check_harness");

(async () => {
  // Wiring: every element the script asks for exists in index.html, and
  // index.html loads the script and the style sheet. A renamed id would
  // otherwise only show up as a dead button in the real window.
  const html = fs.readFileSync(path.join(dist, "index.html"), "utf8");
  const js = fs.readFileSync(path.join(dist, "app.js"), "utf8");
  const used = [...js.matchAll(/el\("([^"]+)"\)/g)].map((m) => m[1]);
  check("the script asks for elements at all", used.length > 0);
  for (const id of new Set(used)) {
    check(`index.html has id="${id}"`, html.includes(`id="${id}"`));
  }
  check("index.html loads app.js", html.includes('src="app.js"'));
  check("index.html loads style.css", html.includes('href="style.css"'));
  check("no inline script in index.html", !/<script>[\s\S]*?<\/script>/.test(html));

  const { formatSize } = loadPage(null);
  check("formatSize bytes", formatSize(512) === "512 bytes", formatSize(512));
  check("formatSize MB", formatSize(1.5 * 1024 * 1024) === "1.5 MB", formatSize(1.5 * 1024 * 1024));
  check("formatSize GB", formatSize(20 * 1024 ** 3) === "20 GB", formatSize(20 * 1024 ** 3));

  // An error from Go: message and hint on separate lines.
  {
    const p = loadPage({
      PickFile: async () => "C:\\x.txt",
      CheckFile: async () => { throw "Cannot open C:\\x.txt: it does not exist\nCheck the name and the folder."; },
      Version: async () => "1.0.0",
    });
    await p.ui.check();
    check("error is shown", p.el("error").hidden === false);
    check("error message", p.el("error-msg").textContent === "Cannot open C:\\x.txt: it does not exist", p.el("error-msg").textContent);
    check("error hint", p.el("error-hint").textContent === "Check the name and the folder.", p.el("error-hint").textContent);
    check("button enabled again after an error", p.el("pick").disabled === false);
    check("status cleared after an error", p.el("status").textContent === "");
  }

  // Cancelled dialog: no error, no check.
  {
    let checked = false;
    const p = loadPage({
      PickFile: async () => "",
      CheckFile: async () => { checked = true; return {}; },
      Version: async () => "1.0.0",
    });
    await p.ui.check();
    check("cancel is not an error", p.el("error").hidden !== false && p.el("error-msg").textContent === "");
    check("cancel checks nothing", checked === false);
  }

  // Success.
  {
    const p = loadPage({
      PickFile: async () => "C:\\a.txt",
      CheckFile: async () => ({ path: "C:\\a.txt", size: 2048, sha256: "abc" }),
      Version: async () => "1.0.0",
    });
    await p.ui.check();
    check("result shown", p.el("result").hidden === false);
    check("result size", p.el("result-size").textContent === "2.0 KB", p.el("result-size").textContent);
    check("no error on success", p.el("error").hidden !== false);
  }

  // Outside the program (no Go side): a clear message, not a silent dead button.
  {
    const p = loadPage(null);
    await p.ui.check();
    check("no backend: error shown", p.el("error").hidden === false && p.el("error-msg").textContent.includes("not connected"));
  }

  // Start: version in the footer, click wired to check().
  {
    const p = loadPage({ PickFile: async () => "", CheckFile: async () => ({}), Version: async () => "2.3.4" });
    await p.docListeners.DOMContentLoaded();
    check("version shown", p.el("version").textContent === "2.3.4");
    check("button wired", (p.el("pick").listeners.click || []).length === 1);
  }

  done();
})();
