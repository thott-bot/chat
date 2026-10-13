package main

import (
	"bytes"
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
	"strconv"
	"strings"
	"time"
)

const (
	repoURL   = "https://github.com/thott-bot/chat"
	codexURL  = "https://learn.chatgpt.com/docs/codex/cli"
	claudeURL = "https://code.claude.com/docs/en/setup"
	tagPrefix = "v"
)

var latestAPI = "https://api.github.com/repos/thott-bot/chat/releases/latest"

var errDigest = errors.New("the download does not match the SHA-256 digest that GitHub lists for it")

type release struct {
	version, page string
	asset         *releaseFile
}

type releaseFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"browser_download_url"`
}

// latestRelease returns the newer published release, or nil when this build is current.
func latestRelease(ctx context.Context) (*release, error) {
	if version == "dev" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "thott-bot-chat/"+version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var latest struct {
		TagName    string        `json:"tag_name"`
		Draft      bool          `json:"draft"`
		Prerelease bool          `json:"prerelease"`
		Assets     []releaseFile `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPacketBytes)).Decode(&latest); err != nil {
		return nil, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	if latest.Draft || latest.Prerelease {
		return nil, fmt.Errorf("GitHub's latest release %s is a draft or a pre-release", latest.TagName)
	}
	v, ok := strings.CutPrefix(latest.TagName, tagPrefix)
	if !ok || !newerVersion(v, version) {
		return nil, nil
	}
	r := &release{version: v, page: repoURL + "/releases/tag/" + url.PathEscape(latest.TagName)}
	for i := range latest.Assets {
		if latest.Assets[i].Name == releaseAsset && releaseAsset != "" {
			r.asset = &latest.Assets[i]
		}
	}
	return r, nil
}

func newerVersion(a, b string) bool {
	pa, validA := versionParts(a)
	pb, validB := versionParts(b)
	if !validA || !validB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func versionParts(v string) ([3]uint64, bool) {
	var parts [3]uint64
	fields := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(fields) != len(parts) {
		return parts, false
	}
	for i, field := range fields {
		n, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

// downloadUpdate downloads the release's ZIP for this system beside the app, checks it against the SHA-256 digest
// that GitHub computed when the release was published, and unpacks it. On any failure it removes what it wrote.
func downloadUpdate(ctx context.Context, r *release) (staged, stage string, err error) {
	if r.asset == nil {
		return "", "", fmt.Errorf("the release has no %s", releaseAsset)
	}
	sum, ok := strings.CutPrefix(r.asset.Digest, "sha256:")
	want, hexErr := hex.DecodeString(sum)
	if !ok || hexErr != nil || len(want) != sha256.Size {
		return "", "", fmt.Errorf("GitHub lists no SHA-256 digest for %s", r.asset.Name)
	}
	app, err := runningApp()
	if err != nil {
		return "", "", err
	}
	dir, err := os.MkdirTemp(filepath.Dir(app), stagePrefix+"*")
	if err != nil {
		return "", "", fmt.Errorf("thott.bot chat can't write to its folder: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.asset.URL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "thott-bot-chat/"+version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	zip := filepath.Join(dir, releaseAsset)
	f, err := os.OpenFile(zip, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, r.asset.Size+1))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
		return "", "", fmt.Errorf("downloading: %w", err)
	case n != r.asset.Size:
		return "", "", fmt.Errorf("the download stopped after %d of %d bytes", n, r.asset.Size)
	case !bytes.Equal(h.Sum(nil), want):
		return "", "", errDigest
	}
	staged, err = extractApp(zip, dir)
	if err != nil {
		return "", "", fmt.Errorf("unpacking: %w", err)
	}
	return staged, dir, os.Remove(zip)
}

type updateCheck int

const (
	checkIdle updateCheck = iota
	checkRunning
	checkUpToDate
)

type updateStage int

const (
	updateNone updateStage = iota
	updateDownloading
	updateReady
	updateInstalling
	updateFailed
)

func (a *app) checkUpdates() {
	tick := time.NewTicker(24 * time.Hour)
	defer tick.Stop()
	asked := false
	for {
		a.checkForUpdate(asked)
		select {
		case <-a.ctx.Done():
			return
		case <-tick.C:
			asked = false
		case <-a.checkNow:
			asked = true
		}
	}
}

func (a *app) requestUpdateCheck() {
	select {
	case a.checkNow <- struct{}{}:
	default:
	}
}

func (a *app) checkForUpdate(asked bool) {
	if asked {
		a.mu.Lock()
		a.checking = checkRunning
		a.mu.Unlock()
		a.publish()
	}
	r, err := latestRelease(a.ctx)
	switch {
	case err != nil:
		logf("Couldn't check for updates: %v", err)
	case r != nil:
		logf("Update available: %s", r.version)
	}
	a.mu.Lock()
	a.checking = checkIdle
	if r == nil && err == nil && asked {
		a.checking = checkUpToDate
	}
	if r == nil || a.updateVer == r.version && (a.updateStage == updateReady || a.updateStage == updateInstalling) {
		a.mu.Unlock()
		a.publish()
		return
	}
	oldStage := a.stageDir
	a.updateVer, a.updateURL, a.updateStage, a.staged, a.stageDir = r.version, r.page, updateDownloading, "", ""
	a.mu.Unlock()
	a.publish()
	if oldStage != "" {
		os.RemoveAll(oldStage)
	}

	staged, stage, err := downloadUpdate(a.ctx, r)
	a.mu.Lock()
	if err != nil {
		a.updateStage = updateFailed
		logf("Couldn't download thott.bot chat %s: %v. Download it from %s", r.version, err, r.page)
	} else {
		a.updateStage, a.staged, a.stageDir = updateReady, staged, stage
		logf("thott.bot chat %s is ready to install", r.version)
	}
	a.mu.Unlock()
	a.publish()
	select {
	case a.rescan <- struct{}{}:
	default:
	}
}

// installWhenIdle installs a ready update while WoW is closed. Then no session runs, so no agent is connected.
func (a *app) installWhenIdle() {
	a.mu.Lock()
	idle := !a.running && a.updateStage == updateReady
	a.mu.Unlock()
	if idle {
		a.installUpdate(true)
	}
}

// installUpdate puts the downloaded version in place of this one. With relaunch it then opens the new version and
// quits this one the usual way, which only closes each agent's input.
func (a *app) installUpdate(relaunch bool) {
	a.mu.Lock()
	if a.updateStage != updateReady {
		a.mu.Unlock()
		return
	}
	a.updateStage = updateInstalling
	ver, page, staged, stage := a.updateVer, a.updateURL, a.staged, a.stageDir
	a.mu.Unlock()
	a.publish()

	app, err := runningApp()
	if err == nil {
		err = swapApp(staged, app)
	}
	if err != nil {
		os.RemoveAll(stage)
		logf("Couldn't install thott.bot chat %s: %v. thott.bot chat %s is still installed. Download the update from %s", ver, err, version, page)
		a.mu.Lock()
		a.updateStage, a.staged, a.stageDir = updateFailed, "", ""
		a.mu.Unlock()
		a.publish()
		return
	}
	logf("Installed thott.bot chat %s", ver)
	a.mu.Lock()
	a.updateVer, a.updateStage, a.staged, a.stageDir = "", updateNone, "", ""
	if err := os.RemoveAll(stage); err != nil {
		a.leftovers = append(a.leftovers, stage)
	}
	a.mu.Unlock()
	if !relaunch {
		a.publish()
		return
	}
	if err := launchApp(appExe(app)); err != nil {
		logf("Couldn't open thott.bot chat %s: %v. Quit thott.bot chat and open it again", ver, err)
		a.mu.Lock()
		a.problem = "Quit thott.bot chat and open it again to finish the update."
		a.mu.Unlock()
		a.publish()
		return
	}
	logf("Opening thott.bot chat %s", ver)
	a.exit()
}
