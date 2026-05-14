package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultOwner = "adamshand"
	defaultRepo  = "ai-dur"
	maxBinary    = 128 << 20
)

var errManualInstallRequired = errors.New("manual install required")

// Options configures a dur self-update.
type Options struct {
	CurrentVersion string
	Out            io.Writer
	Err            io.Writer
	HTTPClient     *http.Client

	// ExecutablePath is only intended for tests. When empty, os.Executable is used.
	ExecutablePath string

	// RepoOwner and RepoName default to the official dur repository.
	RepoOwner string
	RepoName  string
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type updater struct {
	options Options
	client  *http.Client
}

// Run updates the current dur binary from the latest GitHub release.
func Run(ctx context.Context, opts Options) int {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Err == nil {
		opts.Err = io.Discard
	}
	if opts.CurrentVersion == "" {
		opts.CurrentVersion = "dev"
	}
	if opts.RepoOwner == "" {
		opts.RepoOwner = defaultOwner
	}
	if opts.RepoName == "" {
		opts.RepoName = defaultRepo
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}

	if isDevVersion(opts.CurrentVersion) {
		fmt.Fprintln(opts.Out, "update disabled on dev builds")
		return 1
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	u := updater{options: opts, client: opts.HTTPClient}
	if err := u.run(ctx); err != nil {
		if !errors.Is(err, errManualInstallRequired) {
			fmt.Fprintln(opts.Err, "dur:", err)
		}
		return 1
	}
	return 0
}

func (u updater) run(ctx context.Context) error {
	release, err := u.latestRelease(ctx)
	if err != nil {
		return err
	}
	if release.TagName == "" {
		return errors.New("latest GitHub release has no tag_name")
	}
	if release.TagName == u.options.CurrentVersion {
		fmt.Fprintf(u.options.Out, "dur: already up to date: %s\n", u.options.CurrentVersion)
		return nil
	}

	archiveName := assetName(release.TagName, runtime.GOOS, runtime.GOARCH)
	archiveAsset, ok := findAsset(release.Assets, archiveName)
	if !ok {
		return fmt.Errorf("no release asset for %s/%s: expected %s", runtime.GOOS, runtime.GOARCH, archiveName)
	}
	checksumAsset, ok := findAsset(release.Assets, "checksums.txt")
	if !ok {
		return errors.New("latest release is missing checksums.txt")
	}

	fmt.Fprintf(u.options.Out, "dur: updating %s -> %s\n", u.options.CurrentVersion, release.TagName)

	tmpDir, err := os.MkdirTemp("", "dur-update-"+safeTempPart(release.TagName)+"-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := u.downloadFile(ctx, checksumAsset.BrowserDownloadURL, checksumsPath); err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	expectedHash, err := expectedChecksum(checksumsPath, archiveName)
	if err != nil {
		return err
	}

	archivePath := filepath.Join(tmpDir, archiveName)
	if err := u.downloadFile(ctx, archiveAsset.BrowserDownloadURL, archivePath); err != nil {
		return fmt.Errorf("download %s: %w", archiveName, err)
	}
	if err := verifySHA256(archivePath, expectedHash); err != nil {
		return err
	}

	binaryPath := filepath.Join(tmpDir, "dur")
	if err := extractDurBinary(archivePath, binaryPath); err != nil {
		return err
	}

	target, err := u.installBinary(binaryPath)
	if err != nil {
		if os.IsPermission(err) {
			cleanup = false
			fmt.Fprintf(u.options.Err, "dur: cannot replace %s: %v\n", target, err)
			fmt.Fprintf(u.options.Err, "dur: downloaded verified binary to %s\n", binaryPath)
			fmt.Fprintln(u.options.Err, "dur: install manually with:")
			fmt.Fprintf(u.options.Err, "  sudo install -m 0755 %s %s\n", shellQuote(binaryPath), shellQuote(target))
			return errManualInstallRequired
		}
		return fmt.Errorf("install update: %w", err)
	}

	fmt.Fprintf(u.options.Out, "dur: installed %s\n", target)
	return nil
}

func (u updater) latestRelease(ctx context.Context) (githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", u.options.RepoOwner, u.options.RepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, err
	}
	u.setHeaders(req)
	res, err := u.client.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return githubRelease{}, fmt.Errorf("GitHub latest release request failed: %s: %s", res.Status, strings.TrimSpace(string(body)))
	}
	var release githubRelease
	if err := json.NewDecoder(res.Body).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("parse GitHub latest release response: %w", err)
	}
	return release, nil
}

func (u updater) downloadFile(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	u.setHeaders(req)
	res, err := u.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("download failed: %s: %s", res.Status, strings.TrimSpace(string(body)))
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, res.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (u updater) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "dur/"+u.options.CurrentVersion)
}

func (u updater) installBinary(src string) (string, error) {
	exe := u.options.ExecutablePath
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("find current executable: %w", err)
		}
	}
	exe, err := filepath.Abs(exe)
	if err != nil {
		return exe, err
	}
	target, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, fmt.Errorf("resolve executable symlinks: %w", err)
	}
	st, err := os.Stat(target)
	if err != nil {
		return target, fmt.Errorf("stat current executable: %w", err)
	}
	if !st.Mode().IsRegular() {
		return target, fmt.Errorf("current executable is not a regular file: %s", target)
	}
	mode := st.Mode().Perm()

	tmp, err := os.CreateTemp(filepath.Dir(target), ".dur-update-*")
	if err != nil {
		return target, fmt.Errorf("create temporary install file: %w", err)
	}
	tmpName := tmp.Name()
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(tmpName)
		}
	}()

	in, err := os.Open(src)
	if err != nil {
		_ = tmp.Close()
		return target, err
	}
	_, copyErr := io.Copy(tmp, in)
	closeInErr := in.Close()
	if copyErr != nil {
		_ = tmp.Close()
		return target, copyErr
	}
	if closeInErr != nil {
		_ = tmp.Close()
		return target, closeInErr
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return target, err
	}
	if err := tmp.Close(); err != nil {
		return target, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return target, err
	}
	installed = true
	return target, nil
}

func isDevVersion(version string) bool {
	return version == "dev" || strings.HasPrefix(version, "dev-")
}

func assetName(version, goos, goarch string) string {
	return fmt.Sprintf("dur-%s-%s-%s.tar.gz", version, goos, goarch)
}

func findAsset(assets []githubAsset, name string) (githubAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return githubAsset{}, false
}

func expectedChecksum(checksumsPath, archiveName string) (string, error) {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != archiveName {
			continue
		}
		hash := strings.ToLower(fields[0])
		if len(hash) != sha256.Size*2 {
			return "", fmt.Errorf("invalid SHA256 for %s in checksums.txt", archiveName)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return "", fmt.Errorf("invalid SHA256 for %s in checksums.txt", archiveName)
		}
		return hash, nil
	}
	return "", fmt.Errorf("checksums.txt is missing %s", archiveName)
}

func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != strings.ToLower(expected) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", filepath.Base(path), expected, actual)
	}
	return nil
}

func extractDurBinary(archivePath, dst string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}
		if hdr.Name != "dur" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return errors.New("archive dur entry is not a regular file")
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, copyErr := copyLimited(out, tr, maxBinary)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return errors.New("archive is missing dur binary")
}

func copyLimited(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	limited := &io.LimitedReader{R: src, N: limit + 1}
	n, err := io.Copy(dst, limited)
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("dur binary exceeds limit of %d bytes", limit)
	}
	return n, nil
}

func safeTempPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "release"
	}
	return out
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
