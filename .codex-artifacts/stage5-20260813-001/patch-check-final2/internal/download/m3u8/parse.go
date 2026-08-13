package m3u8download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"
)

const manifestReadLimit = 8 << 20

type ParseResult struct {
	Task         core.Task
	ManifestBody string
	FinalURL     string
	Headers      map[string]string
	ManifestType ManifestType
	IsLive       bool
}

func Parse(ctx context.Context, rawSource string, settings config.Settings, headers map[string]string) (ParseResult, error) {
	source := strings.TrimSpace(rawSource)
	if !IsManifestSource(source) {
		return ParseResult{}, fmt.Errorf("not a m3u8/mpd source")
	}

	body, finalURL, responseHeaders, err := fetchManifest(ctx, source, settings.ProxyURL, headers)
	if err != nil {
		return ParseResult{}, err
	}
	manifestType := DetectManifestType(finalURL, responseHeaders, body)
	isLive := IsLiveManifest(manifestType, body)
	extension := settings.M3U8OutputFormat
	if isLive {
		extension = "ts"
	}
	title := TitleFrom(finalURL, responseHeaders, extension)

	stage := core.NewStage("m3u8", finalURL, 1, settings.M3U8ThreadCount, settings.M3U8RetryCount, false, headers, settings.ProxyURL)
	stage.State = map[string]string{
		"manifestType":           string(manifestType),
		"isLive":                 boolArg(isLive),
		"installDir":             settings.M3U8InstallDir,
		"ffmpegInstallDir":       settings.FFmpegInstallDir,
		"outputFormat":           settings.M3U8OutputFormat,
		"requestTimeoutSec":      itoa(settings.M3U8RequestTimeoutSec),
		"concurrentDownload":     boolArg(settings.M3U8ConcurrentDownload),
		"checkSegmentsCount":     boolArg(settings.M3U8CheckSegmentsCount),
		"deleteAfterDone":        boolArg(settings.M3U8DeleteAfterDone),
		"selectAllAudioSubtitle": boolArg(settings.M3U8SelectAllAudioSubtitle),
		"mp4RealTimeDecryption":  boolArg(settings.M3U8MP4RealTimeDecryption),
		"subtitleFormat":         settings.M3U8SubtitleFormat,
	}
	task := core.NewTask("m3u8", title, source, settings.DownloadDir, 1, stage)
	return ParseResult{
		Task:         task,
		ManifestBody: body,
		FinalURL:     finalURL,
		Headers:      responseHeaders,
		ManifestType: manifestType,
		IsLive:       isLive,
	}, nil
}

func fetchManifest(ctx context.Context, source string, proxyURL string, headers map[string]string) (string, string, map[string]string, error) {
	if localPath := localManifestPath(source); localPath != "" {
		body, err := readLocalManifest(localPath)
		if err != nil {
			return "", "", nil, err
		}
		if !strings.Contains(body, "http://") && !strings.Contains(body, "https://") {
			return "", "", nil, fmt.Errorf("local manifest only contains relative segment paths; use the original online URL instead")
		}
		return body, localPath, nil, nil
	}

	client, err := newClient(proxyURL, 30*time.Second)
	if err != nil {
		return "", "", nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return "", "", nil, err
	}
	for name, value := range headers {
		if strings.TrimSpace(value) != "" {
			request.Header.Set(name, value)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return "", "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", nil, fmt.Errorf("manifest request failed: %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, manifestReadLimit+1))
	if err != nil {
		return "", "", nil, err
	}
	if len(body) > manifestReadLimit {
		return "", "", nil, fmt.Errorf("manifest is larger than %d bytes", manifestReadLimit)
	}
	return string(body), response.Request.URL.String(), lowerHeaders(response.Header), nil
}

func readLocalManifest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, manifestReadLimit+1))
	if err != nil {
		return "", err
	}
	if len(body) > manifestReadLimit {
		return "", fmt.Errorf("manifest is larger than %d bytes", manifestReadLimit)
	}
	return string(body), nil
}

func localManifestPath(source string) string {
	text := strings.TrimSpace(source)
	if text == "" {
		return ""
	}
	if filepath.VolumeName(text) != "" && isManifestPath(text) {
		return text
	}
	parsed, err := url.Parse(text)
	if err == nil && strings.EqualFold(parsed.Scheme, "file") {
		path := parsed.Path
		if parsed.Host != "" {
			path = "//" + parsed.Host + parsed.Path
		}
		if unescaped, err := url.PathUnescape(path); err == nil {
			path = unescaped
		}
		if filepath.VolumeName(path) == "" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		if isManifestPath(path) {
			return path
		}
	}
	if !strings.Contains(text, "://") && isManifestPath(text) {
		return text
	}
	return ""
}

func lowerHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for name, values := range headers {
		if len(values) > 0 {
			result[strings.ToLower(name)] = values[0]
		}
	}
	return result
}
