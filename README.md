# Skill Manager

Kleines persönliches Go-/Bubble-Tea-Tool für **ein Git-Repository** mit Skills.
Wenige Dateien in einem Package, ohne Services, eigene Interfaces oder Installationsmanifest.

## Start

Voraussetzungen: Go 1.27+, Git im PATH und ein Terminal.

```powershell
cd C:\_workspaces\skill-manager
go build -o skill-manager.exe .
.\skill-manager.exe
```

Beim ersten Start die Git-URL eingeben. Der vorhandene `origin` des Quellordners
wird vorgeschlagen. Im Entwicklungsordner `skill-manager` ist der Standard die
benachbarte `skill-library`, hier `C:\_workspaces\skill-library`.
Außerhalb dieses Ordners: `~/.local/share/skill-manager/skill-library`.

Alternativ die erste Einrichtung ohne Rückfrage starten:

```powershell
.\skill-manager.exe --repo https://github.com/roman-kiefer/skill-library.git --source C:\_workspaces\skill-library
```

macOS/Linux: `go build -o skill-manager .` und `./skill-manager --repo <Git-URL> --source /pfad/skill-library`.

Die Konfiguration liegt unter `os.UserConfigDir()/skill-manager/config.json`
(Windows: `%AppData%\skill-manager\config.json`, macOS: `~/Library/Application Support/skill-manager/config.json`,
Linux: `${XDG_CONFIG_HOME:-~/.config}/skill-manager/config.json`).
Sie enthält nur `url` und den absoluten `source`-Pfad. Eine vorhandene Datei wird nie
automatisch überschrieben. Für dauerhafte Änderungen selbst bearbeiten;
`--repo`, `--source` überschreiben Werte nur für den jeweiligen Start.
`--config <Pfad>` wählt eine andere Konfigurationsdatei.

## Bibliothek

```text
skill-library/
  skills/
    mein-skill/
      SKILL.md
      scripts/       # optional
```

Nur direkte Skill-Ordner mit regulärer `SKILL.md` werden erkannt, alphabetisch
nach Ordnernamen. Die komplette Skill-Struktur wird verlinkt. Das Verzeichnis `skills`
darf kein Link sein; verlinkte Skill-Ordner und verlinkte `SKILL.md` werden übersprungen.
Das Tool führt keine Skills aus.
Der Ordnername wird zum Installationsnamen; kein YAML-Parser und kein Manifest nötig.

Das derzeit leere Repository ist zulässig. Skills dort anlegen, committen und zum
Remote pushen. Bei lokalen uncommitteten Änderungen verweigert der Manager das Git-Update;
die lokale Auswahl und Installation bleiben verfügbar.

## Bedienung

| Taste | Aktion |
| --- | --- |
| ↑ / ↓ oder k / j | Skill auswählen |
| Leertaste | Markierung umschalten |
| a | Alle markieren / Markierungen aufheben |
| Tab | Installationsziel: Codex → Claude Code → beide |
| Enter / i | Markierte Skills installieren, sonst den aktuellen Skill |
| u | Git-Update und Status neu laden |
| r | Lokalen Status neu einlesen |
| q / Ctrl+C | Beenden |

Die Tabelle zeigt den Status für beide Ziele:

- **fehlt**: noch nicht installiert.
- **installiert**: Link zeigt auf den aktuellen Skill-Ordner.
- **Konflikt**: vorhandene Datei, Ordner, fremder oder defekter Link. Bleibt unverändert.
- **Fehler**: Ziel kann nicht geprüft werden, etwa wegen fehlender Rechte.

Codex: `~/.agents/skills/<name>`, Claude Code: `~/.claude/skills/<name>`.
Windows: zuerst Verzeichnis-Symlink, bei fehlenden Rechten automatisch lokale Junction.
Junctions benötigen keinen Entwicklermodus und keine Administratorrechte. Der Fallback
verweigert UNC-Pfade und Shell-Sonderzeichen; für solche Pfade Symlinks mit aktiviertem
Windows-Entwicklermodus verwenden. macOS/Linux verwenden Symlinks.

Die Quelle muss am selben Ort bleiben. Git-Updates ändern durch die Links sofort die
installierten Inhalte. Entfernte/umbenannte Skills können defekte Links hinterlassen;
dieses MVP löscht oder ersetzt keine Installationen. Solche Links bei Bedarf manuell entfernen.

## Git und Status

Beim TUI-Start und mit `u`: fehlende Quelle per `git clone` anlegen, bestehende Quelle
mit `git pull --ff-only origin HEAD` auf den Remote-Standardbranch aktualisieren.
Der Quellordner muss selbst ein Git-Checkout mit passendem `origin` sein.
Keine automatischen Resets, kein Stash, kein Merge bei auseinanderlaufender Historie.
Vorhandene Dateien außerhalb dieses Git-Updates werden nicht ersetzt.

Git nutzt vorhandene SSH-/Credential-Helper-Einstellungen. Terminal-Passwortabfragen
sind deaktiviert; die Authentifizierung vorher mit Git einrichten. Git-Aufrufe haben
ein Zeitlimit von zwei Minuten. Bei Netz-/Git-Fehlern zeigt die TUI weiterhin die
lokal vorhandenen Skills und den Fehler an.

```powershell
.\skill-manager.exe --status   # nur lokaler Status, auch offline
.\skill-manager.exe --update   # Git aktualisieren, Status ausgeben, beenden
go test ./...
go vet ./...
```

Tests verwenden ausschließlich temporäre Repositorys und Installationsordner.

## Programm-Updates

Die Programmversion steht im Kopf der TUI und wird mit `--version` ausgegeben.
Offizielle Release-Builds prüfen beim TUI-Start im Hintergrund auf ein neues stabiles
Release von `roman-kiefer/skill-manager`. Die Skill-Quelle ist davon unabhängig.
Die Prüfung benötigt keinen GitHub-Token. Offlinebetrieb und API-Limits blockieren
die Skill-Verwaltung nicht.

| Taste | Aktion |
| --- | --- |
| v | Manuell nach einer neuen Programmversion suchen |
| s | Verfügbares Programm-Update installieren; vorher mit j/y bestätigen |

Enter, n und Escape brechen die Bestätigung ab; die Standardantwort ist Nein.
Nach erfolgreicher Installation beendet sich das Tool. Bitte anschließend neu starten.
Das vorhandene `u` und `--update` aktualisieren weiterhin ausschließlich die Skills.

```powershell
.\skill-manager.exe --version
.\skill-manager.exe --check-update
.\skill-manager.exe --self-update
```

Diese drei Befehle funktionieren ohne eingerichtete Skill-Quelle. `--self-update`
fragt vor der Installation nach `[j/N]`; ohne ausdrückliche Zustimmung passiert nichts.
Die Programm-Betriebsarten `--version`, `--check-update` und `--self-update` sind
nicht miteinander oder mit `--status`/`--update` kombinierbar.

Nur stabile Tags im Format `vMAJOR.MINOR.PATCH` werden unterstützt. Gleiche und ältere
Versionen werden nicht installiert. Lokale Builds ohne Versionsflag zeigen `dev`,
prüfen nicht automatisch und ersetzen sich nicht selbst. `--check-update` bzw. `v`
zeigt bei solchen Builds das aktuelle Release und die Release-Seite an.

Updates laden das Archiv für das laufende Betriebssystem und die Architektur herunter,
prüfen dessen SHA256 gegen die Release-Datei `SHA256SUMS` und lesen ausschließlich die
erwartete Programmdatei aus dem Archiv. Prüfung: fünf Sekunden Zeitlimit; Download:
zwei Minuten. Download und entpackte Daten sind auf 100 MiB begrenzt.

Der Programmordner muss beschreibbar sein. Das Tool erhöht keine Rechte. Die kleine
Bibliothek `minio/selfupdate` übernimmt den Dateiaustausch und versucht bei Fehlern,
die alte Datei wiederherzustellen. Vorhandene fremde Hilfsdateien werden nicht ersetzt.
Unter Windows kann eine Datei `.skill-manager.exe.backup-*` nach erfolgreichem Update
bis zum Ende des laufenden Prozesses gesperrt bleiben; danach kann sie gelöscht werden.
Nach einem Prozessabbruch kann `.skill-manager[.exe].update-lock` liegen bleiben.
Diese Datei erst entfernen, wenn kein Updateprozess mehr läuft.

## Version veröffentlichen

Die GitHub Action `.github/workflows/build.yml` läuft ausschließlich für gepushte
Versionstags. Ein zusätzlicher Formatcheck verweigert Vorabversionen und ungültige Tags.

```powershell
git tag v1.0.0
git push origin v1.0.0
```

Die Action führt Tests und `go vet` auf Windows, Linux und macOS aus, bettet den Tag
in die Programme ein und erstellt `skill-manager-<os>-<arch>.tar.gz` mit Programm und
README. Nach Erfolg aller drei Builds veröffentlicht sie die Archive und `SHA256SUMS`
als GitHub Release. Ein Release-Entwurf verhindert die Anzeige unvollständiger Updates.
Bereits veröffentlichte Releases werden nicht überschrieben. Ein fehlgeschlagener
Upload kann durch erneutes Ausführen des Workflows im vorhandenen Entwurf fortgesetzt werden.
Nur der Veröffentlichungsjob hat Schreibrechte.

Release-Dateien bleiben auf der GitHub-Releases-Seite verfügbar; die zusätzlichen
Actions-Artefakte werden 30 Tage aufbewahrt. Unterstützt werden die Betriebssysteme
und Architekturen, für die der jeweilige Release tatsächlich ein Archiv enthält.

Der erste Release muss manuell installiert werden. Ab dem zweiten Release ist die
Aktualisierung über das Tool möglich. Der Ende-zu-Ende-Test baut zwei Programmversionen
in temporären Ordnern und lässt eine laufende Testkopie sich selbst aktualisieren;
die installierte Anwendung wird dabei nicht verändert. Dieser Test läuft auch in CI.
`go test -short ./...` lässt nur diesen aufwendigeren Test aus.

## Lizenz

Skill Manager steht unter der GNU General Public License Version 3 (GPLv3).
Den vollständigen Lizenztext enthält [LICENSE](LICENSE). Die Lizenzdatei wird auch
mit den Release-Paketen ausgeliefert. Die verwendeten Go-Abhängigkeiten behalten
ihre jeweiligen Lizenzen.
