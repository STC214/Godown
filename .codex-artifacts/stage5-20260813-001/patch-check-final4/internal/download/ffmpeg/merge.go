package ffmpegdownload

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"
)

const mergeStateResources = "resources"

type MergeResource struct {
	URL           string            `json:"url"`
	Filename      string            `json:"filename"`
	Mime          string            `json:"mime"`
	Size          int64             `json:"size"`
	Headers       map[string]string `json:"headers,omitempty"`
	SupportsRange bool              `json:"supportsRange"`
	Role          string            `json:"role"`
}

func NewMergeTask(title, outputDir string, resources []MergeResource, settings config.Settings) (core.Task, error) {
	if len(resources) != 2 {
		return core.Task{}, fmt.Errorf("resource merge requires exactly 2 resources")
	}
	if strings.TrimSpace(outputDir) == "" {
		outputDir = settings.DownloadDir
	}
	title = safeTitle(title)
	if !strings.EqualFold(filepath.Ext(title), ".mp4") {
		title += ".mp4"
	}

	resources = assignRoles(resources)
	for _, resource := range resources {
		if !strings.HasPrefix(strings.ToLower(resource.URL), "http://") && !strings.HasPrefix(strings.ToLower(resource.URL), "https://") {
			return core.Task{}, fmt.Errorf("resource merge only supports HTTP resources")
		}
	}
	body, err := json.Marshal(resources)
	if err != nil {
		return core.Task{}, err
	}
	var size int64
	for _, resource := range resources {
		if resource.Size > 0 {
			size += resource.Size
		}
	}
	stage := core.NewStage("ffmpeg_merge", resources[0].URL, size, 1, settings.RetryCount, false, nil, settings.ProxyURL)
	stage.State = map[string]string{
		mergeStateResources: string(body),
		"ffmpegInstallDir":  settings.FFmpegInstallDir,
	}
	return core.NewTask("ffmpeg", title, resources[0].URL, outputDir, size, stage), nil
}

func ResourcesFromState(task core.Task) ([]MergeResource, error) {
	var resources []MergeResource
	if err := json.Unmarshal([]byte(task.Stage.State[mergeStateResources]), &resources); err != nil {
		return nil, err
	}
	if len(resources) != 2 {
		return nil, fmt.Errorf("resource merge requires exactly 2 resources")
	}
	return assignRoles(resources), nil
}

func assignRoles(resources []MergeResource) []MergeResource {
	next := make([]MergeResource, len(resources))
	copy(next, resources)
	for i := range next {
		if next[i].Role == "" {
			next[i].Role = "video"
			if i == 1 {
				next[i].Role = "audio"
			}
		}
	}
	return next
}

func safeTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "merged-media"
	}
	replacer := strings.NewReplacer("<", "_", ">", "_", ":", "_", `"`, "_", "/", "_", `\`, "_", "|", "_", "?", "_", "*", "_")
	title = replacer.Replace(title)
	title = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return '_'
		}
		return r
	}, title)
	title = strings.Trim(title, ". ")
	if title == "" {
		return "merged-media"
	}
	return title
}

func resourceExtension(resource MergeResource) string {
	for _, value := range []string{resource.Filename, resource.URL} {
		if value == "" {
			continue
		}
		target := value
		if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
			target = parsed.Path
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(target)), ".")
		if ext != "" {
			return ext
		}
	}
	if strings.Contains(strings.ToLower(resource.Mime), "audio") {
		return "m4a"
	}
	return "mp4"
}
