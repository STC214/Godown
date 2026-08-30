package pluginhost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
)

const (
	manifestFilename = "manifest.json"
	maxMessageBytes  = 4 << 20
	maxStderrBytes   = 64 << 10
)

type Plugin struct {
	Manifest    Manifest
	ManifestDir string
}

type Manager struct {
	plugins []Plugin
	request atomic.Int64
}

// Discover finds one manifest.json per immediate child directory. A malformed
// plugin is reported while valid siblings remain available to the caller.
func Discover(root string) (*Manager, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return &Manager{}, nil
		}
		return nil, fmt.Errorf("read plugin directory: %w", err)
	}
	var plugins []Plugin
	var problems []error
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		data, readErr := os.ReadFile(filepath.Join(dir, manifestFilename))
		if readErr != nil {
			if !os.IsNotExist(readErr) {
				problems = append(problems, fmt.Errorf("plugin directory %q: %w", entry.Name(), readErr))
			}
			continue
		}
		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			problems = append(problems, fmt.Errorf("plugin directory %q manifest: %w", entry.Name(), err))
			continue
		}
		if err := manifest.validate(); err != nil {
			problems = append(problems, err)
			continue
		}
		if _, exists := seen[manifest.ID]; exists {
			problems = append(problems, fmt.Errorf("duplicate plugin id %q", manifest.ID))
			continue
		}
		seen[manifest.ID] = struct{}{}
		plugins = append(plugins, Plugin{Manifest: manifest, ManifestDir: dir})
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Manifest.ID < plugins[j].Manifest.ID })
	return &Manager{plugins: plugins}, errors.Join(problems...)
}

func (m *Manager) Plugins() []Manifest {
	if m == nil {
		return nil
	}
	result := make([]Manifest, len(m.plugins))
	for i := range m.plugins {
		result[i] = m.plugins[i].Manifest
	}
	return result
}

// Resolve asks plugins in stable ID order. Process failures are isolated to the
// plugin call; the host process and the remaining plugins continue normally.
func (m *Manager) Resolve(ctx context.Context, params ParseParams) (ParseResult, bool, error) {
	if m == nil {
		return ParseResult{}, false, nil
	}
	var problems []error
	for i := range m.plugins {
		plugin := &m.plugins[i]
		var match MatchResult
		if err := m.call(ctx, plugin, "matches", MatchParams{URL: params.URL}, &match); err != nil {
			problems = append(problems, fmt.Errorf("plugin %q matches: %w", plugin.Manifest.ID, err))
			continue
		}
		if !match.Matched {
			continue
		}
		var parsed ParseResult
		if err := m.call(ctx, plugin, "parse", params, &parsed); err != nil {
			return ParseResult{}, true, fmt.Errorf("plugin %q parse: %w", plugin.Manifest.ID, err)
		}
		if err := parsed.validate(); err != nil {
			return ParseResult{}, true, fmt.Errorf("plugin %q parse: %w", plugin.Manifest.ID, err)
		}
		return parsed, true, nil
	}
	return ParseResult{}, false, errors.Join(problems...)
}

func (m *Manager) CheckManifest(ctx context.Context, plugin Plugin) (Manifest, error) {
	var remote Manifest
	if err := m.call(ctx, &plugin, "manifest", struct{}{}, &remote); err != nil {
		return Manifest{}, err
	}
	if err := remote.validate(); err != nil {
		return Manifest{}, err
	}
	if remote.ID != plugin.Manifest.ID {
		return Manifest{}, fmt.Errorf("manifest id mismatch: disk=%q rpc=%q", plugin.Manifest.ID, remote.ID)
	}
	return remote, nil
}

func (m *Manager) call(parent context.Context, plugin *Plugin, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(parent, plugin.Manifest.timeout())
	defer cancel()

	executable := plugin.Manifest.Executable
	if !filepath.IsAbs(executable) {
		candidate := filepath.Join(plugin.ManifestDir, executable)
		if _, err := os.Stat(candidate); err == nil {
			executable = candidate
		} else if strings.ContainsRune(executable, filepath.Separator) || strings.ContainsRune(executable, '/') {
			executable = candidate
		}
	}
	args := append(append([]string{}, plugin.Manifest.Args...), "--stdio")
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = plugin.ManifestDir
	cmd.Env = os.Environ()
	for key, value := range plugin.Manifest.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	id := m.request.Add(1)
	request := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	encodeErr := json.NewEncoder(stdin).Encode(request)
	closeErr := stdin.Close()
	if encodeErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("write request: %w", encodeErr)
	}
	if closeErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("close request: %w", closeErr)
	}

	line, readErr := bufio.NewReader(io.LimitReader(stdout, maxMessageBytes+1)).ReadBytes('\n')
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return fmt.Errorf("timed out after %s", plugin.Manifest.timeout())
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("read response: %w", readErr)
	}
	if len(line) > maxMessageBytes {
		return fmt.Errorf("response exceeds %d bytes", maxMessageBytes)
	}
	if waitErr != nil {
		return fmt.Errorf("process exited: %w%s", waitErr, stderr.suffix())
	}
	var response rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(line), &response); err != nil {
		return fmt.Errorf("invalid JSON response: %w%s", err, stderr.suffix())
	}
	if response.JSONRPC != "2.0" || response.ID != id {
		return fmt.Errorf("invalid JSON-RPC response envelope")
	}
	if response.Error != nil {
		return fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 {
		return fmt.Errorf("JSON-RPC response has no result")
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	return nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := maxStderrBytes - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return original, nil
}

func (b *limitedBuffer) suffix() string {
	message := strings.TrimSpace(b.String())
	if message == "" {
		return ""
	}
	return ": " + message
}
