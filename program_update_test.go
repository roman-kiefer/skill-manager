package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/minio/selfupdate"
)

func setTestVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		next, current string
		want          bool
	}{
		{"v1.10.0", "v1.9.9", true}, {"v2.0.0", "v1.99.99", true},
		{"v1.0.1", "v1.0.0", true}, {"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v2.0.0", false}, {"v1.0.0-rc.1", "v0.9.0", false},
		{"v01.2.3", "v0.0.0", false}, {"v1.2", "v0.0.0", false},
		{"1.2.3", "v0.0.0", false}, {"v1.0.0", "dev", false},
		{"v9999999999999999999999.0.0", "v1.0.0", true},
	} {
		if got := newerVersion(tc.next, tc.current); got != tc.want {
			t.Errorf("%s > %s: %v", tc.next, tc.current, got)
		}
	}
}

func testArchive(t *testing.T, entries []*tar.Header, contents [][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i, hdr := range entries {
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(contents[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func binaryArchive(t *testing.T, binary []byte, goos, goarch string) []byte {
	t.Helper()
	name := "skill-manager-" + goos + "-" + goarch + "/skill-manager"
	if goos == "windows" {
		name += ".exe"
	}
	return testArchive(t, []*tar.Header{{Name: name, Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(binary))}}, [][]byte{binary})
}

func releaseFixture(t *testing.T, archive []byte, checksumOverride string) (*httptest.Server, programUpdate) {
	t.Helper()
	name := "skill-manager-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%x  %s\n", sum, name)
	if checksumOverride != "" {
		checksums = checksumOverride
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.0", "assets": []releaseAsset{{name, server.URL + "/archive"}, {"SHA256SUMS", server.URL + "/sums"}}})
		case "/archive":
			w.Write(archive)
		case "/sums":
			io.WriteString(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	update, err := checkProgramUpdate(context.Background(), server.Client(), server.URL+"/latest", "v1.0.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	return server, update
}

func TestReleaseCheckAndDownload(t *testing.T) {
	binary := []byte("new executable")
	server, update := releaseFixture(t, binaryArchive(t, binary, runtime.GOOS, runtime.GOARCH), "")
	if !update.Available || update.Version != "v1.2.0" {
		t.Fatalf("%+v", update)
	}
	got, err := downloadProgram(context.Background(), server.Client(), update, runtime.GOOS)
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("%q, %v", got, err)
	}
	for _, current := range []string{"dev", "v1.2.0", "v2.0.0"} {
		got, err := checkProgramUpdate(context.Background(), server.Client(), server.URL+"/latest", current, runtime.GOOS, runtime.GOARCH)
		if err != nil || got.Available {
			t.Fatalf("current=%s: %+v %v", current, got, err)
		}
	}
	if _, err := checkProgramUpdate(context.Background(), server.Client(), server.URL+"/latest", "v1.0.0", "unsupported", "unsupported"); err == nil {
		t.Fatal("missing platform accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := checkProgramUpdate(ctx, server.Client(), server.URL+"/latest", "v1.0.0", runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("cancelled check succeeded")
	}
}

func TestReleaseErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"no release", 404, ""}, {"forbidden", 403, ""}, {"rate limit", 429, ""},
		{"bad json", 200, "{"}, {"prerelease", 200, `{"tag_name":"v1.1.0-rc.1","prerelease":true}`},
		{"draft", 200, `{"tag_name":"v1.1.0","draft":true}`},
		{"missing assets", 200, `{"tag_name":"v1.1.0"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
			defer server.Close()
			if _, err := checkProgramUpdate(context.Background(), server.Client(), server.URL, "v1.0.0", runtime.GOOS, runtime.GOARCH); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	if _, err := checkProgramUpdate(context.Background(), server.Client(), server.URL, "v1.0.0", runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("offline check succeeded")
	}
}

func TestDownloadFailuresAndLimits(t *testing.T) {
	archive := binaryArchive(t, []byte("candidate"), runtime.GOOS, runtime.GOARCH)
	server, update := releaseFixture(t, archive, strings.Repeat("0", 64)+"  skill-manager-"+runtime.GOOS+"-"+runtime.GOARCH+".tar.gz\n")
	if _, err := downloadProgram(context.Background(), server.Client(), update, runtime.GOOS); err == nil {
		t.Fatal("bad checksum accepted")
	}
	server2, update2 := releaseFixture(t, archive, "")
	update2.Archive.URL = server2.URL + "/missing"
	if _, err := downloadProgram(context.Background(), server2.Client(), update2, runtime.GOOS); err == nil {
		t.Fatal("missing download accepted")
	}
	if _, err := fetchBytes(context.Background(), server2.Client(), server2.URL+"/archive", 4); err == nil {
		t.Fatal("oversized response accepted")
	}
	for _, sums := range []string{"", "bogus  file", strings.Repeat("0", 64) + "  file\n" + strings.Repeat("1", 64) + "  file\n"} {
		if _, err := archiveChecksum([]byte(sums), "file"); err == nil {
			t.Fatalf("bad sums accepted: %q", sums)
		}
	}
}

func TestUnpackOnlyExpectedRegularBinary(t *testing.T) {
	directory := "skill-manager-linux-amd64"
	for _, name := range []string{"../skill-manager", directory + "/../skill-manager", directory + "/other", "skill-manager.exe"} {
		archive := testArchive(t, []*tar.Header{{Name: name, Typeflag: tar.TypeReg, Mode: 0755, Size: 1}}, [][]byte{[]byte("x")})
		if _, err := unpackProgram(archive, directory, "linux"); err == nil {
			t.Fatalf("unexpected file accepted: %s", name)
		}
	}
	for _, typeflag := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeDir} {
		archive := testArchive(t, []*tar.Header{{Name: directory + "/skill-manager", Typeflag: typeflag, Linkname: "outside", Mode: 0755}}, [][]byte{nil})
		if _, err := unpackProgram(archive, directory, "linux"); err == nil {
			t.Fatal("nonregular file accepted")
		}
	}
	archive := binaryArchive(t, []byte("x"), "linux", "amd64")
	if _, err := unpackProgram(archive[:len(archive)-5], directory, "linux"); err == nil {
		t.Fatal("truncated gzip accepted")
	}
	duplicate := testArchive(t, []*tar.Header{
		{Name: directory + "/skill-manager", Typeflag: tar.TypeReg, Mode: 0755, Size: 1},
		{Name: directory + "/skill-manager", Typeflag: tar.TypeReg, Mode: 0755, Size: 1},
	}, [][]byte{[]byte("a"), []byte("b")})
	if _, err := unpackProgram(duplicate, directory, "linux"); err == nil {
		t.Fatal("duplicate binary accepted")
	}
}

func TestExecutableReplacementAndRollback(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprint("rollback=", rollback), func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "program")
			if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
				t.Fatal(err)
			}
			commit := selfupdate.CommitBinary
			if rollback {
				commit = func(opts selfupdate.Options) error {
					if err := os.Remove(filepath.Join(dir, ".program.new")); err != nil {
						t.Fatal(err)
					}
					return selfupdate.CommitBinary(opts)
				}
			}
			err := replaceProgram(target, []byte("new"), commit)
			if (err != nil) != rollback {
				t.Fatalf("%v", err)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			want := "new"
			if rollback {
				want = "old"
			}
			if string(data) != want {
				t.Fatalf("%s != %s", data, want)
			}
		})
	}
	t.Run("existing helper file", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "program")
		writeTestFile(t, target, "old")
		writeTestFile(t, filepath.Join(dir, ".program.new"), "keep")
		if err := replaceProgram(target, []byte("new"), selfupdate.CommitBinary); err == nil {
			t.Fatal("existing helper file replaced")
		}
		data, _ := os.ReadFile(filepath.Join(dir, ".program.new"))
		if string(data) != "keep" {
			t.Fatal("helper file changed")
		}
	})
}

func TestProgramCLIWithoutSkillConfiguration(t *testing.T) {
	setTestVersion(t, "dev")
	config := filepath.Join(t.TempDir(), "must-not-exist.json")
	for _, args := range [][]string{{"--version"}, {"--help"}} {
		var out bytes.Buffer
		if err := runArgs(append([]string{"--config", config}, args...), strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			t.Fatal("missing output")
		}
	}
	var out bytes.Buffer
	if err := runArgs([]string{"--self-update", "--config", config}, strings.NewReader("j\n"), &out); err == nil || !strings.Contains(err.Error(), "Entwicklungsbuild") {
		t.Fatal(err)
	}
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatal("created skill configuration")
	}
	if err := runArgs([]string{"--self-update", "--update"}, strings.NewReader(""), &out); err == nil {
		t.Fatal("mixed modes accepted")
	}
	for _, answer := range []string{"", "\n", "n", "no", "maybe"} {
		if confirmProgramUpdate(answer) {
			t.Fatalf("confirmed %q", answer)
		}
	}
	for _, answer := range []string{"j", "ja\n", "Y", "yes"} {
		if !confirmProgramUpdate(answer) {
			t.Fatalf("rejected %q", answer)
		}
	}
}

// Build two real versions, download the new one through a local release server,
// then let a temporary copy of the old executable replace itself while running.
// This exercises Windows executable locking without modifying the installed tool.
func TestProgramUpdateEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("two temporary executable builds")
	}
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	old, next := filepath.Join(dir, "old"+ext), filepath.Join(dir, "next"+ext)
	builds := [][]string{
		{"test", "-c", "-ldflags=-X github.com/roman-kiefer/skill-manager.version=v1.0.0", "-o", old, "."},
		{"build", "-ldflags=-X main.version=v1.2.0", "-o", next, "."},
	}
	for _, args := range builds {
		cmd := exec.Command("go", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, output)
		}
	}
	binary, err := os.ReadFile(next)
	if err != nil {
		t.Fatal(err)
	}
	server, _ := releaseFixture(t, binaryArchive(t, binary, runtime.GOOS, runtime.GOARCH), "")
	cmd := exec.Command(old, "-test.run=^TestProgramUpdateHelper$")
	cmd.Env = append(os.Environ(), "SKILL_MANAGER_TEST_RELEASE="+server.URL+"/latest")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running update: %v\n%s", err, output)
	}
	cmd = exec.Command(old, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "v1.2.0") {
		t.Fatalf("updated version: %v\n%s", err, output)
	}
}

func TestProgramUpdateHelper(t *testing.T) {
	endpoint := os.Getenv("SKILL_MANAGER_TEST_RELEASE")
	if endpoint == "" {
		t.Skip("subprocess helper")
	}
	update, err := checkProgramUpdate(context.Background(), http.DefaultClient, endpoint, version, runtime.GOOS, runtime.GOARCH)
	if err != nil || !update.Available {
		t.Fatalf("%+v %v", update, err)
	}
	if err := installProgram(update); err != nil {
		t.Fatal(err)
	}
}
