# Anleitung für KI in diesem Projekt

Gilt für jede KI, die hier arbeitet (`AGENTS.md` zeigt hierher). Diese Datei ist öffentlich: nur Dinge hineinschreiben, die jeder lesen darf. Private Notizen gehören nach `_notizen/` (wird nicht eingecheckt).

## Grundlage

- Dieses Projekt folgt dem **dev-starterkit** (`burnersen/dev-starterkit`, privat). Kit-Stand beim Anlegen: **1.1**, Vorlage `go-wails`. Vor größerer Arbeit das Kit holen und die passenden Leitlinien lesen (`03` bis `09`). Ist das Kit neuer als der Stand oben, kurz sagen, was sich laut `AENDERUNGEN-*.md` geändert hat.
- Antworte auf Deutsch, locker, ohne Gedankenstriche. Erst sagen, wie du den Auftrag verstanden hast, bei allem Größeren auf Okay warten.
- Befehle, die der Nutzer selbst ausführt: als ganzer Block zum Kopieren, mit Sicherung vorher und dem Befehl zum Zurückholen (`01-arbeitsweise.md`).

## Steckbrief

| | |
|---|---|
| Zweck | A modern window and download manager for yt-dlp: catch videos from almost any site, by link or straight from the browser. |
| Sichtbarkeit | erst privat, später öffentlich (deshalb von Anfang an wie öffentlich gebaut) |
| Sprache Kommentare, Commits, Meldungen | Englisch |
| Plattformen | Windows x64 und ARM64 (Fenster mit Wails v2 und WebView2); Linux später, Logik dafür schon plattformneutral |
| Lizenz | GPL-3.0-only |

## Befehle

```
gofmt -l .                      # muss leer bleiben
GOOS=windows go vet ./...
go test -race -count=1 ./...
node frontend/app_check.js      # Seite ohne Browser prüfen (lädt i18n.js, app.js und jobs.js)
node extension/extension_check.js   # Erweiterung ohne Browser prüfen
node extension/build.js 1.2.3   # Erweiterung bauen: extension/dist/chromium und extension/dist/firefox
wails build -platform windows/amd64 -skipbindings -trimpath   # nur unter Windows
```

Release: Tag `v1.2.3` pushen, vorher `.github/release-notes/1.2.3.md` schreiben (`07-build-und-release.md`).

## Aufbau

Siehe Kopfkommentar in `main.go` (Liste aller Dateien). Kurz: `queue.go` ist der Download-Manager (ein yt-dlp-Lauf je Auftrag), `options.go` macht aus den Einstellungen die Argumentliste, `toolbox.go`/`tools.go`/`ytdlp.go` holen und prüfen yt-dlp, FFmpeg und Deno, `intake.go` nimmt Links von außen an (Format in `docs/browser-bridge.md`). Die Logik ist frei von Wails und läuft in den Tests unter Linux; nur `app.go` und `main.go` brauchen Wails. Jede Methode in `app.go` gibt Fehler nur über `uiError` an die Seite (erste Zeile Meldung, zweite Zeile Hinweis), die technische Ursache geht ins Log. Ein Fehler beim Start zeigt ein Meldungsfenster (`showFatal`), nie `log.Fatal`. Die Seite liegt in `frontend/dist` (HTML, CSS, JS getrennt, kein npm; `app.js` ist das Fenster, `jobs.js` die Liste der Downloads und hängt sich an dasselbe `ui`-Objekt), geprüft wird sie mit `frontend/*_check.js`. Die Browser-Erweiterung liegt in `extension/src` (Manifest V3, eine Codebasis, `build.js` macht daraus die Fassung für Chrome/Edge und für Firefox); sie spricht über Native Messaging mit `VideoCatcher.exe` selbst (`nativehost.go`, ohne Fenster). Videos findet sie auf drei Wegen (Tabelle in `docs/browser-bridge.md`): `background.js` sieht die Anfragen, `page.js` läuft in der Seite selbst (world `MAIN`) und liest den Anfang der Antworten von `fetch` und `XMLHttpRequest`, `content.js` liest das Markup und reicht alles weiter. Was aus einer Seite kommt, gilt als Eingabe von außen und wird in `pageItems` (`common.js`) geprüft. Die Chrome-ID hängt am `key` im Manifest, `TestExtensionIDMatchesKey` prüft das. Skripte, die zusammen geladen werden (`common.js` mit `background.js` oder `popup.js`), teilen einen globalen Namensraum: `common.js` hält alles in einer Funktion. `logging.go`, `paths.go` und `files.go` sind dieselben wie in der Vorlage `go-cli`. Seit 0.3.0: `speed.go` ist der Tempo-Regler, ein kleiner Proxy nur auf 127.0.0.1 mit einem Passwort je Start, den nur yt-dlp bekommt (`--proxy`); ohne Passwort antwortet er nichts. Das ist der einzige offene Port, die Erweiterung braucht keinen. `names.go` räumt Dateinamen auf (Regel wie in NVENCForge), `afterall.go` macht "wenn alle fertig", `tray_windows.go` das Symbol im Infobereich (reines Win32, Wails v2 hat keins). Pausierte Aufträge behalten ihre Teile in `partial\<job>`. Seit 0.5.0: Ein Auftrag, dessen Seite nicht erreichbar war, wartet und versucht es von selbst nochmal (`retryDelays` in `queue.go`; welche Fehler das sind, steht als `again` an der Regel in `ytdlp_errors.go`). Eine Playlist mit einzelnen kaputten Einträgen endet als fertig mit `Skipped`. `notify.go` entscheidet, wann das Tray-Symbol etwas meldet, `appupdate.go` fragt nach einer neueren Version (nur Hinweis mit Link, die Adresse baut Go selbst), `awake` in `power_*.go` hält den PC wach, solange etwas läuft oder wartet. `OpenFile` in `app.go` reicht nur Video- und Audiodateien an Windows weiter (`playable`). Seit 0.6.0 (Seiten mit Token): Die Erweiterung merkt sich zu jedem gefundenen Video die Kopfzeilen der echten Anfrage (`sentHeaders` in `common.js`) und schickt sie als `headers` mit. `headers.go` prüft sie nochmal (`splitHeaders`, dieselbe Auslassliste wie in `common.js`, ein Check vergleicht beide): Der User-Agent bleibt am Auftrag und geht als Argument an yt-dlp, alle anderen sind Anmeldungen wie Cookies und gehen als Datei im Cookie-Ordner über `--config-locations` (nie Befehlszeile, Liste oder Log). Lehnt die Seite den Link ab (`linkRefused` in `ytdlp_errors.go`), versucht der Auftrag einmal die Seite, auf der er gefunden wurde (`ViaPage` in `queue.go`). `cookiesFor` fragt auch nach den Cookies des Player-Frames, nach abgetrennten (partitioned) Cookies und im Cookie-Speicher des Tabs (privates Fenster, Firefox-Container). Seit 0.7.0 (Seiten hinter einer Bot-Prüfung): Weist die Seite yt-dlp ab (403 oder Cloudflare im Fehlertext, `wantsBrowser` in `ytdlp_errors.go`), läuft der Auftrag einmal neu mit `--impersonate` (`AsBrowser` in `queue.go`, Ziel aus dem User-Agent, `browserTarget` in `options.go`), erst danach kommt `ViaPage`; kann yt-dlp das nicht, merkt sich der Auftrag `NoBrowser`. Die Meldung am Ende baut `explainEnd` aus den Zeilen aller Versuche. Die Erweiterung schickt die Cookies der Bot-Prüfung immer mit (`withPass`, `passCookie` in `common.js`), alle anderen nur mit Haken. "Bericht kopieren" im Popup (`reportText`, Links gekürzt mit `brief`) zeigt, was die Erweiterung auf einer Seite gesehen hat; `page.js` hängt sich auch in die eigenen Frames einer Seite (`watchFrames`) und notiert, wie der Player an sein Video kommt. Seit 0.8.0 (Streams ohne Link): Baut ein Player seine HLS-Liste im Speicher der Seite (blob, Qualitäten als `data:`), reicht `page.js` den Text weiter (`fromMemory`), `pageItems` macht daraus einen Eintrag mit `via: "memory"`, geschickt wird er als `list` mit der Player-Seite als `url`. `memlist.go` prüft die Liste (`writeList`): heraus kommen nur Web-Links und Dateien, die es selbst für diesen Auftrag in den Cookie-Ordner geschrieben hat, alles andere wird abgelehnt; nur so ein Auftrag bekommt `--enable-file-urls` (`ListNames` in `queue.go`). Einen Schlüssel aus dem Player holt es nie (`errListKey`). Kann eine Anfrage der Erweiterung nicht zum Auftrag werden, steht der Grund als `Problem` an der Anfrage und `accept` zeigt ihn im Fenster. Seit 0.9.1: Sind die Teile so einer Liste verschlüsselt, ohne dass die Liste es sagt, merkt `intake.go` das am Anfang des ersten Teils (`peekPart`, `looksLocked` in `memlist.go`) und lehnt die Anfrage ab; scheitert erst FFmpeg daran, sagt `queue.go` dasselbe (`lockedMsg`). Die Fenstergröße steht als `window` in der INI und wird in `main.go` gelesen, bevor es das Fenster gibt (`savedWindow`, ändert die Datei nie).

## Bewusste Abweichungen vom Kit

- **S4, yt-dlp ohne festgenagelte Prüfsumme** (`projekt-pruefen.sh` meldet es als WARNUNG): yt-dlp muss neu sein, sonst funktionieren die Seiten nicht mehr. Deshalb die neueste stabile Version, aber nur, wenn ihre Prüfsummenliste mit dem eingebauten yt-dlp-Schlüssel signiert ist (Fingerabdruck in `ytdlp.go`) und die Datei zur Liste passt. Ablauf und Tests: `ytdlp.go`, `pgp.go`, `tools_test.go`. FFmpeg und Deno sind wie üblich in `runtime.json` festgenagelt.

## Live-Test

`live_test.go` lädt die echten Programme und macht einen echten Download. Er läuft nur mit `VIDEOCATCHER_TEST_LIVE=1`, im Workflow "Live test" auf GitHub (von Hand, wöchentlich und bei Änderungen an den Download-Teilen). In einer Cloud-Sitzung dürfen heruntergeladene Programme nicht laufen, dort also nie.

## Vor jedem Commit

Die Checklisten aus `03` bis `09` des Kits durchgehen, mindestens: gofmt leer, vet und Tests grün, kein neues `_ =` ohne Kommentar, keine Geheimnisse und privaten Pfade, README und Release-Notes nachgezogen, wenn sich für Nutzer etwas ändert. Danach `bash <kit>/werkzeuge/projekt-pruefen.sh .`

## Das Kit lernt mit

Ergibt sich etwas, das auch anderen Projekten hilft (Fehler, der wiederkommen kann, gute Lösung, Bibliothek mit gutem oder schlechtem Ergebnis, Build-Trick, bessere Regel), am Ende des Arbeitsschritts in einem Satz fragen: "Das wäre was fürs Kit: <worum es geht>. Übernehmen?" Ablauf in `21-kit-lernt-mit.md` des Kits.
