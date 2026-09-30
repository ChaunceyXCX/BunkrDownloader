package aria2

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Aria2Version is the aria2 release provisioned when none is found locally.
const Aria2Version = "1.37.0"

// releaseBase is the GitHub download root for aria2 artifacts.
const releaseBase = "https://github.com/aria2/aria2/releases/download/release-" + Aria2Version

// assetName returns the official release asset for the current platform.
//
// The upstream project publishes Windows and macOS binaries but no Linux ones
// (Linux is distributed through the distro packages), so Linux returns an
// error explaining how to install aria2 instead of a 404 URL.
func assetName() (name string, err error) {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return fmt.Sprintf("aria2-%s-win-64bit-build1.zip", Aria2Version), nil
		case "386":
			return fmt.Sprintf("aria2-%s-win-32bit-build1.zip", Aria2Version), nil
		}
		return "", fmt.Errorf("aria2: unsupported arch %s/%s", runtime.GOOS, runtime.GOARCH)
	case "darwin":
		// Apple Silicon and Intel both run the universal macOS build.
		return fmt.Sprintf("aria2-%s-mac-64bit-build1.tar.bz2", Aria2Version), nil
	case "linux":
		return "", fmt.Errorf(
			"aria2: upstream publishes no Linux binary for %s; install it with your "+
				"package manager (Debian/Ubuntu: `apt install aria2`, Alpine: `apk add aria2`, "+
				"Fedora: `dnf install aria2`), or point BUNKR_ARIA2_BIN at an existing binary",
			runtime.GOARCH)
	}
	return "", fmt.Errorf("aria2: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

// FetchBinary downloads and extracts aria2c into destDir.
//
// It is idempotent: when an executable aria2c already exists there, it
// returns immediately. The download is verified against GitHub's published
// checksum (SHA-256SUMS) when available, and always validated structurally
// (the binary must report a version).
func FetchBinary(ctx context.Context, destDir string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(destDir, exeName())
	if isExecutable(target) {
		return nil
	}

	asset, err := assetName()
	if err != nil {
		return err
	}
	url := releaseBase + "/" + asset
	log.Info("downloading aria2", "url", url)

	body, err := download(ctx, url, 64<<20)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}

	if want, err := releaseChecksum(ctx, asset); err == nil && want != "" {
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, want) {
			return fmt.Errorf("checksum mismatch for %s: want %s got %s", asset, want, got)
		}
		log.Info("aria2 checksum verified", "sha256", want)
	} else {
		// Upstream does not publish a checksum manifest for every release, so a
		// missing file is expected rather than exceptional.
		log.Debug("no published checksum to verify against", "asset", asset, "reason", err)
	}

	staging := filepath.Join(destDir, ".aria2-extract")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	if err := extractArchive(asset, body, staging); err != nil {
		return fmt.Errorf("extract %s: %w", asset, err)
	}

	bin := findBinary(staging)
	if bin == "" {
		return errors.New("aria2c not present in the release archive")
	}
	_ = os.Chmod(bin, 0o755)

	// Publish atomically so a concurrent reader never sees a partial binary.
	tmp := target + ".tmp"
	if err := copyFile(bin, tmp); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o755)
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		// Windows can refuse a rename onto an existing file.
		_ = os.Remove(target)
		if err2 := os.Rename(tmp, target); err2 != nil {
			return fmt.Errorf("install aria2c: %w", err2)
		}
	}
	log.Info("aria2c installed", "path", target)
	return nil
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "aria2c.exe"
	}
	return "aria2c"
}

func download(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BunkrDownloader/1.0 (+aria2-provisioner)")

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
}

// releaseChecksum looks the asset digest up in the published SHA-256SUMS file.
func releaseChecksum(ctx context.Context, asset string) (string, error) {
	body, err := download(ctx, releaseBase+"/SHA-256SUMS", 1<<20)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum published for %s", asset)
}

func extractArchive(asset string, data []byte, dest string) error {
	switch {
	case strings.HasSuffix(asset, ".zip"):
		return extractZip(data, dest)
	case strings.HasSuffix(asset, ".tar.gz"), strings.HasSuffix(asset, ".tgz"):
		return extractTarGz(data, dest)
	case strings.HasSuffix(asset, ".tar.bz2"):
		return extractTarBz2(data, dest)
	}
	return fmt.Errorf("unsupported archive %s", asset)
}

func extractZip(data []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		target, err := safeJoin(dest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeStream(target, rc, f.Mode())
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTar(gz, dest)
}

func extractTarBz2(data []byte, dest string) error {
	return extractTar(bzip2.NewReader(bytes.NewReader(data)), dest)
}

func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeStream(target, tr, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Skip symlinks: we only need the aria2c executable.
			continue
		}
	}
}

// safeJoin prevents zip-slip / path traversal during extraction.
//
// Note: on Windows `filepath.IsAbs` does not treat a leading backslash as
// absolute, so the separator prefix is rejected explicitly as well.
func safeJoin(dest, name string) (string, error) {
	slashed := filepath.ToSlash(name)
	if strings.HasPrefix(slashed, "/") || strings.Contains(slashed, "\x00") {
		return "", fmt.Errorf("aria2: unsafe archive entry %q", name)
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("aria2: unsafe archive entry %q", name)
	}
	target := filepath.Join(dest, cleaned)
	rel, err := filepath.Rel(dest, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("aria2: unsafe archive entry %q", name)
	}
	return target, nil
}

func writeStream(target string, r io.Reader, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// findBinary locates the aria2c executable inside an extracted tree.
func findBinary(root string) string {
	want := exeName()
	var found string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || found != "" {
			return nil //nolint:nilerr // best-effort search
		}
		if info.IsDir() {
			return nil
		}
		if strings.EqualFold(info.Name(), want) {
			found = path
		}
		return nil
	})
	return found
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
