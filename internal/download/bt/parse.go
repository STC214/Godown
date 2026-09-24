package btdownload

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/net/proxy"

	"ghost-downloader-go-win32/internal/core"
)

const maxMetainfoBytes int64 = 64 << 20

// Options contains everything required to resolve a BitTorrent source without
// consulting application-global configuration.
type Options struct {
	DownloadDir string
	ProxyURL    string
	Headers     map[string]string

	MetadataTimeout       time.Duration
	ListenPort            int
	ConnectionsLimit      int
	DownloadRateLimit     int64
	UploadRateLimit       int64
	EnableDHT             bool
	EnableLSD             bool
	EnableUPnP            bool
	EnableNATPMP          bool
	SequentialDownload    bool
	SeedRatioLimitPercent int
	SeedTimeLimitMinutes  int
	ExtraTrackers         []string
	SaveMagnetTorrentFile bool

	// MagnetResolver is an optional test/application boundary. Production uses
	// anacrolix/torrent v1.61 when this is nil.
	MagnetResolver MagnetResolver
}

// MagnetResolver obtains complete bencoded metainfo for a magnet URI.
type MagnetResolver func(context.Context, string, Options) ([]byte, error)

// IsSource reports whether source is a btih magnet, local .torrent path/file
// URL, or an HTTP(S) URL whose path ends in .torrent.
func IsSource(source string) bool {
	text := strings.TrimSpace(source)
	if text == "" {
		return false
	}
	parsed, err := url.Parse(text)
	if err == nil {
		switch strings.ToLower(parsed.Scheme) {
		case "magnet":
			magnet, err := metainfo.ParseMagnetV2Uri(text)
			return err == nil && (magnet.InfoHash.Ok || magnet.V2InfoHash.Ok)
		case "http", "https":
			return strings.EqualFold(filepath.Ext(parsed.Path), ".torrent")
		case "file":
			return strings.EqualFold(filepath.Ext(parsed.Path), ".torrent")
		}
	}
	return !strings.Contains(text, "://") && strings.EqualFold(filepath.Ext(text), ".torrent")
}

// Resolve loads metadata and returns a scheduler-ready BitTorrent task.
func Resolve(ctx context.Context, source string, options Options) (core.Task, error) {
	if err := ctx.Err(); err != nil {
		return core.Task{}, err
	}
	source = strings.TrimSpace(source)
	if !IsSource(source) {
		return core.Task{}, errors.New("not a supported BitTorrent source")
	}
	options = normalizeOptions(options)

	data, sourceType, resolvedSource, magnetTrackers, err := loadSource(ctx, source, options)
	if err != nil {
		return core.Task{}, err
	}
	mi, err := loadMetainfoBytes(data)
	if err != nil {
		return core.Task{}, err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return core.Task{}, fmt.Errorf("decode torrent info dictionary: %w", err)
	}
	files, total, err := filesFromInfo(info)
	if err != nil {
		return core.Task{}, err
	}

	trackers := ParseTrackers(append(append(append([]string{}, magnetTrackers...), mi.UpvertedAnnounceList().DistinctValues()...), options.ExtraTrackers...)...)
	filesJSON, err := json.Marshal(files)
	if err != nil {
		return core.Task{}, fmt.Errorf("encode BitTorrent files: %w", err)
	}
	trackersJSON, err := json.Marshal(trackers)
	if err != nil {
		return core.Task{}, fmt.Errorf("encode BitTorrent trackers: %w", err)
	}

	stage := core.NewStage("bt", resolvedSource, total, 1, 0, false, cloneHeaders(options.Headers), options.ProxyURL)
	stage.State = map[string]string{
		stateMetainfo:         base64.StdEncoding.EncodeToString(data),
		stateSourceType:       sourceType,
		stateTrackers:         string(trackersJSON),
		stateFiles:            string(filesJSON),
		stateMetadataTimeout:  strconv.FormatInt(int64(options.MetadataTimeout/time.Second), 10),
		stateListenPort:       strconv.Itoa(options.ListenPort),
		stateConnectionsLimit: strconv.Itoa(options.ConnectionsLimit),
		stateDownloadRate:     strconv.FormatInt(options.DownloadRateLimit, 10),
		stateUploadRate:       strconv.FormatInt(options.UploadRateLimit, 10),
		stateEnableDHT:        boolText(options.EnableDHT),
		stateEnableLSD:        boolText(options.EnableLSD),
		stateEnableUPnP:       boolText(options.EnableUPnP),
		stateEnableNATPMP:     boolText(options.EnableNATPMP),
		stateSequential:       boolText(options.SequentialDownload),
		stateSeedRatio:        strconv.Itoa(options.SeedRatioLimitPercent),
		stateSeedTime:         strconv.Itoa(options.SeedTimeLimitMinutes),
		stateSaveMagnet:       boolText(options.SaveMagnetTorrentFile),
	}
	title := safeName(info.BestName(), "torrent")
	task := core.NewTask("bt", title, resolvedSource, options.DownloadDir, total, stage)
	if _, err := storagePaths(task, info, files); err != nil {
		return core.Task{}, err
	}
	return task, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	for name, value := range headers {
		cloned[name] = value
	}
	return cloned
}

func normalizeOptions(options Options) Options {
	options.DownloadDir = strings.TrimSpace(options.DownloadDir)
	if options.DownloadDir == "" {
		options.DownloadDir = "."
	}
	if options.MetadataTimeout <= 0 {
		options.MetadataTimeout = 30 * time.Second
	}
	if options.ListenPort < 0 || options.ListenPort > 65535 {
		options.ListenPort = 0
	}
	if options.ConnectionsLimit <= 0 {
		options.ConnectionsLimit = 500
	}
	if options.DownloadRateLimit < 0 {
		options.DownloadRateLimit = 0
	}
	if options.UploadRateLimit < 0 {
		options.UploadRateLimit = 0
	}
	if options.SeedRatioLimitPercent < 0 {
		options.SeedRatioLimitPercent = 0
	}
	if options.SeedTimeLimitMinutes < 0 {
		options.SeedTimeLimitMinutes = 0
	}
	return options
}

func loadSource(ctx context.Context, source string, options Options) ([]byte, string, string, []string, error) {
	parsed, _ := url.Parse(source)
	switch strings.ToLower(parsed.Scheme) {
	case "magnet":
		magnet, err := metainfo.ParseMagnetV2Uri(source)
		if err != nil {
			return nil, "", "", nil, fmt.Errorf("invalid magnet URI: %w", err)
		}
		resolver := options.MagnetResolver
		if resolver == nil {
			resolver = resolveMagnet
		}
		metadataCtx, cancel := context.WithTimeout(ctx, options.MetadataTimeout)
		defer cancel()
		data, err := resolver(metadataCtx, source, options)
		if err != nil {
			if errors.Is(metadataCtx.Err(), context.DeadlineExceeded) {
				return nil, "", "", nil, fmt.Errorf("magnet metadata timeout after %s: %w", options.MetadataTimeout, metadataCtx.Err())
			}
			if errors.Is(metadataCtx.Err(), context.Canceled) {
				return nil, "", "", nil, metadataCtx.Err()
			}
			return nil, "", "", nil, fmt.Errorf("resolve magnet metadata: %w", err)
		}
		return data, "magnet", source, magnet.Trackers, nil
	case "http", "https":
		data, finalURL, err := fetchTorrent(ctx, source, options)
		return data, "torrent", finalURL, nil, err
	case "file":
		path, err := fileURLPath(parsed)
		if err != nil {
			return nil, "", "", nil, err
		}
		data, err := readLimitedFile(ctx, path)
		return data, "torrent", path, nil, err
	default:
		data, err := readLimitedFile(ctx, source)
		absolute, absErr := filepath.Abs(source)
		if absErr == nil {
			source = absolute
		}
		return data, "torrent", source, nil, err
	}
}

func loadMetainfoBytes(data []byte) (*metainfo.MetaInfo, error) {
	if int64(len(data)) > maxMetainfoBytes {
		return nil, fmt.Errorf("torrent metadata exceeds %d bytes", maxMetainfoBytes)
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode torrent metainfo: %w", err)
	}
	return mi, nil
}

func readLimitedFile(ctx context.Context, path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open torrent file: %w", err)
	}
	defer file.Close()
	if stat, err := file.Stat(); err == nil && stat.Size() > maxMetainfoBytes {
		return nil, fmt.Errorf("torrent metadata exceeds %d bytes", maxMetainfoBytes)
	}
	return readLimitedContext(ctx, file)
}

func fetchTorrent(ctx context.Context, source string, options Options) ([]byte, string, error) {
	client, err := httpClient(options.ProxyURL)
	if err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, "", err
	}
	for name, value := range options.Headers {
		if strings.TrimSpace(value) != "" {
			request.Header.Set(name, value)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetch torrent metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("torrent request failed: %s", response.Status)
	}
	if response.ContentLength > maxMetainfoBytes {
		return nil, "", fmt.Errorf("torrent metadata exceeds %d bytes", maxMetainfoBytes)
	}
	data, err := readLimitedContext(ctx, response.Body)
	if err != nil {
		return nil, "", err
	}
	return data, response.Request.URL.String(), nil
}

func readLimitedContext(ctx context.Context, reader io.Reader) ([]byte, error) {
	var buffer bytes.Buffer
	chunk := make([]byte, 32*1024)
	limited := io.LimitReader(reader, maxMetainfoBytes+1)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		read, err := limited.Read(chunk)
		if read > 0 {
			_, _ = buffer.Write(chunk[:read])
			if int64(buffer.Len()) > maxMetainfoBytes {
				return nil, fmt.Errorf("torrent metadata exceeds %d bytes", maxMetainfoBytes)
			}
		}
		if errors.Is(err, io.EOF) {
			return buffer.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func fileURLPath(parsed *url.URL) (string, error) {
	if parsed == nil || !strings.EqualFold(parsed.Scheme, "file") {
		return "", errors.New("not a file URL")
	}
	path := parsed.Path
	if len(parsed.Host) == 2 && parsed.Host[1] == ':' {
		path = parsed.Host + parsed.Path
	} else if parsed.Host != "" {
		path = "//" + parsed.Host + parsed.Path
	} else if parsed.Opaque != "" {
		path = parsed.Opaque
	}
	unescaped, err := url.PathUnescape(path)
	if err != nil {
		return "", fmt.Errorf("decode file URL: %w", err)
	}
	if len(unescaped) >= 3 && unescaped[0] == '/' && unescaped[2] == ':' {
		unescaped = unescaped[1:]
	}
	return filepath.FromSlash(unescaped), nil
}

func httpClient(proxyURL string) (*http.Client, error) {
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	text := strings.TrimSpace(proxyURL)
	if text == "" {
		return &http.Client{Transport: transport}, nil
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
	case "socks5", "socks5h":
		dialer, err := proxy.FromURL(parsed, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("invalid SOCKS proxy: %w", err)
		}
		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			transport.DialContext = contextDialer.DialContext
		} else {
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				result := make(chan struct {
					connection net.Conn
					err        error
				}, 1)
				go func() {
					connection, err := dialer.Dial(network, address)
					result <- struct {
						connection net.Conn
						err        error
					}{connection, err}
				}()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case result := <-result:
					return result.connection, result.err
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}
	return &http.Client{Transport: transport}, nil
}

func resolveMagnet(ctx context.Context, source string, options Options) ([]byte, error) {
	config := torrent.NewDefaultClientConfig()
	metadataDir, err := os.MkdirTemp("", "ghost-downloader-bt-metadata-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(metadataDir)
	config.DataDir = metadataDir
	config.NoDHT = !options.EnableDHT
	config.NoDefaultPortForwarding = !options.EnableUPnP && !options.EnableNATPMP
	config.ListenPort = options.ListenPort
	config.NoUpload = true
	config.Seed = false
	config.EstablishedConnsPerTorrent = options.ConnectionsLimit
	if err := applyTorrentProxy(config, options.ProxyURL); err != nil {
		return nil, err
	}
	applyTorrentHeaders(config, options.Headers)
	client, err := newClientWithPortFallback(config)
	if err != nil {
		return nil, fmt.Errorf("create BitTorrent metadata client: %w", err)
	}
	defer client.Close()
	spec, err := torrent.TorrentSpecFromMagnetUri(source)
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	prepareTorrentSpecForClient(spec)
	torrentHandle, _, err := client.AddTorrentSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("add magnet: %w", err)
	}
	defer torrentHandle.Drop()
	if trackers := ParseTrackers(options.ExtraTrackers...); len(trackers) > 0 {
		torrentHandle.AddTrackers([][]string{trackers})
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-torrentHandle.GotInfo():
	}
	mi := torrentHandle.Metainfo()
	var buffer bytes.Buffer
	if err := mi.Write(&buffer); err != nil {
		return nil, fmt.Errorf("encode magnet metainfo: %w", err)
	}
	if int64(buffer.Len()) > maxMetainfoBytes {
		return nil, fmt.Errorf("torrent metadata exceeds %d bytes", maxMetainfoBytes)
	}
	return buffer.Bytes(), nil
}

func prepareTorrentSpecForClient(spec *torrent.TorrentSpec) {
	if spec == nil || !spec.InfoHash.IsZero() || !spec.InfoHashV2.Ok {
		return
	}
	spec.InfoHash = *spec.InfoHashV2.Value.ToShort()
	spec.InfoHashV2.SetNone()
}
