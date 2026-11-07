// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// check_harness.js runs the page's own script without a browser.
//
// It loads the shipped dist/app.js, not a copy: a copy would drift and then
// prove nothing. Elements are stand-ins that remember what was written to
// them, so a check can read back what the window would show. Run a check
// with "node app_check.js"; it ends with exit code 1 on the first failure.
"use strict";
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const dist = path.join(__dirname, "dist");

function fakeElement(id) {
  const el = {
    id, textContent: "", value: "", hidden: false, disabled: false,
    className: "", listeners: {},
    addEventListener(type, fn) { (el.listeners[type] = el.listeners[type] || []).push(fn); },
    click() { (el.listeners.click || []).forEach((fn) => fn()); },
  };
  return el;
}

// loadPage runs dist/app.js with a fake document and the given Go backend
// (an object with the methods of App in app.go, or null).
function loadPage(backend) {
  const elements = {};
  const docListeners = {};
  const document = {
    getElementById: (id) => (elements[id] = elements[id] || fakeElement(id)),
    addEventListener: (type, fn) => { docListeners[type] = fn; },
  };
  const window = backend ? { go: { main: { App: backend } } } : {};
  const context = vm.createContext({ document, window, console });
  vm.runInContext(fs.readFileSync(path.join(dist, "app.js"), "utf8"), context, { filename: "app.js" });
  return { ui: context.kitUI, formatSize: context.kitFormatSize, elements, el: document.getElementById, docListeners };
}

let failures = 0;
function check(name, ok, detail) {
  if (ok) {
    console.log("ok    " + name);
  } else {
    console.log("FAIL  " + name + (detail === undefined ? "" : "\n      " + JSON.stringify(detail)));
    failures++;
  }
}

function done() {
  if (failures > 0) {
    console.log(failures + " check(s) failed");
    process.exit(1);
  }
}

module.exports = { loadPage, check, done, dist };
