// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only
//
// Checks of the page logic, without a browser: node app_check.js
"use strict";
const fs = require("fs");
const path = require("path");
const { loadPage, check, done, dist, target } = require("./check_harness");

const html = fs.readFileSync(path.join(dist, "index.html"), "utf8");
const js = fs.readFileSync(path.join(dist, "app.js"), "utf8");
const css = fs.readFileSync(path.join(dist, "style.css"), "utf8");
const appGo = fs.readFileSync(path.join(__dirname, "..", "app.go"), "utf8");

const choices = {
  quality: ["best", "2160", "1440", "1080", "720", "480", "360", "audio"],
  container: ["auto", "mp4", "mkv", "webm"], audioFormat: ["best", "mp3"], audioQuality: ["best", "192"],
  subMode: ["none", "embed", "file", "both"], playlist: ["single", "all"],
  sponsorBlock: ["off", "mark", "remove"], cookies: ["none", "firefox"],
};
const defaults = {
  quality: "best", formatId: "", container: "auto", audioFormat: "best", audioQuality: "best",
  subLangs: "de,en", subMode: "none", autoSubs: false, outputDir: "D:\\Videos\\VideoCatcher",
  template: "%(title)s [%(id)s].%(ext)s", playlist: "single", playlistItems: "", embedThumbnail: false,
  embedMetadata: true, embedChapters: true, sponsorBlock: "off", rateLimit: "", fragments: 4, cookies: "none",
};
const job = (over) => Object.assign({ id: "j1", url: "https://www.youtube.com/watch?v=abc", title: "A video",
  status: "running", stage: "downloading", percent: 42.7, done: 1048576, total: 10485760, speed: 524288,
  eta: 18, files: [], details: [], added: "2026-11-07T14:00:00Z" }, over || {});

function backend(over) {
  const calls = [];
  const b = {
    calls,
    Initial: async () => ({ version: "1.2.3", settings: { defaults, downloadDir: "", parallel: 2, checkUpdates: true, language: "auto", watchClipboard: false },
      defaultDir: defaults.outputDir, choices, tools: [{ tool: "yt-dlp", state: "ready", version: "2026.10.26", purpose: "downloads" }], jobs: [] }),
    Probe: async (link, pl, ck) => { calls.push(["Probe", link, pl, ck]); return { url: link, title: "Found <b>it</b>", uploader: "Someone", site: "Youtube", duration: 212,
      thumbnail: "https://i.ytimg.com/x.jpg", heights: [1080, 720], formats: [{ id: "137", ext: "mp4", height: 1080, hasVideo: true, hasAudio: false, size: 1000 }],
      subtitles: ["de"], autoSubs: ["en-orig"], probeId: "p1" }; },
    Preview: async () => "D:\\Videos\\VideoCatcher\\Found it [abc].mp4",
    Add: async (link, o, title) => { calls.push(["Add", link, o, title]); return job({ id: "j9", url: link, title, status: "waiting", percent: 0 }); },
    Cancel: async (id) => { calls.push(["Cancel", id]); },
    Retry: async (id) => { calls.push(["Retry", id]); },
    Confirm: async (id) => { calls.push(["Confirm", id]); },
    Remove: async (id) => { calls.push(["Remove", id]); },
    ShowFile: async (id, i) => { calls.push(["ShowFile", id, i]); },
    Clipboard: async () => "https://example.org/v",
    ClearFinished: async () => [],
    BrowserLinks: async () => false,
  };
  return Object.assign(b, over || {});
}

(async () => {
  // Wiring: every element the script asks for exists in index.html, and
  // index.html loads the scripts and the style sheet. A renamed id would
  // otherwise only show up as a dead button in the real window.
  const used = [...js.matchAll(/(?:el|on)\("([^"]+)"/g)].map((m) => m[1]).filter((id) => !id.endsWith("-")); // "job-" + id is built
  check("the script asks for elements at all", used.length > 20);
  for (const id of new Set(used)) {
    check(`index.html has id="${id}"`, html.includes(`id="${id}"`));
  }
  for (const box of ["error", "set-error"]) {
    check(`error box ${box} is complete`, ["", "-msg", "-hint"].every((s) => html.includes(`id="${box}${s}"`)));
  }
  check("index.html loads i18n.js before app.js", html.indexOf('src="i18n.js"') > 0 && html.indexOf('src="i18n.js"') < html.indexOf('src="app.js"'));
  check("index.html loads style.css", html.includes('href="style.css"'));
  check("no inline script in index.html", !/<script>[\s\S]*?<\/script>/.test(html));

  // Texts: both languages have the same keys, and every key the page uses exists.
  const p0 = loadPage(null);
  const en = Object.keys(p0.texts.en).sort();
  const de = Object.keys(p0.texts.de).sort();
  check("English and German have the same keys", JSON.stringify(en) === JSON.stringify(de),
    en.filter((k) => !de.includes(k)).concat(de.filter((k) => !en.includes(k))));
  const htmlKeys = [...html.matchAll(/data-i18n(?:-ph)?="([^"]+)"/g)].map((m) => m[1]);
  const jsKeys = [...js.matchAll(/\bt\("([^"]+)"/g)].map((m) => m[1]).filter((k) => !k.endsWith(".")); // prefixes: checked below
  for (const k of new Set([...htmlKeys, ...jsKeys])) check(`text "${k}" exists`, en.includes(k));
  for (const prefix of ["q.", "s.", "p.", "sb.", "job.", "tools."]) {
    const need = { "q.": choices.quality, "s.": choices.subMode, "p.": choices.playlist, "sb.": choices.sponsorBlock,
      "job.": ["waiting", "confirm", "running", "done", "failed", "cancelled"],
      "tools.": ["missing", "downloading", "ready", "failed"] }[prefix];
    for (const v of need) check(`text "${prefix}${v}" exists`, en.includes(prefix + v));
  }

  // Helpers.
  check("formatSize MB", p0.formatSize(1.5 * 1024 * 1024) === "1.5 MB");
  check("formatSize GB", p0.formatSize(20 * 1024 ** 3) === "20 GB");
  check("formatDuration", p0.formatDuration(212) === "3:32" && p0.formatDuration(3723) === "1:02:03");
  check("esc", p0.esc(`<img src=x onerror="go()">`) === "&lt;img src=x onerror=&quot;go()&quot;&gt;");

  // Start: version, menus, tools.
  const b = backend();
  const p = loadPage(b);
  await p.docListeners.DOMContentLoaded();
  check("version shown", p.el("version").textContent === "1.2.3");
  check("quality menu filled", p.el("quality").innerHTML.includes('value="1080"') && p.el("quality").innerHTML.includes("Audio only"));
  check("defaults in the form", p.el("template").value === defaults.template && p.el("folder").value === defaults.outputDir);
  check("tools shown", p.el("tools").innerHTML.includes("2026.10.26"));
  check("events subscribed", ["tools", "job", "log", "clipboard"].every((e) => typeof p.events[e] === "function"));
  check("buttons wired", (p.el("lookup").listeners.click || []).length === 1 && (p.el("add").listeners.click || []).length === 1);

  // Look up: info shown, title as text, preview asked, button back.
  p.el("link").value = "https://www.youtube.com/watch?v=abc";
  await p.el("lookup").click();
  check("looked up with the form's settings", JSON.stringify(b.calls[0]) === JSON.stringify(["Probe", "https://www.youtube.com/watch?v=abc", "single", "none"]));
  check("info shown", p.el("info").hidden === false && p.el("info-title").textContent === "Found <b>it</b>");
  check("available heights marked", p.el("quality").innerHTML.includes("1080p, available"));
  check("format list escapes and lists", p.el("formats").innerHTML.includes('value="137"'));
  check("lookup button enabled again", p.el("lookup").disabled === false);
  await p.runTimers();
  check("file name preview shown", p.el("preview").textContent.endsWith("Found it [abc].mp4"));

  // A name of your own: the field starts with the pattern's name, and only a
  // changed name goes to Go.
  check("own name field shown for a single video", p.el("opt-own-name").hidden === false);
  check("own name starts with the pattern's name", p.el("own-name").value === "Found it [abc]", p.el("own-name").value);
  check("untouched own name is not sent", p.ui.options().fileName === "");
  p.el("own-name").value = "  Holiday 2026 ";
  await p.el("own-name").fire("input");
  check("typed own name is sent, trimmed", p.ui.options().fileName === "Holiday 2026");
  await p.runTimers();
  check("typed name stays while the preview updates", p.el("own-name").value === "  Holiday 2026 ");
  await p.el("own-name-reset").click();
  check("reset brings the pattern back", p.el("own-name").value === "Found it [abc]" && p.ui.options().fileName === "");
  p.el("own-name").value = "Holiday 2026";
  await p.el("own-name").fire("input");
  p.el("playlist").value = "all";
  p.ui.updateVisibility();
  check("no own name for a whole playlist", p.el("opt-own-name").hidden === true && p.ui.options().fileName === "");
  p.el("playlist").value = "single";
  p.ui.updateVisibility();

  // A video stream without sound gets the best sound.
  p.ui.chooseFormat("137", true, false);
  check("video-only format gets audio", p.ui.options().formatId === "137+ba/137");
  p.ui.chooseFormat("", false, false);

  // Add: the title from the look-up goes with it, the row appears.
  await p.el("add").click();
  const add = b.calls.find((c) => c[0] === "Add");
  check("added with title and options", add && add[3] === "Found <b>it</b>" && add[2].template === defaults.template);
  check("added with the own name", add && add[2].fileName === "Holiday 2026");
  check("own name gone after adding", p.el("opt-own-name").hidden === true && p.el("own-name").value === "" && p.ui.ownNameEdited === false);
  check("row in the list", p.el("jobs").innerHTML.includes('id="job-j9"'));
  check("titles from sites are escaped in the list", p.el("jobs").innerHTML.includes("Found &lt;b&gt;it&lt;/b&gt;") && !p.el("jobs").innerHTML.includes("<b>it</b>"));
  check("empty text hidden", p.el("jobs-empty").hidden === true);

  // Rows for every state have the right buttons.
  const row = (j) => p.ui.jobRow(job(j));
  check("running: stop, no remove", row({}).includes('data-action="cancel"') && !row({}).includes('data-action="remove"'));
  check("running: big percent and speed", row({}).includes(">42%<") && row({}).includes("512 KB/s"));
  check("confirm: download button and the link", row({ status: "confirm" }).includes('data-action="confirm"') && row({ status: "confirm" }).includes("web page"));
  const failed = row({ status: "failed", error: "The video is not available\nIt may have been deleted.", details: ["Error: <script>"] });
  check("failed: message, hint, retry, escaped details", failed.includes("The video is not available") && failed.includes('data-action="retry"') && failed.includes("&lt;script&gt;"));
  check("done: show in folder", row({ status: "done", percent: 100, files: ["C:\\a.mp4"] }).includes('data-action="show"'));

  // Clicks in the list call Go with the job's id.
  await p.ui.onJobsClick({ target: target({ action: "cancel", id: "j9" }) });
  await p.ui.onJobsClick({ target: target({ action: "show", id: "j9", index: "0" }) });
  check("stop calls Cancel", b.calls.some((c) => c[0] === "Cancel" && c[1] === "j9"));
  check("show calls ShowFile", b.calls.some((c) => c[0] === "ShowFile" && c[1] === "j9" && c[2] === 0));
  await p.ui.onJobsClick({ target: target({ action: "remove", id: "j9" }) });
  check("remove takes the row away", !p.el("jobs").innerHTML.includes("job-j9"));

  // Progress events change only the numbers and the bar of the row (a new
  // row four times a second would swallow clicks on "Stop").
  p.events.job(job({ id: "j5" }));
  check("new job: row with a bar", p.el("jobs").innerHTML.includes('id="job-j5-fill" class="fill" style="width:42.7%"'));
  p.el("job-j5").outerHTML = "untouched";
  p.events.job(job({ id: "j5", percent: 80, speed: 1048576 }));
  check("progress: number and bar change", p.el("job-j5-pct").textContent === "80%" && p.el("job-j5-fill").style.width === "80%");
  check("progress: meta changes", p.el("job-j5-meta").innerHTML.includes("1.0 MB/s"));
  check("progress: the row itself stays", p.el("job-j5").outerHTML === "untouched");

  // The bar can show at all: the fill is a span and needs a block box, and
  // every part of a row has its own grid column (0.1.0 had neither).
  const rule = (sel) => (new RegExp("(^|\\n)" + sel.replace(/[.*]/g, "\\$&") + " \\{([^}]*)\\}").exec(css) || [])[2] || "";
  check("fill is a block", /display:\s*block/.test(rule(".fill")), rule(".fill"));
  check("track is a block", /display:\s*block/.test(rule(".track")), rule(".track"));
  check("title in column 2", /grid-column:\s*2;/.test(rule(".job .title")));
  check("buttons in column 3", /grid-column:\s*3;/.test(rule(".job .actions")));
  check("counts", p.el("jobs-counts").textContent === "1 running, 0 waiting", p.el("jobs-counts").textContent);
  p.events.job(job({ id: "j5", status: "done", percent: 100, files: ["C:\\a.mp4"] }));
  check("new state: the row is drawn again", p.el("job-j5").outerHTML.includes('data-action="show"'));

  // Log lines are text, never markup.
  p.events.log({ level: "warn", text: "<i>x</i>", time: "14:00:00" });
  check("log escaped", p.el("log").innerHTML.includes("&lt;i&gt;x&lt;/i&gt;"));
  check("log level class does not clash with .info", p.el("log").innerHTML.includes('class="lvl-warn"'));

  // Every stage Go reports has a text in both languages.
  const goSrc = ["queue.go", "progress.go"].map((f) => fs.readFileSync(path.join(__dirname, "..", f), "utf8")).join("\n");
  const stages = new Set();
  for (const line of goSrc.split("\n")) {
    const m = /(?:Stage\b[^=\n]*=[^"\n]*|Stage:\s*)"([^"]+)"/.exec(line);
    if (m) stages.add(m[1]);
  }
  const labels = /func postStepLabel[\s\S]*?\n}/.exec(goSrc)[0];
  for (const m of labels.matchAll(/return "([^"]+)"/g)) stages.add(m[1]);
  check("stages found in Go", stages.size > 15, [...stages]);
  for (const st of stages) check(`stage "${st}" has a text`, en.includes("st." + st));
  {
    const g = loadPage(backend(), "de-DE");
    await g.docListeners.DOMContentLoaded();
    check("stage in German", g.ui.jobRow(job({ stage: "joining picture and sound" })).includes("Bild und Ton zusammenfügen"));
    check("unknown stage as it is", g.ui.jobRow(job({ stage: "something new" })).includes("something new"));
  }

  // Log lines from Go with a key show in the page's language.
  for (const m of appGo.matchAll(/uiLogKey\("[a-z]+", "([^"]+)"/g)) check(`log text "${m[1]}" exists`, en.includes(m[1]));
  {
    const g = loadPage(backend({ SaveSettings: async () => {}, SetBrowserLinks: async () => {} }), "de-DE");
    await g.docListeners.DOMContentLoaded();
    g.events.log({ level: "info", text: "Link received: https://x.org/v", key: "log.received", vars: { url: "https://x.org/v" }, time: "14:00:00" });
    check("log line in German", g.el("log").innerHTML.includes("Link angekommen: https://x.org/v"), g.el("log").innerHTML);
    g.events.log({ level: "info", text: "Something new", key: "log.unknownKey", time: "14:00:01" });
    check("unknown key: the English text", g.el("log").innerHTML.includes("Something new"));
    g.ui.setLanguage("en");
    check("log follows a language switch", g.el("log").innerHTML.includes("Link received: https://x.org/v"));
    g.ui.init.settings.language = "auto";
    await g.el("set-save").click();
    check("page's own log line has a time", /<span class="time">\d\d:\d\d:\d\d<\/span>Einstellungen gespeichert\./.test(g.el("log").innerHTML), g.el("log").innerHTML);
  }

  // Errors from Go: message and hint on separate lines, button back.
  {
    const q = loadPage(backend({ Probe: async () => { throw "yt-dlp does not know this site or this kind of link\nCheck the link."; } }));
    await q.docListeners.DOMContentLoaded();
    q.el("link").value = "https://x.org";
    await q.el("lookup").click();
    check("error message", q.el("error-msg").textContent === "yt-dlp does not know this site or this kind of link");
    check("error hint", q.el("error-hint").textContent === "Check the link.");
    check("info hidden after an error", q.el("info").hidden === true);
    check("button enabled after an error", q.el("lookup").disabled === false);
  }

  // The preview is there before anything is looked up.
  {
    let asked = 0;
    const v = loadPage(backend({ Preview: async () => { asked++; return "D:\\Videos\\%(title)s.%(ext)s"; } }));
    await v.docListeners.DOMContentLoaded();
    await v.runTimers();
    check("preview on start", asked === 1 && v.el("preview").textContent.includes("%(title)s"));
    check("no own name without a looked-up video", v.el("opt-own-name").hidden === true);
  }
  check("baseName", p.baseName("D:\\V\\sub/Holiday [abc].mp4") === "Holiday [abc]" && p.baseName("noext") === "noext");

  // Audio only shows the audio fields and hides the container.
  p.el("quality").value = "audio";
  p.ui.updateVisibility();
  check("audio fields", p.el("opt-audio-format").hidden === false && p.el("opt-container").hidden === true);

  // German.
  {
    const g = loadPage(backend(), "de-DE");
    await g.docListeners.DOMContentLoaded();
    check("German texts", g.el("quality").innerHTML.includes("Nur Ton"));
  }

  // Outside the program (no Go side): a clear message, not a silent dead window.
  {
    const n = loadPage(null);
    await n.docListeners.DOMContentLoaded();
    check("no backend: error shown", n.el("error").hidden === false && n.el("error-msg").textContent.includes("not connected"));
  }

  done();
})();
