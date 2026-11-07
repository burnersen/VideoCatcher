// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Makes the two forms of the extension from src/, without npm:
//
//   node extension/build.js 0.2.0
//
// dist/chromium  Chrome and Edge: background as a service worker, with the
//                "key" that fixes the extension's ID (nativehost.go allows
//                exactly that ID). Without browser_specific_settings, which
//                Chrome would warn about.
// dist/firefox   Firefox: background as a page with both scripts, the gecko
//                ID, without "key", which Firefox would warn about.
//
// The version comes from the command line (the release's version), so the
// extension and Video-Catcher always carry the same number.
"use strict";
const fs = require("fs");
const path = require("path");

const version = process.argv[2] || "0.0.0";
if (!/^\d+\.\d+\.\d+$/.test(version)) {
  console.error(`not a version for the extension: ${version} (browsers want 1.2.3, no suffix)`);
  process.exit(1);
}
const src = path.join(__dirname, "src");
const dist = path.join(__dirname, "dist");
const base = JSON.parse(fs.readFileSync(path.join(src, "manifest.json"), "utf8"));

function variant(name, change) {
  const out = path.join(dist, name);
  fs.rmSync(out, { recursive: true, force: true });
  fs.cpSync(src, out, { recursive: true });
  const m = JSON.parse(JSON.stringify(base));
  m.version = version;
  change(m);
  fs.writeFileSync(path.join(out, "manifest.json"), JSON.stringify(m, null, 2) + "\n");
  console.log(`${name}: ${out}`);
}

variant("chromium", (m) => {
  delete m.browser_specific_settings;
});
variant("firefox", (m) => {
  delete m.key;
  m.background = { scripts: ["common.js", "background.js"] };
});
