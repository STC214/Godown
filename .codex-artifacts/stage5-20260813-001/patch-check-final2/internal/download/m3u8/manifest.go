package m3u8download

import (
	"mime"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

type ManifestType string

const (
	ManifestHLS  ManifestType = "m3u8"
	ManifestDASH ManifestType = "mpd"
)

var unsafeFilename = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

var manifestSuffixes = map[string]struct{}{
	".m3u8": {},
	".m3u":  {},
	".mpd":  {},
}

var knownOutputSuffixes = map[string]struct{}{
	".m3u8": {},
	".m3u":  {},
	".mpd":  {},
	".mp4":  {},
	".mkv":  {},
	".ts":   {},
	".webm": {},
	".m4a":  {},
	".m4v":  {},
	".vtt":  {},
	".srt":  {},
}

func IsManifestSource(source string) bool {
	text := strings.TrimSpace(source)
	if text == "" {
		return false
	}
	if filepath.VolumeName(text) != "" {
		return isManifestPath(text)
	}
	parsed, err := url.Parse(text)
	if err == nil {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https":
			lowered := strings.ToLower(text)
			return strings.Contains(lowered, ".m3u") || strings.Contains(lowered, ".mpd")
		case "file":
			return isManifestPath(parsed.Path)
		}
		if parsed.Scheme != "" {
			return false
		}
	}
	return isManifestPath(text)
}

func DetectManifestType(source string, headers map[string]string, body string) ManifestType {
	loweredURL := strings.ToLower(source)
	contentType := headerValue(headers, "content-type")
	sample := strings.ToLower(strings.TrimSpace(body))
	if strings.Contains(loweredURL, ".mpd") || strings.Contains(contentType, "dash+xml") || strings.HasPrefix(sample, "<mpd") {
		return ManifestDASH
	}
	return ManifestHLS
}

func IsLiveManifest(kind ManifestType, body string) bool {
	lowered := strings.ToLower(body)
	if kind == ManifestDASH {
		return strings.Contains(lowered, `type="dynamic"`) || strings.Contains(lowered, `type='dynamic'`)
	}
	if !strings.Contains(lowered, "#extm3u") {
		return false
	}
	return !strings.Contains(lowered, "#ext-x-endlist")
}

func TitleFrom(source string, headers map[string]string, extension string) string {
	if extension == "" {
		extension = "mp4"
	}
	candidates := make([]string, 0, 4)
	if disposition := headerValue(headers, "content-disposition"); disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if filename := strings.TrimSpace(params["filename*"]); filename != "" {
				candidates = append(candidates, filename)
			}
			if filename := strings.TrimSpace(params["filename"]); filename != "" {
				candidates = append(candidates, filename)
			}
		}
	}
	if parsed, err := url.Parse(source); err == nil {
		query := parsed.Query()
		for _, key := range []string{"filename", "file", "name", "title"} {
			if value := strings.TrimSpace(query.Get(key)); value != "" {
				candidates = append(candidates, value)
			}
		}
		if parsed.Path != "" {
			if unescaped, err := url.PathUnescape(filepath.Base(parsed.Path)); err == nil {
				candidates = append(candidates, unescaped)
			} else {
				candidates = append(candidates, filepath.Base(parsed.Path))
			}
		}
	} else {
		candidates = append(candidates, filepath.Base(source))
	}

	for _, candidate := range candidates {
		stem := stripKnownSuffix(safeFilename(candidate))
		if stem != "" {
			return stem + "." + extension
		}
	}
	return "stream." + extension
}

func isManifestPath(path string) bool {
	_, ok := manifestSuffixes[strings.ToLower(filepath.Ext(strings.TrimSpace(path)))]
	return ok
}

func stripKnownSuffix(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if _, ok := knownOutputSuffixes[ext]; ok && len(name) > len(ext) {
		return name[:len(name)-len(ext)]
	}
	return name
}

func safeFilename(name string) string {
	clean := strings.Trim(unsafeFilename.ReplaceAllString(name, "_"), " ._")
	if clean == "" {
		return "stream"
	}
	return clean
}

func headerValue(headers map[string]string, key string) string {
	for name, value := range headers {
		if strings.EqualFold(name, key) {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return ""
}
