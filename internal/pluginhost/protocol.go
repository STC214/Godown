package pluginhost

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const ProtocolVersion = 1

var validPluginID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Manifest is the host-readable plugin descriptor stored as manifest.json.
// Executable may be absolute, relative to the manifest directory, or available
// through PATH. Plugins are always launched out of process with --stdio.
type Manifest struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	ProtocolVersion int               `json:"protocolVersion"`
	Executable      string            `json:"executable"`
	Args            []string          `json:"args,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	TimeoutMS       int               `json:"timeoutMs,omitempty"`
}

func (m Manifest) validate() error {
	if !validPluginID.MatchString(m.ID) {
		return fmt.Errorf("invalid plugin id %q", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("plugin %q requires name and version", m.ID)
	}
	if m.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("plugin %q protocol version %d is unsupported", m.ID, m.ProtocolVersion)
	}
	if strings.TrimSpace(m.Executable) == "" {
		return fmt.Errorf("plugin %q requires executable", m.ID)
	}
	if m.TimeoutMS < 0 || m.TimeoutMS > 120000 {
		return fmt.Errorf("plugin %q timeoutMs must be between 0 and 120000", m.ID)
	}
	return nil
}

func (m Manifest) timeout() time.Duration {
	if m.TimeoutMS == 0 {
		return 10 * time.Second
	}
	return time.Duration(m.TimeoutMS) * time.Millisecond
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type MatchParams struct {
	URL string `json:"url"`
}

type MatchResult struct {
	Matched bool `json:"matched"`
}

type ParseParams struct {
	URL         string            `json:"url"`
	DownloadDir string            `json:"downloadDir"`
	Headers     map[string]string `json:"headers,omitempty"`
	ProxyURL    string            `json:"proxyUrl,omitempty"`
}

// ParseResult is deliberately declarative: a plugin can classify and rewrite
// a source, but the host remains responsible for constructing and scheduling
// the actual download task.
type ParseResult struct {
	Kind    string            `json:"kind"`
	URL     string            `json:"url"`
	Title   string            `json:"title,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (r ParseResult) validate() error {
	switch r.Kind {
	case "http", "m3u8", "bt":
	default:
		return fmt.Errorf("unsupported parsed kind %q", r.Kind)
	}
	if strings.TrimSpace(r.URL) == "" {
		return fmt.Errorf("plugin parse result has empty url")
	}
	return nil
}
