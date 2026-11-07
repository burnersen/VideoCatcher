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
node frontend/app_check.js      # Seite ohne Browser prüfen (lädt i18n.js und app.js)
wails build -platform windows/amd64 -skipbindings -trimpath   # nur unter Windows
```

Release: Tag `v1.2.3` pushen, vorher `.github/release-notes/1.2.3.md` schreiben (`07-build-und-release.md`).

## Aufbau

Siehe Kopfkommentar in `main.go` (Liste aller Dateien). Kurz: `queue.go` ist der Download-Manager (ein yt-dlp-Lauf je Auftrag), `options.go` macht aus den Einstellungen die Argumentliste, `toolbox.go`/`tools.go`/`ytdlp.go` holen und prüfen yt-dlp, FFmpeg und Deno, `intake.go` nimmt Links von außen an (Format in `docs/browser-bridge.md`). Die Logik ist frei von Wails und läuft in den Tests unter Linux; nur `app.go` und `main.go` brauchen Wails. Jede Methode in `app.go` gibt Fehler nur über `uiError` an die Seite (erste Zeile Meldung, zweite Zeile Hinweis), die technische Ursache geht ins Log. Ein Fehler beim Start zeigt ein Meldungsfenster (`showFatal`), nie `log.Fatal`. Die Seite liegt in `frontend/dist` (HTML, CSS, JS getrennt, kein npm), geprüft wird sie mit `frontend/*_check.js`. `logging.go`, `paths.go` und `files.go` sind dieselben wie in der Vorlage `go-cli`.

## Bewusste Abweichungen vom Kit

- **S4, yt-dlp ohne festgenagelte Prüfsumme** (`projekt-pruefen.sh` meldet es als WARNUNG): yt-dlp muss neu sein, sonst funktionieren die Seiten nicht mehr. Deshalb die neueste stabile Version, aber nur, wenn ihre Prüfsummenliste mit dem eingebauten yt-dlp-Schlüssel signiert ist (Fingerabdruck in `ytdlp.go`) und die Datei zur Liste passt. Ablauf und Tests: `ytdlp.go`, `pgp.go`, `tools_test.go`. FFmpeg und Deno sind wie üblich in `runtime.json` festgenagelt.

## Live-Test

`live_test.go` lädt die echten Programme und macht einen echten Download. Er läuft nur mit `VIDEOCATCHER_TEST_LIVE=1`, im Workflow "Live test" auf GitHub (von Hand, wöchentlich und bei Änderungen an den Download-Teilen). In einer Cloud-Sitzung dürfen heruntergeladene Programme nicht laufen, dort also nie.

## Vor jedem Commit

Die Checklisten aus `03` bis `09` des Kits durchgehen, mindestens: gofmt leer, vet und Tests grün, kein neues `_ =` ohne Kommentar, keine Geheimnisse und privaten Pfade, README und Release-Notes nachgezogen, wenn sich für Nutzer etwas ändert. Danach `bash <kit>/werkzeuge/projekt-pruefen.sh .`

## Das Kit lernt mit

Ergibt sich etwas, das auch anderen Projekten hilft (Fehler, der wiederkommen kann, gute Lösung, Bibliothek mit gutem oder schlechtem Ergebnis, Build-Trick, bessere Regel), am Ende des Arbeitsschritts in einem Satz fragen: "Das wäre was fürs Kit: <worum es geht>. Übernehmen?" Ablauf in `21-kit-lernt-mit.md` des Kits.
