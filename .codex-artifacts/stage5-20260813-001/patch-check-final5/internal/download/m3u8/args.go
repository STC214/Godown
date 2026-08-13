package m3u8download

import (
	"path/filepath"
	"sort"
	"strings"
)

type Options struct {
	URL        string
	SaveDir    string
	Title      string
	TaskID     string
	FFmpegPath string

	Headers  map[string]string
	ProxyURL string

	ThreadCount        int
	RetryCount         int
	RequestTimeoutSec  int
	AutoSelect         bool
	ConcurrentDownload bool
	AppendURLParams    bool
	BinaryMerge        bool
	CheckSegmentsCount bool
	DeleteAfterDone    bool

	OutputFormat           string
	CustomMuxAfterDone     string
	SubtitleFormat         string
	SelectVideo            string
	SelectAllAudioSubtitle bool
	MaxSpeed               int
	SpeedUnit              string
	AdKeyword              string
	NoDateInfo             bool
	DecryptionEngine       string
	DecryptionBinaryPath   string
	MP4RealTimeDecryption  bool
	DecryptionKeys         []string
	KeyTextFile            string
	MuxImports             []string
	IsLive                 bool
	LiveKeepSegments       bool
	LivePipeMux            bool
	LiveFixVTT             bool
	LiveWaitTime           int
	LiveTakeCount          int
	LiveRecordLimit        string
}

func (o Options) BuildArgs() []string {
	o = o.normalized()
	args := []string{
		o.URL,
		"--save-dir=" + slash(o.SaveDir),
		"--save-name=" + saveName(o.Title),
		"--tmp-dir=" + slash(filepath.Join(o.SaveDir, ".gd3_m3u8", o.TaskID)),
		"--thread-count=" + itoa(o.ThreadCount),
		"--download-retry-count=" + itoa(o.RetryCount),
		"--http-request-timeout=" + itoa(o.RequestTimeoutSec),
		"--concurrent-download=" + boolArg(o.ConcurrentDownload),
		"--append-url-params=" + boolArg(o.AppendURLParams),
		"--binary-merge=" + boolArg(o.BinaryMerge),
		"--check-segments-count=" + boolArg(o.CheckSegmentsCount),
		"--del-after-done=" + boolArg(o.DeleteAfterDone),
		"--sub-format=" + o.SubtitleFormat,
		"--write-meta-json=false",
		"--no-log=true",
		"--no-ansi-color=true",
		"--disable-update-check=true",
	}

	if o.SelectAllAudioSubtitle {
		selected := o.SelectVideo
		if selected == "" {
			selected = "best"
		}
		args = append(args, "--select-video="+selected, "--select-audio=all", "--select-subtitle=all")
	} else if o.SelectVideo != "" {
		args = append(args, "--select-video="+o.SelectVideo, "--auto-select="+boolArg(o.AutoSelect))
	} else {
		args = append(args, "--auto-select="+boolArg(o.AutoSelect))
	}

	if o.MaxSpeed > 0 {
		args = append(args, "--max-speed="+itoa(o.MaxSpeed)+o.SpeedUnit)
	}
	if o.AdKeyword != "" {
		args = append(args, "--ad-keyword="+o.AdKeyword)
	}
	if o.NoDateInfo {
		args = append(args, "--no-date-info=true")
	}

	args = append(args, "--use-system-proxy=false")
	if proxy := normalizeProxy(o.ProxyURL); proxy != "" {
		args = append(args, "--custom-proxy="+proxy)
	}
	if o.FFmpegPath != "" {
		args = append(args, "--ffmpeg-binary-path="+slash(o.FFmpegPath))
	}

	args = append(args,
		"--decryption-engine="+decryptionEngine(o.DecryptionEngine),
		"--mp4-real-time-decryption="+boolArg(o.MP4RealTimeDecryption),
	)
	if o.DecryptionBinaryPath != "" {
		args = append(args, "--decryption-binary-path="+slash(o.DecryptionBinaryPath))
	}
	for _, key := range o.DecryptionKeys {
		if text := strings.TrimSpace(key); text != "" {
			args = append(args, "--key="+text)
		}
	}
	if o.KeyTextFile != "" {
		args = append(args, "--key-text-file="+slash(o.KeyTextFile))
	}

	if o.IsLive {
		args = append(args,
			"--live-real-time-merge=true",
			"--live-keep-segments="+boolArg(o.LiveKeepSegments),
			"--live-pipe-mux="+boolArg(o.LivePipeMux),
		)
		if o.LiveFixVTT {
			args = append(args, "--live-fix-vtt-by-audio=true")
		}
		if o.LiveWaitTime > 0 {
			args = append(args, "--live-wait-time="+itoa(o.LiveWaitTime))
		}
		if o.LiveTakeCount > 0 {
			args = append(args, "--live-take-count="+itoa(o.LiveTakeCount))
		}
		if o.LiveRecordLimit != "" {
			args = append(args, "--live-record-limit="+o.LiveRecordLimit)
		}
	} else if o.CustomMuxAfterDone != "" {
		args = append(args, "--mux-after-done="+o.CustomMuxAfterDone)
	} else {
		mux := "format=" + o.OutputFormat + ":muxer=ffmpeg"
		if o.FFmpegPath != "" {
			mux += ":bin_path=" + slash(o.FFmpegPath)
		}
		args = append(args, "--mux-after-done="+mux)
	}

	for _, item := range o.MuxImports {
		if text := strings.TrimSpace(item); text != "" {
			args = append(args, "--mux-import="+text)
		}
	}

	headerNames := make([]string, 0, len(o.Headers))
	for name := range o.Headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	for _, name := range headerNames {
		value := strings.TrimSpace(o.Headers[name])
		if value != "" {
			args = append(args, "-H", name+": "+value)
		}
	}
	return args
}

func (o Options) normalized() Options {
	if o.ThreadCount <= 0 {
		o.ThreadCount = 8
	}
	if o.RetryCount < 0 {
		o.RetryCount = 0
	}
	if o.RequestTimeoutSec <= 0 {
		o.RequestTimeoutSec = 100
	}
	if o.OutputFormat == "" {
		o.OutputFormat = "mp4"
	}
	if o.SubtitleFormat == "" {
		o.SubtitleFormat = "SRT"
	}
	if o.SpeedUnit == "" {
		o.SpeedUnit = "Mbps"
	}
	if o.TaskID == "" {
		o.TaskID = "task"
	}
	return o
}

func boolArg(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func normalizeProxy(proxy string) string {
	proxy = strings.TrimSpace(proxy)
	if strings.HasPrefix(proxy, "socks5h://") {
		return "socks5://" + strings.TrimPrefix(proxy, "socks5h://")
	}
	return proxy
}

func decryptionEngine(engine string) string {
	switch engine {
	case "MP4Decrypt":
		return "MP4DECRYPT"
	case "Shaka Packager":
		return "SHAKA_PACKAGER"
	default:
		return "FFMPEG"
	}
}

func saveName(title string) string {
	return strings.TrimSuffix(title, filepath.Ext(title))
}

func slash(path string) string {
	return filepath.ToSlash(path)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	negative := value < 0
	if negative {
		value = -value
	}
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
