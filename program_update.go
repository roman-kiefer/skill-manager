package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

var version = "dev"

const releaseAPI = "https://api.github.com/repos/roman-kiefer/skill-manager/releases/latest"
const releasesPage = "https://github.com/roman-kiefer/skill-manager/releases"
const maxUpdateBytes int64 = 100 << 20

var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var errNoRelease = errors.New("noch kein veröffentlichtes Release vorhanden")
var errDownloadNotFound = errors.New("Download nicht gefunden (HTTP 404)")

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type programUpdate struct {
	Version   string
	Available bool
	Archive   releaseAsset
	Checksums releaseAsset
}

// Decimal components are compared by length first, without an integer-size limit.
func newerVersion(candidate, current string) bool {
	a, b := stableVersion.FindStringSubmatch(candidate), stableVersion.FindStringSubmatch(current)
	if a == nil || b == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		if len(a[i]) != len(b[i]) {
			return len(a[i]) > len(b[i])
		}
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func checkProgramUpdate(ctx context.Context, client *http.Client, endpoint, current, goos, goarch string) (programUpdate, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := fetchBytes(ctx, client, endpoint, 1<<20)
	if errors.Is(err, errDownloadNotFound) {
		return programUpdate{}, errNoRelease
	}
	if err != nil {
		return programUpdate{}, err
	}
	var release struct {
		Tag        string         `json:"tag_name"`
		Draft      bool           `json:"draft"`
		Prerelease bool           `json:"prerelease"`
		Assets     []releaseAsset `json:"assets"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return programUpdate{}, fmt.Errorf("ungültige Release-Antwort: %w", err)
	}
	if release.Draft || release.Prerelease || !stableVersion.MatchString(release.Tag) {
		return programUpdate{}, errors.New("kein stabiles Release im Format vMAJOR.MINOR.PATCH")
	}
	update := programUpdate{Version: release.Tag, Available: newerVersion(release.Tag, current)}
	if !update.Available {
		return update, nil
	}
	name := "skill-manager-" + goos + "-" + goarch + ".tar.gz"
	for _, a := range release.Assets {
		switch a.Name {
		case name:
			if update.Archive.Name != "" {
				return programUpdate{}, errors.New("mehrdeutiges Release-Archiv")
			}
			update.Archive = a
		case "SHA256SUMS":
			if update.Checksums.Name != "" {
				return programUpdate{}, errors.New("mehrdeutige Prüfsummendatei")
			}
			update.Checksums = a
		}
	}
	if update.Archive.URL == "" || update.Checksums.URL == "" {
		return programUpdate{}, fmt.Errorf("Release-Dateien für %s/%s fehlen", goos, goarch)
	}
	// Production assets must belong to this repository and this exact release.
	if endpoint == releaseAPI {
		base := releasesPage + "/download/" + release.Tag + "/"
		for _, a := range []releaseAsset{update.Archive, update.Checksums} {
			if a.URL != base+a.Name {
				return programUpdate{}, errors.New("Release enthält einen unerwarteten Download-Link")
			}
		}
	}
	return update, nil
}

func fetchBytes(ctx context.Context, client *http.Client, address string, limit int64) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("ungültiger Download-Link")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "skill-manager/"+version)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errDownloadNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Download fehlgeschlagen: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, errors.New("Download überschreitet die Größenbegrenzung")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("Download überschreitet die Größenbegrenzung")
	}
	return data, nil
}

func archiveChecksum(data []byte, name string) ([]byte, error) {
	var sum []byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if sum != nil {
			return nil, errors.New("mehrdeutige Archiv-Prüfsumme")
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return nil, errors.New("ungültige SHA256-Prüfsumme")
		}
		sum = decoded
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if sum == nil {
		return nil, errors.New("Archiv-Prüfsumme fehlt")
	}
	return sum, nil
}

func downloadProgram(ctx context.Context, client *http.Client, update programUpdate, goos string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	checksums, err := fetchBytes(ctx, client, update.Checksums.URL, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("Prüfsummen: %w", err)
	}
	expected, err := archiveChecksum(checksums, update.Archive.Name)
	if err != nil {
		return nil, err
	}
	archive, err := fetchBytes(ctx, client, update.Archive.URL, maxUpdateBytes)
	if err != nil {
		return nil, fmt.Errorf("Archiv: %w", err)
	}
	actual := sha256.Sum256(archive)
	if !bytes.Equal(expected, actual[:]) {
		return nil, errors.New("Archiv-Prüfsumme stimmt nicht; Update abgebrochen")
	}
	return unpackProgram(archive, strings.TrimSuffix(update.Archive.Name, ".tar.gz"), goos)
}

func unpackProgram(archive []byte, directory, goos string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	// Bound all decompression, including skipped entries, to prevent archive bombs.
	limited := &io.LimitedReader{R: gz, N: maxUpdateBytes + 1}
	tr := tar.NewReader(limited)
	binaryName := "skill-manager"
	if goos == "windows" {
		binaryName += ".exe"
	}
	wanted := directory + "/" + binaryName
	var binary []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ungültiges Archiv: %w", err)
		}
		if hdr.Name != wanted {
			continue
		}
		if binary != nil || hdr.Typeflag != tar.TypeReg || hdr.Size <= 0 || hdr.Size > maxUpdateBytes {
			return nil, errors.New("ungültige Programmdatei im Archiv")
		}
		binary, err = io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
	}
	// Also read the gzip trailer, so truncated/corrupt streams are rejected.
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return nil, err
	}
	if limited.N <= 0 {
		return nil, errors.New("entpacktes Archiv überschreitet 100 MiB")
	}
	if len(binary) == 0 {
		return nil, errors.New("erwartete Programmdatei fehlt im Archiv")
	}
	return binary, nil
}

func installProgram(update programUpdate) error {
	if !stableVersion.MatchString(version) {
		return errors.New("Entwicklungsbuilds aktualisieren sich nicht selbst; bitte ein Release installieren")
	}
	if !update.Available || !newerVersion(update.Version, version) {
		return errors.New("keine neuere Programmversion ausgewählt")
	}
	binary, err := downloadProgram(context.Background(), http.DefaultClient, update, runtime.GOOS)
	if err != nil {
		return err
	}
	target, err := os.Executable()
	if err != nil {
		return err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	return replaceProgram(target, binary, selfupdate.CommitBinary)
}

// The commit function permits exercising rollback without touching a running tool.
func replaceProgram(target string, binary []byte, commit func(selfupdate.Options) error) error {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("Programmziel ist keine reguläre Datei")
	}
	if len(binary) == 0 || int64(len(binary)) > maxUpdateBytes {
		return errors.New("ungültige Größe der Programmdatei")
	}
	dir, base := filepath.Dir(target), filepath.Base(target)
	lock, err := os.OpenFile(filepath.Join(dir, "."+base+".update-lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("Update nicht möglich (Schreibrechte oder laufendes Update): %w", err)
	}
	defer func() { lock.Close(); os.Remove(lock.Name()) }()
	newPath := filepath.Join(dir, "."+base+".new")
	if _, err := os.Lstat(newPath); !os.IsNotExist(err) {
		return fmt.Errorf("temporärer Update-Pfad %s ist belegt oder nicht prüfbar", newPath)
	}
	backup, err := os.CreateTemp(dir, "."+base+".backup-*")
	if err != nil {
		return fmt.Errorf("keine Schreibrechte im Programmordner: %w", err)
	}
	backup.Close()
	sum := sha256.Sum256(binary)
	opts := selfupdate.Options{TargetPath: target, TargetMode: info.Mode().Perm(), OldSavePath: backup.Name(), Checksum: sum[:]}
	err = selfupdate.PrepareAndCheckBinary(bytes.NewReader(binary), opts)
	if err == nil {
		err = commit(opts)
	}
	if rollbackErr := selfupdate.RollbackError(err); rollbackErr != nil {
		return fmt.Errorf("Update und Wiederherstellung fehlgeschlagen: %v; %v. Alte Programmdatei: %s", err, rollbackErr, backup.Name())
	}
	os.Remove(newPath)
	// On Windows the running old executable may remain locked until process exit.
	os.Remove(backup.Name())
	if err != nil {
		return fmt.Errorf("Programm-Update fehlgeschlagen: %w", err)
	}
	return nil
}

func programUpdateText(update programUpdate, current string) string {
	if !stableVersion.MatchString(current) {
		return fmt.Sprintf("Entwicklungsbuild %s; aktuelles Release: %s (%s)", current, update.Version, releasesPage)
	}
	if update.Available {
		return fmt.Sprintf("Programm-Update verfügbar: %s → %s", current, update.Version)
	}
	return fmt.Sprintf("Programm %s ist aktuell.", current)
}

func runProgramUpdate(install bool, in io.Reader, out io.Writer) error {
	if install && !stableVersion.MatchString(version) {
		return errors.New("Entwicklungsbuilds aktualisieren sich nicht selbst; bitte ein Release installieren")
	}
	update, err := checkProgramUpdate(context.Background(), http.DefaultClient, releaseAPI, version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, programUpdateText(update, version))
	if !install || !update.Available {
		return nil
	}
	fmt.Fprintf(out, "Programm von %s auf %s aktualisieren? [j/N]: ", version, update.Version)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	if !confirmProgramUpdate(answer) {
		fmt.Fprintln(out, "Abgebrochen.")
		return nil
	}
	if err := installProgram(update); err != nil {
		return err
	}
	fmt.Fprintln(out, "Programm-Update installiert. Bitte Skill Manager neu starten.")
	return nil
}

func confirmProgramUpdate(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "j", "ja", "y", "yes":
		return true
	}
	return false
}
