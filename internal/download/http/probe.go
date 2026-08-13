package httpdownload

import (
	"context"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ghost-downloader-go-win32/internal/core"
)

const (
	UnknownSize      int64 = 0
	NotSupportedSize int64 = -1
)

var unsafeFilename = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

var preferredContentTypeExtensions = map[string]string{
	"application/gzip":             ".gz",
	"application/javascript":       ".js",
	"application/json":             ".json",
	"application/pdf":              ".pdf",
	"application/vnd.rar":          ".rar",
	"application/wasm":             ".wasm",
	"application/x-7z-compressed":  ".7z",
	"application/x-bittorrent":     ".torrent",
	"application/x-gzip":           ".gz",
	"application/x-rar-compressed": ".rar",
	"application/zip":              ".zip",
	"audio/mpeg":                   ".mp3",
	"image/gif":                    ".gif",
	"image/jpeg":                   ".jpg",
	"image/png":                    ".png",
	"image/webp":                   ".webp",
	"text/css":                     ".css",
	"text/html":                    ".html",
	"text/javascript":              ".js",
	"text/plain":                   ".txt",
	"video/mp4":                    ".mp4",
	"video/webm":                   ".webm",
}

type ProbeResult struct {
	URL           string
	Filename      string
	FileSize      int64
	SupportsRange bool
	Headers       map[string]string
}

func Parse(ctx context.Context, rawURL, downloadDir string, blockNum int, retryCount int, proxyURL string, headers map[string]string) (core.Task, error) {
	result, err := Probe(ctx, rawURL, proxyURL, headers)
	if err != nil {
		return core.Task{}, err
	}
	if blockNum <= 0 {
		blockNum = 8
	}
	stage := core.NewStage("http", result.URL, result.FileSize, blockNum, retryCount, result.SupportsRange, headers, proxyURL)
	return core.NewTask("http", result.Filename, rawURL, downloadDir, result.FileSize, stage), nil
}

func Probe(ctx context.Context, rawURL, proxyURL string, baseHeaders map[string]string) (ProbeResult, error) {
	client, err := newClient(proxyURL, 30*time.Second)
	if err != nil {
		return ProbeResult{}, err
	}
	probeHeaders := cloneHeaders(baseHeaders)
	probeHeaders["Range"] = "bytes=1-1"
	probeHeaders["Accept-Encoding"] = "identity"
	status, headers, finalURL, err := sendProbe(ctx, client, rawURL, probeHeaders)
	if err != nil {
		return ProbeResult{}, err
	}

	fileSize := rangeSize(headers)
	supportsRange := status == http.StatusPartialContent && headers.Get("Content-Range") != ""
	if supportsRange {
		return ProbeResult{
			URL:           finalURL,
			Filename:      filenameFrom(finalURL, headers),
			FileSize:      fileSize,
			SupportsRange: true,
			Headers:       lowerHeaders(headers),
		}, nil
	}

	fileSize = contentLength(headers)
	if status == http.StatusOK && (fileSize == UnknownSize || fileSize == 1) {
		fallbackProbeHeaders := cloneHeaders(baseHeaders)
		fallbackProbeHeaders["Range"] = "bytes=0-0"
		fallbackProbeHeaders["Accept-Encoding"] = "identity"
		fallbackStatus, fallbackHeaders, _, err := sendProbe(ctx, client, rawURL, fallbackProbeHeaders)
		if err == nil {
			fallbackSize := rangeSize(fallbackHeaders)
			if fallbackStatus == http.StatusPartialContent && fallbackHeaders.Get("Content-Range") != "" {
				return ProbeResult{
					URL:           finalURL,
					Filename:      filenameFrom(finalURL, fallbackHeaders),
					FileSize:      fallbackSize,
					SupportsRange: true,
					Headers:       lowerHeaders(fallbackHeaders),
				}, nil
			}
			if fileSize == UnknownSize {
				fileSize = contentLength(fallbackHeaders)
				if fileSize == UnknownSize && fallbackStatus == http.StatusRequestedRangeNotSatisfiable {
					fileSize = rangeSize(fallbackHeaders)
				}
			}
		}
	}

	return ProbeResult{
		URL:           finalURL,
		Filename:      filenameFrom(finalURL, headers),
		FileSize:      fileSize,
		SupportsRange: false,
		Headers:       lowerHeaders(headers),
	}, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		result[key] = value
	}
	return result
}

func sendProbe(ctx context.Context, client *http.Client, rawURL string, headers map[string]string) (int, http.Header, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, "", err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return 0, nil, "", respError(resp)
	}
	return resp.StatusCode, resp.Header.Clone(), resp.Request.URL.String(), nil
}

func contentLength(headers http.Header) int64 {
	value := strings.TrimSpace(headers.Get("Content-Length"))
	if value == "" {
		return UnknownSize
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size <= 0 {
		return UnknownSize
	}
	return size
}

func rangeSize(headers http.Header) int64 {
	value := strings.TrimSpace(headers.Get("Content-Range"))
	if value == "" {
		return UnknownSize
	}
	_, total, ok := strings.Cut(value, "/")
	if !ok || total == "" || total == "*" {
		return UnknownSize
	}
	size, err := strconv.ParseInt(total, 10, 64)
	if err != nil || size <= 0 {
		return UnknownSize
	}
	return size
}

func filenameFrom(rawURL string, headers http.Header) string {
	contentType := headers.Get("Content-Type")
	if name := filenameFromContentDisposition(headers.Get("Content-Disposition")); name != "" {
		return safeFilename(withContentTypeExtension(name, contentType))
	}
	if name := filenameFromLocation(headers.Get("Content-Location")); name != "" {
		return safeFilename(withContentTypeExtension(name, contentType))
	}
	parsed, err := url.Parse(rawURL)
	if err == nil {
		if name := filenameFromContentDisposition(parsed.Query().Get("response-content-disposition")); name != "" {
			return safeFilename(withContentTypeExtension(name, contentType))
		}
		if base := path.Base(parsed.Path); base != "." && base != "/" && base != "" {
			if unescaped, err := url.PathUnescape(base); err == nil {
				return safeFilename(withContentTypeExtension(stripPathParams(unescaped), contentType))
			}
			return safeFilename(withContentTypeExtension(stripPathParams(base), contentType))
		}
	}
	return safeFilename("file_" + strconv.FormatInt(time.Now().UnixNano(), 10) + contentTypeExtension(contentType))
}

func filenameFromContentDisposition(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(value)
	if err == nil {
		if name := strings.TrimSpace(params["filename*"]); name != "" {
			return strings.Trim(name, `"' `)
		}
		if name := strings.TrimSpace(params["filename"]); name != "" {
			return strings.Trim(name, `"' `)
		}
	}
	if name := filenameParam(value, "filename*"); name != "" {
		if decoded := decodeRFC5987(name); decoded != "" {
			return decoded
		}
		return strings.Trim(name, `"' `)
	}
	if name := filenameParam(value, "filename"); name != "" {
		if unescaped, err := url.QueryUnescape(strings.Trim(name, `"' `)); err == nil {
			return unescaped
		}
		return strings.Trim(name, `"' `)
	}
	return ""
}

func filenameParam(value, key string) string {
	pattern := `(?i)(?:^|;)\s*` + regexp.QuoteMeta(key) + `\s*=\s*("(?:\\.|[^"])*"|'[^']*'|[^;]+)`
	match := regexp.MustCompile(pattern).FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func decodeRFC5987(value string) string {
	value = strings.Trim(value, `"' `)
	first := strings.Index(value, "'")
	if first < 0 {
		return ""
	}
	second := strings.Index(value[first+1:], "'")
	if second < 0 {
		return ""
	}
	second += first + 1
	charset := strings.ToLower(value[:first])
	encoded := value[second+1:]
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		return ""
	}
	switch charset {
	case "utf-8", "us-ascii", "":
		return decoded
	case "iso-8859-1", "latin1", "latin-1":
		runes := make([]rune, 0, len(decoded))
		for i := 0; i < len(decoded); i++ {
			runes = append(runes, rune(decoded[i]))
		}
		return string(runes)
	default:
		return decoded
	}
}

func filenameFromLocation(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Path != "" {
		value = parsed.Path
	}
	base := path.Base(value)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if unescaped, err := url.PathUnescape(base); err == nil {
		return stripPathParams(unescaped)
	}
	return stripPathParams(base)
}

func stripPathParams(value string) string {
	if before, _, ok := strings.Cut(value, ";"); ok {
		return before
	}
	return value
}

func withContentTypeExtension(name, contentType string) string {
	if strings.Contains(path.Base(name), ".") {
		return name
	}
	return name + contentTypeExtension(contentType)
}

func contentTypeExtension(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	if mediaType == "" {
		return ""
	}
	if extension := preferredContentTypeExtensions[strings.ToLower(mediaType)]; extension != "" {
		return extension
	}
	extensions, err := mime.ExtensionsByType(strings.ToLower(mediaType))
	if err != nil || len(extensions) == 0 {
		return ""
	}
	return extensions[0]
}

func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = unsafeFilename.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	if name == "" {
		return "download"
	}
	return name
}

func lowerHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			result[strings.ToLower(key)] = values[0]
		}
	}
	return result
}
