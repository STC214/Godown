package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const appDirName = "GhostDownloaderGo"

type Paths struct {
	DataDir     string
	PluginDir   string
	RuntimeDir  string
	TempDir     string
	LogFile     string
	ConfigDB    string
	TaskFile    string
	DownloadDir string
}

func ResolvePaths() (Paths, error) {
	roaming, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("user config dir: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("user home dir: %w", err)
	}

	dataDir := filepath.Join(roaming, appDirName)
	paths := Paths{
		DataDir:     dataDir,
		PluginDir:   filepath.Join(dataDir, "plugins"),
		RuntimeDir:  filepath.Join(dataDir, "runtimes"),
		TempDir:     filepath.Join(dataDir, "temp"),
		LogFile:     filepath.Join(dataDir, "GhostDownloader.log"),
		ConfigDB:    filepath.Join(dataDir, "config.db"),
		TaskFile:    filepath.Join(dataDir, "tasks.json"),
		DownloadDir: filepath.Join(home, "Downloads"),
	}

	for _, dir := range []string{paths.DataDir, paths.PluginDir, paths.RuntimeDir, paths.TempDir, paths.DownloadDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Paths{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}

	return paths, nil
}
