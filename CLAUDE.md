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
| Sichtbarkeit | öffentlich |
| Sprache Kommentare, Commits, Meldungen | Englisch |
| Plattformen | Windows x64 und ARM64 (Fenster mit Wails v2 und WebView2) |
| Lizenz | GPL-3.0-only |

## Befehle

```
gofmt -l .                      # muss leer bleiben
GOOS=windows go vet ./...
go test -race -count=1 ./...
node frontend/app_check.js      # Seite ohne Browser prüfen
wails build -platform windows/amd64 -skipbindings -trimpath   # nur unter Windows
```

Release: Tag `v1.2.3` pushen, vorher `.github/release-notes/1.2.3.md` schreiben (`07-build-und-release.md`).

## Aufbau

Siehe Kopfkommentar in `main.go`. Jede Methode in `app.go` gibt Fehler nur über `uiError` an die Seite (erste Zeile Meldung, zweite Zeile Hinweis), die technische Ursache geht ins Log. Ein Fehler beim Start zeigt ein Meldungsfenster (`showFatal`), nie `log.Fatal`. Die Seite liegt in `frontend/dist` (HTML, CSS, JS getrennt, kein npm), geprüft wird sie mit `frontend/*_check.js`. `logging.go`, `paths.go` und `files.go` sind dieselben wie in der Vorlage `go-cli`.

## Vor jedem Commit

Die Checklisten aus `03` bis `09` des Kits durchgehen, mindestens: gofmt leer, vet und Tests grün, kein neues `_ =` ohne Kommentar, keine Geheimnisse und privaten Pfade, README und Release-Notes nachgezogen, wenn sich für Nutzer etwas ändert. Danach `bash <kit>/werkzeuge/projekt-pruefen.sh .`

## Das Kit lernt mit

Ergibt sich etwas, das auch anderen Projekten hilft (Fehler, der wiederkommen kann, gute Lösung, Bibliothek mit gutem oder schlechtem Ergebnis, Build-Trick, bessere Regel), am Ende des Arbeitsschritts in einem Satz fragen: "Das wäre was fürs Kit: <worum es geht>. Übernehmen?" Ablauf in `21-kit-lernt-mit.md` des Kits.
