// Package brosdk – auto-download of platform brosdk library from GitHub Releases.
//
// On first run (or when the library is missing), this module downloads the
// latest brosdk release from https://github.com/browsersdk/brosdk/releases,
// extracts it, and places the binary under libs/<platform>/.
package brosdk

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// PlatformKey returns the release asset platform tag for the current OS/arch,
// e.g. "windows-x64", "darwin-arm64".
func PlatformKey() string {
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	default:
		arch = runtime.GOARCH
	}
	return runtime.GOOS + "-" + arch
}

// AssetFile returns the expected library filename inside the release archive.
// On Windows this is "brosdk.dll"; on macOS "libbrosdk.dylib".
func AssetFile() string {
	if runtime.GOOS == "windows" {
		return "brosdk.dll"
	}
	if runtime.GOOS == "darwin" {
		return "libbrosdk.dylib"
	}
	return "libbrosdk.so"
}

const (
	ghReleasesAPI = "https://api.github.com/repos/browsersdk/brosdk/releases/latest"
	downloadUserAgent = "brosdk-mcp-go/0.1.0"
)

// GitHubRelease represents the relevant fields from the GitHub Releases API response.
type GitHubRelease struct {
	TagName string          `json:"tag_name"`
	Assets  []GitHubAsset   `json:"assets"`
}

// GitHubAsset is one downloaded file in a release.
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// FindLatestRelease queries the GitHub Releases API and returns the latest release.
func FindLatestRelease() (*GitHubRelease, error) {
	req, err := http.NewRequest("GET", ghReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", downloadUserAgent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release json: %w", err)
	}
	return &rel, nil
}

// FindAsset returns the asset matching the current platform in the release.
// Returns nil if no matching asset is found.
func FindAsset(rel *GitHubRelease, platform string) *GitHubAsset {
	prefix := "brosdk-"
	// Asset names: brosdk-1.0.0.8-windows-x64.zip, brosdk-1.0.0.8-darwin-arm64.tar.gz
	for i := range rel.Assets {
		a := &rel.Assets[i]
		if strings.HasPrefix(a.Name, prefix) && strings.Contains(a.Name, platform) {
			return a
		}
	}
	return nil
}

// progressWriter wraps an io.Writer and reports download progress.
type progressWriter struct {
	total    int64
	received int64
	lastPct  int
	out      io.Writer
	onTick   func(received, total int64, pct int)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.out.Write(p)
	if n > 0 {
		pw.received += int64(n)
		pct := 0
		if pw.total > 0 {
			pct = int(float64(pw.received) / float64(pw.total) * 100)
		}
		if pct != pw.lastPct || pw.received == pw.total {
			pw.lastPct = pct
			if pw.onTick != nil {
				pw.onTick(pw.received, pw.total, pct)
			}
		}
	}
	return n, err
}

// DownloadAsset downloads a release asset to a temporary file, showing progress.
// Returns the path to the downloaded file.
func DownloadAsset(asset *GitHubAsset) (string, error) {
	fmt.Printf("[download] fetching %s (%s)\n", asset.Name, formatSize(asset.Size))

	req, err := http.NewRequest("GET", asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", downloadUserAgent)

	client := &http.Client{Timeout: 0} // no timeout — large files
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Create temp file
	tmp, err := os.CreateTemp("", "brosdk-*"+filepath.Ext(asset.Name))
	if err != nil {
		return "", err
	}

	pw := &progressWriter{
		total: resp.ContentLength,
		out:   tmp,
		onTick: func(received, total int64, pct int) {
			if total > 0 {
				fmt.Printf("\r[download] %s / %s (%d%%)", formatSize(received), formatSize(total), pct)
			} else {
				fmt.Printf("\r[download] %s received", formatSize(received))
			}
		},
	}

	_, copyErr := io.Copy(pw, resp.Body)
	fmt.Println() // final newline after progress
	closeErr := tmp.Close()

	if copyErr != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("copy: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("close: %w", closeErr)
	}

	// Verify size if known
	if resp.ContentLength > 0 {
		fi, _ := os.Stat(tmp.Name())
		if fi.Size() != resp.ContentLength {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("size mismatch: got %d, expected %d", fi.Size(), resp.ContentLength)
		}
	}

	return tmp.Name(), nil
}

// ExtractLibrary extracts the native library from a downloaded archive into targetDir.
// Supports .zip (Windows) and .tar.gz (macOS/Linux).
func ExtractLibrary(archivePath, targetDir, targetFile string) (string, error) {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(archivePath))

	if ext == ".zip" {
		return extractZip(archivePath, targetDir, targetFile)
	}

	if ext == ".gz" || ext == ".tgz" {
		return extractTarGz(archivePath, targetDir, targetFile)
	}

	return "", fmt.Errorf("unsupported archive format: %s", ext)
}

func extractZip(zipPath, targetDir, targetFile string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		// Look for brosdk.dll or libbrosdk.dylib in any subdirectory
		if strings.EqualFold(base, targetFile) || strings.EqualFold(base, "brosdk.dll") || strings.EqualFold(base, "libbrosdk.dylib") {
			dest := filepath.Join(targetDir, targetFile)
			return dest, extractZipFile(f, dest)
		}
	}
	return "", fmt.Errorf("%s not found in archive", targetFile)
}

func extractZipFile(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

func extractTarGz(tgzPath, targetDir, targetFile string) (string, error) {
	f, err := os.Open(tgzPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar read: %w", err)
		}
		base := filepath.Base(hdr.Name)
		if strings.EqualFold(base, targetFile) || strings.EqualFold(base, "brosdk.dll") || strings.EqualFold(base, "libbrosdk.dylib") {
			dest := filepath.Join(targetDir, targetFile)
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return "", err
			}
			defer out.Close()
			if _, err := io.Copy(out, tr); err != nil {
				return "", err
			}
			return dest, nil
		}
	}
	return "", fmt.Errorf("%s not found in archive", targetFile)
}

// EnsureLibrary checks that the native library exists at the expected path.
// If not, it downloads and extracts it automatically from GitHub Releases.
// Returns the absolute path to the library.
func EnsureLibrary(explicitPath string) (string, error) {
	// 1. If user specified an explicit path, just use it
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return explicitPath, fmt.Errorf("library not found at %q: %w", explicitPath, err)
		}
		return explicitPath, nil
	}

	// 2. Check known local paths
	platform := PlatformKey()
	assetFile := AssetFile()
	searchPaths := []string{
		assetFile,
		filepath.Join("libs", platform, assetFile),
	}

	for _, p := range searchPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// 3. Auto-download from GitHub Releases
	fmt.Printf("[brosdk] library not found locally, auto-downloading for %s...\n", platform)

	rel, err := FindLatestRelease()
	if err != nil {
		return "", fmt.Errorf("find latest release: %w", err)
	}

	asset := FindAsset(rel, platform)
	if asset == nil {
		return "", fmt.Errorf("no release asset found for platform %q in %s", platform, rel.TagName)
	}

	fmt.Printf("[brosdk] latest release: %s (%d assets)\n", rel.TagName, len(rel.Assets))

	archivePath, err := DownloadAsset(asset)
	if err != nil {
		return "", fmt.Errorf("download asset: %w", err)
	}
	defer os.Remove(archivePath)

	targetDir := filepath.Join("libs", platform)
	libPath, err := ExtractLibrary(archivePath, targetDir, assetFile)
	if err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}

	fmt.Printf("[brosdk] library installed to %s\n", libPath)
	return libPath, nil
}

func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for s := n / unit; s >= unit; s /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
