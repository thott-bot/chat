package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.9", true},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.99.99", true},
		{"0.2.1", "0.2.0", true},
		{"0.2.0", "0.2.0", false},
		{"0.1.0", "0.2.0", false},
		{"v1.2.3", "1.2.2", true},
		{"1.2.3-beta", "1.2.3", false},
		{"garbage", "0.0.1", false},
		{"9.garbage.0", "1.0.0", false},
		{"9.0", "1.0.0", false},
		{"9.0.0-beta", "1.0.0", false},
		{"9.0.-1", "1.0.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// fakeRelease is what the fake GitHub serves: the latest release and its download.
type fakeRelease struct {
	status     int
	tag        string
	prerelease bool
	zip        []byte
	digest     string // the digest GitHub lists; empty means the digest of zip
	truncate   bool   // the download stops halfway
}

// releaseServer fakes GitHub's latest-release API and its download, and runs the app's update loop against it from an
// installed app in a temporary folder. The returned func sets the next answer. The channel gets each menu state.
func releaseServer(t *testing.T) (*app, func(fakeRelease), <-chan trayView, string) {
	var mu sync.Mutex
	answer := fakeRelease{status: http.StatusOK, tag: "v0.1.0"}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rel := answer
		mu.Unlock()
		if r.URL.Path == "/download" {
			body := rel.zip
			if rel.truncate {
				w.Header().Set("Content-Length", "100000000")
				body = body[:len(body)/2]
			}
			w.Write(body)
			return
		}
		sum := sha256.Sum256(rel.zip)
		digest := "sha256:" + hex.EncodeToString(sum[:])
		if rel.digest != "" {
			digest = rel.digest
		}
		w.WriteHeader(rel.status)
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": rel.tag, "draft": false, "prerelease": rel.prerelease,
			"assets": []map[string]any{
				{"name": "notes.txt", "size": 1, "digest": digest, "browser_download_url": srv.URL + "/wrong"},
				{"name": releaseAsset, "size": len(rel.zip), "digest": digest, "browser_download_url": srv.URL + "/download"},
			},
		})
	}))
	t.Cleanup(srv.Close)
	installed := fakeApp(t, t.TempDir(), []byte("version 0.1.0"))
	oldAPI, oldVersion, oldApp := latestAPI, version, runningApp
	latestAPI, version = srv.URL+"/latest", "0.1.0"
	runningApp = func() (string, error) { return installed, nil }
	t.Cleanup(func() { latestAPI, version, runningApp = oldAPI, oldVersion, oldApp })

	ctx, cancel := context.WithCancel(context.Background())
	shown := make(chan trayView, 16)
	a := &app{
		ctx: ctx, cancel: cancel, checkNow: make(chan struct{}, 1), rescan: make(chan struct{}, 1),
		show: func(v trayView) { shown <- v }, exit: func() {},
	}
	done := make(chan struct{})
	go func() { a.checkUpdates(); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	set := func(r fakeRelease) {
		mu.Lock()
		answer = r
		mu.Unlock()
	}
	return a, set, shown, installed
}

// fakeApp makes an installed app in dir whose executable holds exe.
func fakeApp(t *testing.T, dir string, exe []byte) string {
	t.Helper()
	app := filepath.Join(dir, appName)
	if err := os.MkdirAll(filepath.Dir(appExe(app)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appExe(app), exe, 0o755); err != nil {
		t.Fatal(err)
	}
	return app
}

// releaseZip packs exe the way the release workflow does.
func releaseZip(t *testing.T, exe []byte) []byte {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "darwin" {
		app := fakeApp(t, dir, exe)
		zipPath := filepath.Join(dir, releaseAsset)
		if out, err := exec.Command("ditto", "-c", "-k", "--keepParent", app, zipPath).CombinedOutput(); err != nil {
			t.Fatalf("ditto: %v: %s", err, out)
		}
		data, err := os.ReadFile(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, data := range map[string][]byte{appName: exe, "LICENSE": []byte("GPL")} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func skipWithoutRelease(t *testing.T) {
	if releaseAsset == "" {
		t.Skip("no release is built for this system")
	}
}

func nextView(t *testing.T, shown <-chan trayView) trayView {
	t.Helper()
	select {
	case v := <-shown:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("the menu never changed")
		return trayView{}
	}
}

func startupChecked(t *testing.T, shown <-chan trayView) {
	t.Helper()
	if v := nextView(t, shown); v.check != checkIdle || v.update != "" {
		t.Fatalf("after the startup check found nothing newer: %+v", v)
	}
}

func stagesLeft(t *testing.T, installed string) []string {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(installed), stagePrefix+"*"))
	return left
}

func TestUpdateInstallsAndRelaunchesTheNewVersion(t *testing.T) {
	skipWithoutRelease(t)
	a, set, shown, installed := releaseServer(t)
	startupChecked(t, shown)

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	newExe, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	set(fakeRelease{status: http.StatusOK, tag: "v0.2.0", zip: releaseZip(t, newExe)})
	a.requestUpdateCheck()
	if v := nextView(t, shown); v.check != checkRunning {
		t.Fatalf("while checking: %+v", v)
	}
	if v := nextView(t, shown); v.update != "0.2.0" || v.updating != updateDownloading || updateTitle(v) != "Downloading thott.bot chat 0.2.0…" {
		t.Fatalf("after finding 0.2.0: %+v", v)
	}
	if v := nextView(t, shown); v.update != "0.2.0" || v.updating != updateReady || updateTitle(v) != "Install 0.2.0 and Restart" {
		t.Fatalf("after downloading 0.2.0: %+v", v)
	}
	if got, _ := os.ReadFile(appExe(installed)); string(got) != "version 0.1.0" {
		t.Fatal("the download replaced the app before the player or an idle moment installed it")
	}

	marker := filepath.Join(t.TempDir(), "relaunched")
	t.Setenv("THOTT_BOT_CHAT_TEST_MODE", "relaunched")
	t.Setenv("THOTT_BOT_CHAT_TEST_MARKER", marker)
	exited := make(chan struct{}, 1)
	a.exit = func() { exited <- struct{}{} }
	a.installWhenIdle()
	if got, _ := os.ReadFile(appExe(installed)); !bytes.Equal(got, newExe) {
		t.Fatal("the installed app is not the new version")
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the old version did not quit after the update")
	}
	deadline := time.Now().Add(10 * time.Second)
	for !isFile(marker) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got, _ := os.ReadFile(marker); string(got) != "1\n" {
		t.Fatalf("the new version did not start and wait for the old one: %q", got)
	}
	a.removeLeftovers()
	if left := stagesLeft(t, installed); len(left) != 0 {
		t.Fatalf("the update left %v", left)
	}
}

func TestFailedDownloadLeavesTheAppUntouched(t *testing.T) {
	skipWithoutRelease(t)
	zipData := releaseZip(t, []byte("version 0.2.0"))
	cases := map[string]fakeRelease{
		"wrong digest": {digest: "sha256:" + hex.EncodeToString(make([]byte, sha256.Size))},
		"cut off":      {truncate: true},
		"no digest":    {digest: "md5:0"},
	}
	for name, rel := range cases {
		t.Run(name, func(t *testing.T) {
			a, set, shown, installed := releaseServer(t)
			startupChecked(t, shown)
			rel.status, rel.tag, rel.zip = http.StatusOK, "v0.2.0", zipData
			set(rel)
			a.requestUpdateCheck()
			nextView(t, shown)
			if v := nextView(t, shown); v.updating != updateDownloading {
				t.Fatalf("after finding 0.2.0: %+v", v)
			}
			v := nextView(t, shown)
			if v.updating != updateFailed || updateTitle(v) != "Update Failed: Download 0.2.0 from GitHub…" {
				t.Fatalf("after a bad download: %+v", v)
			}
			a.installWhenIdle()
			if got, _ := os.ReadFile(appExe(installed)); string(got) != "version 0.1.0" {
				t.Fatalf("a bad download changed the installed app: %q", got)
			}
			if left := stagesLeft(t, installed); len(left) != 0 {
				t.Fatalf("a bad download left %v", left)
			}
			a.mu.Lock()
			page := a.updateURL
			a.mu.Unlock()
			if page != repoURL+"/releases/tag/v0.2.0" {
				t.Errorf("the failed update opens %q", page)
			}
		})
	}
}

func TestUpdateTakesOnlyAPublishedRelease(t *testing.T) {
	skipWithoutRelease(t)
	a, set, shown, _ := releaseServer(t)
	startupChecked(t, shown)
	set(fakeRelease{status: http.StatusOK, tag: "v0.2.0", prerelease: true, zip: releaseZip(t, []byte("pre"))})
	a.requestUpdateCheck()
	nextView(t, shown)
	if v := nextView(t, shown); v.update != "" || v.updating != updateNone {
		t.Fatalf("a pre-release was offered: %+v", v)
	}
}

func TestCheckForUpdatesUpToDateUntilDailyCheck(t *testing.T) {
	a, _, shown, _ := releaseServer(t)
	startupChecked(t, shown)

	a.requestUpdateCheck()
	if v := nextView(t, shown); v.check != checkRunning {
		t.Fatalf("while checking: %+v", v)
	}
	if v := nextView(t, shown); v.check != checkUpToDate || v.update != "" {
		t.Fatalf("after finding nothing newer: %+v", v)
	}

	a.checkForUpdate(false) // the daily check
	if v := nextView(t, shown); v.check != checkIdle || v.update != "" {
		t.Fatalf("after the daily check: %+v", v)
	}
}

func TestCheckForUpdatesFails(t *testing.T) {
	a, set, shown, _ := releaseServer(t)
	startupChecked(t, shown)

	set(fakeRelease{status: http.StatusInternalServerError})
	a.requestUpdateCheck()
	if v := nextView(t, shown); v.check != checkRunning {
		t.Fatalf("while checking: %+v", v)
	}
	if v := nextView(t, shown); v.check != checkIdle || v.update != "" {
		t.Fatalf("after a failed check: %+v", v)
	}
}
