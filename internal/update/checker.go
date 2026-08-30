package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

type Release struct {
	Version string
	Name    string
	URL     string
	Notes   string
	Newer   bool
	Assets  []Asset
}

type Asset struct {
	Name        string
	DownloadURL string
	Size        int64
}

type Checker struct {
	CurrentVersion string
	Owner          string
	Repository     string
	Client         *http.Client
	APIBaseURL     string
}

func (c Checker) Check(ctx context.Context) (Release, error) {
	owner, repository := strings.TrimSpace(c.Owner), strings.TrimSpace(c.Repository)
	if owner == "" || repository == "" {
		return Release{}, fmt.Errorf("update repository is not configured")
	}
	base := strings.TrimRight(c.APIBaseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", base, url.PathEscape(owner), url.PathEscape(repository))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "GhostDownloaderGo/"+strings.TrimSpace(c.CurrentVersion))
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("request latest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("latest release request returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return Release{}, fmt.Errorf("read latest release: %w", err)
	}
	if len(body) > maxResponseBytes {
		return Release{}, fmt.Errorf("latest release response exceeds %d bytes", maxResponseBytes)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Draft   bool   `json:"draft"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, fmt.Errorf("decode latest release: %w", err)
	}
	if payload.Draft || strings.TrimSpace(payload.TagName) == "" || strings.TrimSpace(payload.HTMLURL) == "" {
		return Release{}, fmt.Errorf("latest release payload is incomplete")
	}
	newer, err := IsNewer(c.CurrentVersion, payload.TagName)
	if err != nil {
		return Release{}, err
	}
	assets := make([]Asset, 0, len(payload.Assets))
	for _, asset := range payload.Assets {
		if strings.TrimSpace(asset.Name) != "" && strings.TrimSpace(asset.BrowserDownloadURL) != "" {
			assets = append(assets, Asset{Name: asset.Name, DownloadURL: asset.BrowserDownloadURL, Size: asset.Size})
		}
	}
	return Release{Version: strings.TrimPrefix(payload.TagName, "v"), Name: payload.Name, URL: payload.HTMLURL, Notes: payload.Body, Newer: newer, Assets: assets}, nil
}

func IsNewer(current, candidate string) (bool, error) {
	left, err := parseVersion(current)
	if err != nil {
		return false, fmt.Errorf("current version: %w", err)
	}
	right, err := parseVersion(candidate)
	if err != nil {
		return false, fmt.Errorf("release version: %w", err)
	}
	for i := range left {
		if right[i] != left[i] {
			return right[i] > left[i], nil
		}
	}
	return false, nil
}

func parseVersion(value string) ([3]int, error) {
	var result [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "+", 2)[0]
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("invalid semantic version %q", value)
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return result, fmt.Errorf("invalid semantic version %q", value)
		}
		result[i] = number
	}
	return result, nil
}
