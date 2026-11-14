// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// check_harness.js runs the page's own scripts without a browser.
//
// It loads the shipped dist/i18n.js, dist/app.js and dist/jobs.js, not copies: a copy
// would drift and then prove nothing. Elements are stand-ins that remember
// what was written to them, so a check can read back what the window would
// show. Run a check with "node app_check.js"; it ends with exit code 1 on
// the first failure.
"use strict";
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const dist = path.join(__dirname, "dist");

function fakeElement(id) {
  const el = {
    id, textContent: "", value: "", hidden: false, disabled: false, checked: false,
    innerHTML: "", outerHTML: "", placeholder: "", src: "", name: "",
    className: "", dataset: {}, listeners: {}, open: false, style: {},
    addEventListener(type, fn) { (el.listeners[type] = el.listeners[type] || []).push(fn); },
    fire(type, event) { return Promise.all((el.listeners[type] || []).map((fn) => fn(event || { target: el }))); },
    click() { return el.fire("click"); },
    showModal() { el.open = true; },
    close() { el.open = false; },
    closest() { return null; },
  };
  return el;
}

// target builds a click target like a button inside the job list.
function target(dataset) {
  const t = { dataset };
  t.closest = () => t;
  return t;
}

// loadPage runs the page scripts with a fake document and the given Go
// backend (an object with the methods of App in app.go, or null).
function loadPage(backend, language) {
  const elements = {};
  const docListeners = {};
  const document = {
    getElementById: (id) => (elements[id] = elements[id] || fakeElement(id)),
    querySelectorAll: () => [],
    addEventListener: (type, fn) => { docListeners[type] = fn; },
  };
  const events = {};
  const window = backend ? { go: { main: { App: backend } } } : {};
  window.runtime = { EventsOn: (name, fn) => { events[name] = fn; } };
  const timers = [];
  const context = vm.createContext({
    document, window, console, navigator: { language: language || "en-US" },
    setTimeout: (fn) => { timers.push(fn); return timers.length; },
    clearTimeout: () => {},
  });
  for (const file of ["i18n.js", "app.js", "jobs.js"]) {
    vm.runInContext(fs.readFileSync(path.join(dist, file), "utf8"), context, { filename: file });
  }
  const runTimers = async () => { while (timers.length) await timers.shift()(); };
  return {
    ui: context.kitUI, formatSize: context.kitFormatSize, esc: context.vcEsc, t: context.vcT,
    texts: context.vcTexts, formatDuration: context.vcFormatDuration, baseName: context.vcBaseName, clipLong: context.vcClipLong, linksIn: context.vcLinksIn, formatWhen: context.vcFormatWhen,
    elements, el: document.getElementById, docListeners, events, runTimers,
  };
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

module.exports = { loadPage, check, done, dist, target };
