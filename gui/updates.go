package gui

import (
	"bufio"
	"context"
	"crypto/sha256"
	hexenc "encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// KaluluVersion is the version of Kalulu, set at build time with
// -ldflags "-X github.com/bjarneo/cliamp/gui.KaluluVersion=1.2.3".
var KaluluVersion = "2.5.2"

// updateRepo is the GitHub repository of the releases.
const updateRepo = "HubDevCode/Kalulu"

const (
	prefUpdateAuto = "updates.auto"
	prefUpdateLast = "updates.last_check"
)

var errNoRelease = errors.New("no release published yet")

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type release struct {
	Tag       string         `json:"tag_name"`
	Name      string         `json:"name"`
	Body      string         `json:"body"`
	Published time.Time      `json:"published_at"`
	Page      string         `json:"html_url"`
	Draft     bool           `json:"draft"`
	Pre       bool           `json:"prerelease"`
	Assets    []releaseAsset `json:"assets"`
}

var updateClient = &http.Client{Timeout: 30 * time.Second}

// latestRelease asks GitHub for the newest published release.
func latestRelease(ctx context.Context) (release, error) {
	var r release
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Kalulu/"+KaluluVersion)
	resp, err := updateClient.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return r, errNoRelease
	default:
		return r, fmt.Errorf("GitHub: HTTP %d", resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r)
	return r, err
}

// parseVersion reads "v1.2.3", "1.2" or "1.2.3-beta" into numbers.
func parseVersion(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(part)
		out[i] = n
	}
	return out, pre
}

// newerVersion reports whether latest is newer than current. A release
// beats a pre-release of the same numbers.
func newerVersion(latest, current string) bool {
	l, lp := parseVersion(latest)
	c, cp := parseVersion(current)
	for i := range 3 {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return lp == "" && cp != ""
}

// pickAsset chooses the download for this system and the checksum list.
func pickAsset(r release) (pkg, sums *releaseAsset) {
	return pickAssetFor(r, runtime.GOOS, runtime.GOARCH)
}

// pickAssetFor chooses the download for goos and goarch: the AppImage on
// Linux, the installer on Windows.
func pickAssetFor(r release, goos, goarch string) (pkg, sums *releaseAsset) {
	alias := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	for i := range r.Assets {
		a := &r.Assets[i]
		name := strings.ToLower(a.Name)
		switch {
		case name == "sha256sums" || strings.HasSuffix(name, "sha256sums.txt") || name == "checksums.txt":
			sums = a
		case pkg == nil && goos == "linux" && strings.HasSuffix(name, ".appimage") &&
			(strings.Contains(name, goarch) || (alias != "" && strings.Contains(name, alias))):
			pkg = a
		case pkg == nil && goos == "windows" && strings.HasSuffix(name, "-setup.exe") &&
			strings.Contains(name, windowsArch(goarch)):
			pkg = a
		}
	}
	return pkg, sums
}

// windowsArch is the architecture name of the Windows installers.
func windowsArch(goarch string) string {
	if goarch == "amd64" {
		return "x64"
	}
	return goarch
}

// downloadVerified downloads the asset and checks it against the SHA-256
// list of the same release. Without a list it refuses: an unverified
// binary is never installed.
func downloadVerified(ctx context.Context, pkg, sums *releaseAsset, dir string) (string, error) {
	if sums == nil {
		return "", errors.New("the release has no SHA256SUMS file, so the download cannot be verified")
	}
	expected := ""
	resp, err := get(ctx, sums.URL)
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == pkg.Name {
			expected = strings.ToLower(f[0])
		}
	}
	resp.Body.Close()
	if len(expected) != 64 {
		return "", fmt.Errorf("%s is not listed in the checksums", pkg.Name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out, err := os.CreateTemp(dir, ".kalulu-update-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(out.Name())
	resp, err = get(ctx, pkg.URL)
	if err != nil {
		out.Close()
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, 512<<20))
	resp.Body.Close()
	out.Close()
	if err != nil {
		return "", err
	}
	if got := hexenc.EncodeToString(h.Sum(nil)); got != expected {
		return "", errors.New("the downloaded file does not match its checksum; it was discarded")
	}
	final := filepath.Join(dir, pkg.Name)
	if err := os.Rename(out.Name(), final); err != nil {
		return "", err
	}
	_ = os.Chmod(final, 0o755)
	return final, nil
}

func get(ctx context.Context, u string) (*http.Response, error) {
	if parsed, err := url.Parse(u); err != nil || parsed.Scheme != "https" {
		return nil, errors.New("refusing a download that is not HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Kalulu/"+KaluluVersion)
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// updatesCard is the Updates section of Settings.
func (a *App) updatesCard() fyne.CanvasObject {
	status := label(T("Not checked yet"))
	status.Wrapping = fyne.TextWrapWord
	notes := captionLabel("")
	notes.Wrapping = fyne.TextWrapWord
	actions := container.NewHBox()
	var check func()
	check = func() {
		status.SetText(T("Checking…"))
		actions.Objects = nil
		actions.Refresh()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			r, err := latestRelease(ctx)
			a.prefs.SetString(prefUpdateLast, time.Now().Format(time.RFC3339))
			runOnUI(func() {
				switch {
				case errors.Is(err, errNoRelease):
					status.SetText(T("No release has been published yet on GitHub."))
				case err != nil:
					status.SetText(T("Could not check: ") + err.Error())
				case !newerVersion(r.Tag, KaluluVersion):
					status.SetText(fmt.Sprintf(T("Kalulu is up to date (latest: %s)."), r.Tag))
				default:
					status.SetText(fmt.Sprintf(T("Kalulu %s is available, published on %s."), r.Tag, r.Published.Local().Format("02/01/2006")))
					body := strings.TrimSpace(r.Body)
					if len(body) > 900 {
						body = body[:900] + "…"
					}
					notes.SetText(body)
					a.updateActions(actions, r)
				}
			})
		}()
	}
	auto := widget.NewCheck("", func(on bool) { a.prefs.SetBool(prefUpdateAuto, on) })
	auto.Checked = a.prefs.BoolWithFallback(prefUpdateAuto, true)
	return settingsCard(T("Updates"),
		setRow(T("Installed version"), "", label("Kalulu "+KaluluVersion+" · cliamp "+a.opts.Version)),
		setRow(T("Check automatically"), T("Once a day, in the background; playback is never interrupted"), auto),
		container.NewHBox(flatButton(T("Check now"), theme.ViewRefreshIcon(), check)),
		status, notes, actions,
	)
}

// updateActions offers the release page, and a verified download when the
// release has a package for this system.
func (a *App) updateActions(box *fyne.Container, r release) {
	box.Objects = nil
	if u, err := url.Parse(r.Page); err == nil {
		box.Add(flatButton(T("Open the release page"), theme.ComputerIcon(), func() { _ = a.fa.OpenURL(u) }))
	}
	pkg, sums := pickAsset(r)
	if pkg != nil {
		box.Add(flatButton(T("Download and verify"), theme.DownloadIcon(), func() {
			a.toast(T("Downloading the update…"))
			go func() {
				dir := filepath.Join(userDownloads(), "Kalulu")
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
				defer cancel()
				path, err := downloadVerified(ctx, pkg, sums, dir)
				if err != nil {
					a.toastError(err)
					return
				}
				runOnUI(func() { a.offerInstall(path) })
			}()
		}))
	}
	box.Refresh()
}

func userDownloads() string {
	home, _ := os.UserHomeDir()
	for _, name := range []string{"Scaricati", "Downloads"} {
		if st, err := os.Stat(filepath.Join(home, name)); err == nil && st.IsDir() {
			return filepath.Join(home, name)
		}
	}
	return home
}

// offerInstall replaces the running AppImage with the verified download, or
// shows where the file is.
func (a *App) offerInstall(path string) {
	if runtime.GOOS == "windows" {
		a.runInstaller(path)
		return
	}
	current := os.Getenv("APPIMAGE")
	if current == "" {
		a.toast(T("Update verified and saved to ") + path)
		a.openPath(filepath.Dir(path))
		return
	}
	if err := os.Rename(path, current); err != nil {
		data, rerr := os.ReadFile(path)
		if rerr != nil || os.WriteFile(current, data, 0o755) != nil {
			a.toastError(fmt.Errorf("install: %w", err))
			return
		}
	}
	a.toast(T("Update installed; restarting…"))
	a.restart()
}

// runInstaller starts the verified Windows installer without its wizard
// and quits, so the installer can replace the files. The installer starts
// Kalulu again when it is done.
func (a *App) runInstaller(path string) {
	cmd := exec.Command(path, "/SILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CLOSEAPPLICATIONS")
	if err := cmd.Start(); err != nil {
		a.toastError(fmt.Errorf("install: %w", err))
		return
	}
	a.toast(T("Update installed; restarting…"))
	a.quit()
}

// autoCheckUpdates checks once a day and tells the user about a newer
// release, without touching anything.
func (a *App) autoCheckUpdates() {
	if !a.prefs.BoolWithFallback(prefUpdateAuto, true) {
		return
	}
	if last, err := time.Parse(time.RFC3339, a.prefs.String(prefUpdateLast)); err == nil && time.Since(last) < 24*time.Hour {
		return
	}
	go func() {
		time.Sleep(20 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		r, err := latestRelease(ctx)
		a.prefs.SetString(prefUpdateLast, time.Now().Format(time.RFC3339))
		if err == nil && newerVersion(r.Tag, KaluluVersion) {
			a.toast(fmt.Sprintf(T("Kalulu %s is available: Settings → Updates"), r.Tag))
		}
	}()
}
