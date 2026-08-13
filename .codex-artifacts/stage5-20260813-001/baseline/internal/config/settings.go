package config

import (
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
)

const DefaultBrowserBridgePort = 14370

type Settings struct {
	DownloadDir   string `json:"downloadDir"`
	ProxyURL      string `json:"proxyUrl"`
	HeadersText   string `json:"headersText"`
	CookiesText   string `json:"cookiesText"`
	BlockNum      int    `json:"blockNum"`
	RetryCount    int    `json:"retryCount"`
	SpeedLimitKiB int64  `json:"speedLimitKiB"`
	MaxConcurrent int    `json:"maxConcurrent"`

	BrowserExtensionEnabled bool   `json:"browserExtensionEnabled"`
	BrowserPairToken        string `json:"browserPairToken"`
	BrowserBridgePort       int    `json:"browserBridgePort"`

	FFmpegInstallDir string `json:"ffmpegInstallDir"`

	M3U8InstallDir             string `json:"m3u8InstallDir"`
	M3U8OutputFormat           string `json:"m3u8OutputFormat"`
	M3U8ThreadCount            int    `json:"m3u8ThreadCount"`
	M3U8RetryCount             int    `json:"m3u8RetryCount"`
	M3U8RequestTimeoutSec      int    `json:"m3u8RequestTimeoutSec"`
	M3U8ConcurrentDownload     bool   `json:"m3u8ConcurrentDownload"`
	M3U8CheckSegmentsCount     bool   `json:"m3u8CheckSegmentsCount"`
	M3U8DeleteAfterDone        bool   `json:"m3u8DeleteAfterDone"`
	M3U8SelectAllAudioSubtitle bool   `json:"m3u8SelectAllAudioSubtitle"`
	M3U8MP4RealTimeDecryption  bool   `json:"m3u8Mp4RealTimeDecryption"`
	M3U8SubtitleFormat         string `json:"m3u8SubtitleFormat"`
}

func DefaultSettings(paths Paths) Settings {
	return Settings{
		DownloadDir:                paths.DownloadDir,
		BlockNum:                   8,
		RetryCount:                 3,
		SpeedLimitKiB:              0,
		MaxConcurrent:              3,
		BrowserExtensionEnabled:    false,
		BrowserPairToken:           NewBrowserPairToken(),
		BrowserBridgePort:          DefaultBrowserBridgePort,
		FFmpegInstallDir:           filepath.Join(paths.RuntimeDir, "FFmpeg"),
		M3U8InstallDir:             filepath.Join(paths.RuntimeDir, "N_m3u8DL-RE"),
		M3U8OutputFormat:           "mp4",
		M3U8ThreadCount:            8,
		M3U8RetryCount:             3,
		M3U8RequestTimeoutSec:      100,
		M3U8ConcurrentDownload:     true,
		M3U8CheckSegmentsCount:     true,
		M3U8DeleteAfterDone:        true,
		M3U8SelectAllAudioSubtitle: true,
		M3U8MP4RealTimeDecryption:  true,
		M3U8SubtitleFormat:         "SRT",
	}
}

func (s Settings) Normalized(paths Paths) Settings {
	defaults := DefaultSettings(paths)
	if s.DownloadDir == "" {
		s.DownloadDir = paths.DownloadDir
	}
	if s.BlockNum <= 0 {
		s.BlockNum = 8
	}
	if s.RetryCount < 0 {
		s.RetryCount = 0
	}
	if s.MaxConcurrent <= 0 {
		s.MaxConcurrent = 3
	}
	if s.SpeedLimitKiB < 0 {
		s.SpeedLimitKiB = 0
	}
	if s.BrowserPairToken == "" {
		s.BrowserPairToken = NewBrowserPairToken()
	}
	if s.BrowserBridgePort <= 0 || s.BrowserBridgePort > 65535 {
		s.BrowserBridgePort = defaults.BrowserBridgePort
	}
	if s.FFmpegInstallDir == "" {
		s.FFmpegInstallDir = defaults.FFmpegInstallDir
	}
	if s.M3U8InstallDir == "" {
		s.M3U8InstallDir = defaults.M3U8InstallDir
	}
	if s.M3U8OutputFormat != "mp4" && s.M3U8OutputFormat != "mkv" {
		s.M3U8OutputFormat = defaults.M3U8OutputFormat
	}
	if s.M3U8ThreadCount <= 0 {
		s.M3U8ThreadCount = defaults.M3U8ThreadCount
	}
	if s.M3U8RetryCount < 0 {
		s.M3U8RetryCount = 0
	}
	if s.M3U8RequestTimeoutSec <= 0 {
		s.M3U8RequestTimeoutSec = defaults.M3U8RequestTimeoutSec
	}
	if s.M3U8SubtitleFormat != "SRT" && s.M3U8SubtitleFormat != "VTT" {
		s.M3U8SubtitleFormat = defaults.M3U8SubtitleFormat
	}
	return s
}

func NewBrowserPairToken() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "local-development-token"
	}
	return base64.RawURLEncoding.EncodeToString(buf[:])
}
