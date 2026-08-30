package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxPortableBytes = 256 << 20
	maxChecksumBytes = 4 << 10
)

type PreparedUpdate struct {
	Version     string
	ArchivePath string
}

type PortableUpdater struct {
	Client *http.Client
}

func (r Release) HasPortableUpdate() bool {
	_, _, err := portableAssets(r.Assets)
	return err == nil
}

func (u PortableUpdater) Prepare(ctx context.Context, release Release, tempRoot string) (PreparedUpdate, error) {
	archiveAsset, checksumAsset, err := portableAssets(release.Assets)
	if err != nil {
		return PreparedUpdate{}, err
	}
	if archiveAsset.Size > maxPortableBytes {
		return PreparedUpdate{}, fmt.Errorf("portable archive exceeds %d bytes", maxPortableBytes)
	}
	dir, err := os.MkdirTemp(tempRoot, "gd3-update-")
	if err != nil {
		return PreparedUpdate{}, fmt.Errorf("create update directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	archivePath := filepath.Join(dir, filepath.Base(archiveAsset.Name))
	if err := u.download(ctx, archiveAsset.DownloadURL, archivePath, maxPortableBytes); err != nil {
		return PreparedUpdate{}, fmt.Errorf("download portable archive: %w", err)
	}
	checksumPath := archivePath + ".sha256"
	if err := u.download(ctx, checksumAsset.DownloadURL, checksumPath, maxChecksumBytes); err != nil {
		return PreparedUpdate{}, fmt.Errorf("download portable checksum: %w", err)
	}
	if err := verifyChecksum(archivePath, checksumPath); err != nil {
		return PreparedUpdate{}, err
	}
	if err := verifyPortableArchive(archivePath, release.Version); err != nil {
		return PreparedUpdate{}, err
	}
	keep = true
	return PreparedUpdate{Version: release.Version, ArchivePath: archivePath}, nil
}

func (u PortableUpdater) download(ctx context.Context, source, destination string, limit int64) error {
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return fmt.Errorf("invalid asset URL %q", source)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "GhostDownloaderGo-Updater")
	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("asset request returned %s", response.Status)
	}
	if response.ContentLength > limit {
		return fmt.Errorf("asset exceeds %d bytes", limit)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > limit {
		return fmt.Errorf("asset exceeds %d bytes", limit)
	}
	return nil
}

func portableAssets(assets []Asset) (Asset, Asset, error) {
	var archive, checksum Asset
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, "windows-x64-portable.zip") {
			archive = asset
		}
	}
	if archive.Name != "" {
		wanted := strings.ToLower(archive.Name + ".sha256")
		for _, asset := range assets {
			if strings.ToLower(asset.Name) == wanted {
				checksum = asset
				break
			}
		}
	}
	if archive.Name == "" || checksum.Name == "" {
		return Asset{}, Asset{}, fmt.Errorf("release has no portable ZIP and matching SHA-256 asset")
	}
	return archive, checksum, nil
}

func verifyChecksum(archivePath, checksumPath string) error {
	text, err := os.ReadFile(checksumPath)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(text))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return fmt.Errorf("portable checksum file is invalid")
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil {
		return fmt.Errorf("portable checksum file is invalid")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), hex.EncodeToString(expected)) {
		return fmt.Errorf("portable archive SHA-256 mismatch")
	}
	return nil
}

func verifyPortableArchive(path, version string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open portable archive: %w", err)
	}
	defer reader.Close()
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		clean := filepath.Clean(file.Name)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("portable archive contains unsafe path %q", file.Name)
		}
		files[filepath.ToSlash(clean)] = file
	}
	manifestFile := files["release-manifest.json"]
	if manifestFile == nil {
		return fmt.Errorf("portable archive has no release-manifest.json")
	}
	manifestReader, err := manifestFile.Open()
	if err != nil {
		return err
	}
	var manifest struct {
		Version string `json:"version"`
		Files   []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	err = json.NewDecoder(io.LimitReader(manifestReader, 1<<20)).Decode(&manifest)
	_ = manifestReader.Close()
	if err != nil {
		return fmt.Errorf("decode portable manifest: %w", err)
	}
	if manifest.Version != version {
		return fmt.Errorf("portable version %q does not match release %q", manifest.Version, version)
	}
	required := map[string]bool{"gd3win.exe": false, "gd3-bt-runtime.exe": false, "update-portable.ps1": false}
	for _, item := range manifest.Files {
		file := files[filepath.ToSlash(filepath.Clean(item.Name))]
		if file == nil {
			return fmt.Errorf("portable manifest file %q is missing", item.Name)
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, reader)
		_ = reader.Close()
		if copyErr != nil {
			return copyErr
		}
		if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), item.SHA256) {
			return fmt.Errorf("portable manifest hash mismatch for %q", item.Name)
		}
		if _, ok := required[item.Name]; ok {
			required[item.Name] = true
		}
	}
	for name, found := range required {
		if !found {
			return fmt.Errorf("portable archive is missing required file %q", name)
		}
	}
	return nil
}

func LaunchPortableUpdater(prepared PreparedUpdate, installDir, executable string, waitPID int) error {
	script := filepath.Join(installDir, "update-portable.ps1")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("portable updater helper: %w", err)
	}
	command := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
		"-Archive", prepared.ArchivePath, "-InstallDir", installDir, "-WaitPID", fmt.Sprint(waitPID), "-Relaunch", executable)
	command.Dir = installDir
	if err := command.Start(); err != nil {
		return fmt.Errorf("start portable updater: %w", err)
	}
	return command.Process.Release()
}
