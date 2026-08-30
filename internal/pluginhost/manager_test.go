package pluginhost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoverResolveAndRPCManifest(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "example", "normal", 10000)
	manager, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := manager.Plugins(); len(got) != 1 || got[0].ID != "example" {
		t.Fatalf("unexpected plugins: %#v", got)
	}
	remote, err := manager.CheckManifest(context.Background(), manager.plugins[0])
	if err != nil || remote.ID != "example" {
		t.Fatalf("remote manifest = %#v, %v", remote, err)
	}
	parsed, matched, err := manager.Resolve(context.Background(), ParseParams{URL: "example+https://host/file.bin", DownloadDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !matched || parsed.Kind != "http" || parsed.URL != "https://host/file.bin" || parsed.Title != "plugin-file.bin" {
		t.Fatalf("unexpected parse result: matched=%v result=%#v", matched, parsed)
	}
}

func TestBrokenPluginDoesNotPreventNextPlugin(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "a-broken", "invalid", 10000)
	writeTestManifest(t, root, "b-good", "normal", 10000)
	manager, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	_, matched, err := manager.Resolve(context.Background(), ParseParams{URL: "example+https://host/file.bin"})
	if err != nil || !matched {
		t.Fatalf("broken sibling affected resolution: matched=%v err=%v", matched, err)
	}
}

func TestPluginTimeoutKillsCall(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "slow", "hang", 2000)
	manager, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, _, err = manager.Resolve(context.Background(), ParseParams{URL: "anything"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout, got %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("timeout did not stop process promptly")
	}
}

func TestInvalidJSONHasClearError(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "invalid", "invalid", 10000)
	manager, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = manager.Resolve(context.Background(), ParseParams{URL: "anything"})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON response") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
}

func writeTestManifest(t *testing.T, root, id, mode string, timeoutMS int) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		ID: id, Name: id, Version: "1.0.0", ProtocolVersion: ProtocolVersion,
		Executable: os.Args[0], Args: []string{"-test.run=TestPluginHelperProcess", "--"},
		Env: map[string]string{"GD3_PLUGIN_HELPER": mode}, TimeoutMS: timeoutMS,
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFilename), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPluginHelperProcess(t *testing.T) {
	mode := os.Getenv("GD3_PLUGIN_HELPER")
	if mode == "" {
		return
	}
	if mode == "hang" {
		time.Sleep(10 * time.Second)
		return
	}
	if mode == "invalid" {
		fmt.Println("this is not json")
		return
	}
	var request rpcRequest
	if err := json.NewDecoder(bufio.NewReader(os.Stdin)).Decode(&request); err != nil {
		os.Exit(2)
	}
	var result any
	switch request.Method {
	case "manifest":
		result = Manifest{ID: "example", Name: "example", Version: "1.0.0", ProtocolVersion: ProtocolVersion, Executable: os.Args[0]}
	case "matches":
		var params MatchParams
		data, _ := json.Marshal(request.Params)
		_ = json.Unmarshal(data, &params)
		result = MatchResult{Matched: strings.HasPrefix(params.URL, "example+")}
	case "parse":
		var params ParseParams
		data, _ := json.Marshal(request.Params)
		_ = json.Unmarshal(data, &params)
		result = ParseResult{Kind: "http", URL: strings.TrimPrefix(params.URL, "example+"), Title: "plugin-file.bin"}
	default:
		os.Exit(3)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}
