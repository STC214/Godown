package browserbridge

import (
	"context"
	"encoding/json"
	"testing"

	"ghost-downloader-go-win32/internal/core"

	"golang.org/x/net/websocket"
)

func TestBridgeHelloAndSubscribe(t *testing.T) {
	scheduler := core.NewScheduler(core.NewRegistry(), nil, 1)
	bridge := New(scheduler)
	if err := bridge.Start(Settings{Enabled: true, Token: "secret", Port: 0}); err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop(nil)

	conn, err := websocket.Dial("ws://"+bridge.Addr()+"/", "", "http://127.0.0.1/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendJSON(t, conn, map[string]any{
		"type":            "hello",
		"requestId":       "hello-1",
		"protocolVersion": ProtocolVersion,
		"token":           "secret",
	})
	hello := receiveJSON(t, conn)
	if hello["type"] != "hello_ack" {
		t.Fatalf("hello response=%#v", hello)
	}

	sendJSON(t, conn, map[string]any{
		"type":      "subscribe_tasks",
		"requestId": "sub-1",
	})
	snapshot := receiveJSON(t, conn)
	if snapshot["type"] != "task_snapshot" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	if _, ok := snapshot["tasks"].([]any); !ok {
		t.Fatalf("tasks missing: %#v", snapshot)
	}
}

func TestBridgeRejectsBadToken(t *testing.T) {
	bridge := New(nil)
	if err := bridge.Start(Settings{Enabled: true, Token: "secret", Port: 0}); err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop(nil)

	conn, err := websocket.Dial("ws://"+bridge.Addr()+"/", "", "http://127.0.0.1/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendJSON(t, conn, map[string]any{
		"type":            "hello",
		"requestId":       "hello-1",
		"protocolVersion": ProtocolVersion,
		"token":           "wrong",
	})
	response := receiveJSON(t, conn)
	if response["type"] != "error" || response["code"] != "unauthorized" {
		t.Fatalf("response=%#v", response)
	}
}

func TestBridgePairRequestApproved(t *testing.T) {
	bridge := New(nil)
	bridge.SetPairApprovalFunc(func(ctx context.Context, request PairRequest) (bool, error) {
		if request.ProtocolVersion != ProtocolVersion || request.ClientKind != "browser_extension" {
			t.Fatalf("request=%#v", request)
		}
		return true, nil
	})
	if err := bridge.Start(Settings{Enabled: true, Token: "secret", Port: 0}); err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop(nil)

	conn, err := websocket.Dial("ws://"+bridge.Addr()+"/", "", "http://127.0.0.1/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendJSON(t, conn, map[string]any{
		"type":             "pair_request",
		"requestId":        "pair-1",
		"protocolVersion":  ProtocolVersion,
		"clientKind":       "browser_extension",
		"extensionVersion": "1.0.0",
	})
	response := receiveJSON(t, conn)
	if response["type"] != "pair_result" || response["ok"] != true || response["token"] != "secret" {
		t.Fatalf("response=%#v", response)
	}
}

func TestBridgePairRequestRejected(t *testing.T) {
	bridge := New(nil)
	bridge.SetPairApprovalFunc(func(ctx context.Context, request PairRequest) (bool, error) {
		return false, nil
	})
	if err := bridge.Start(Settings{Enabled: true, Token: "secret", Port: 0}); err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop(nil)

	conn, err := websocket.Dial("ws://"+bridge.Addr()+"/", "", "http://127.0.0.1/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendJSON(t, conn, map[string]any{
		"type":            "pair_request",
		"requestId":       "pair-1",
		"protocolVersion": ProtocolVersion,
	})
	response := receiveJSON(t, conn)
	if response["type"] != "pair_result" || response["ok"] != false {
		t.Fatalf("response=%#v", response)
	}
}

func TestBridgeTaskActionTogglePause(t *testing.T) {
	registry := core.NewRegistry()
	registry.Register("fake", blockingWorker{})
	scheduler := core.NewScheduler(registry, nil, 1)
	task := core.NewTask("fake", "file.bin", "https://example.test/file.bin", t.TempDir(), 1, core.NewStage("fake", "https://example.test/file.bin", 1, 1, 3, false, nil, ""))
	scheduler.Add(task)

	bridge := New(scheduler)
	if err := bridge.Start(Settings{Enabled: true, Token: "secret", Port: 0}); err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop(nil)

	conn, err := websocket.Dial("ws://"+bridge.Addr()+"/", "", "http://127.0.0.1/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendJSON(t, conn, map[string]any{
		"type":            "hello",
		"protocolVersion": ProtocolVersion,
		"token":           "secret",
	})
	_ = receiveJSON(t, conn)
	sendJSON(t, conn, map[string]any{
		"type":      "task_action",
		"requestId": "action-1",
		"taskId":    task.ID,
		"action":    "toggle_pause",
	})
	response := receiveJSON(t, conn)
	if response["type"] != "task_action_result" || response["ok"] != true {
		t.Fatalf("response=%#v", response)
	}
}

func TestBridgeCreateTask(t *testing.T) {
	scheduler := core.NewScheduler(core.NewRegistry(), nil, 1)
	bridge := New(scheduler)
	bridge.SetCreateTaskFunc(func(ctx context.Context, request CreateTaskRequest) (core.Task, error) {
		if request.URL != "https://example.test/file.bin" {
			t.Fatalf("request=%#v", request)
		}
		return core.NewTask("fake", request.Title, request.URL, t.TempDir(), 1, core.NewStage("fake", request.URL, 1, 1, 3, false, nil, "")), nil
	})
	if err := bridge.Start(Settings{Enabled: true, Token: "secret", Port: 0}); err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop(nil)

	conn, err := websocket.Dial("ws://"+bridge.Addr()+"/", "", "http://127.0.0.1/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendJSON(t, conn, map[string]any{
		"type":            "hello",
		"protocolVersion": ProtocolVersion,
		"token":           "secret",
	})
	_ = receiveJSON(t, conn)
	sendJSON(t, conn, map[string]any{
		"type":      "create_task",
		"requestId": "create-1",
		"title":     "from-browser.bin",
		"payload": map[string]any{
			"url": "https://example.test/file.bin",
			"headers": map[string]any{
				"Referer": "https://example.test/",
			},
		},
	})
	response := receiveJSON(t, conn)
	if response["type"] != "create_task_result" || response["ok"] != true || response["taskId"] == "" {
		t.Fatalf("response=%#v", response)
	}
	if len(scheduler.Snapshot()) != 1 {
		t.Fatalf("task was not added")
	}
}

func TestBridgeCreateTaskPassesResourceMerge(t *testing.T) {
	bridge := New(core.NewScheduler(core.NewRegistry(), nil, 1))
	bridge.SetCreateTaskFunc(func(ctx context.Context, request CreateTaskRequest) (core.Task, error) {
		if request.Source != "resource_merge" || len(request.Resources) != 2 {
			t.Fatalf("request=%#v", request)
		}
		return core.NewTask("ffmpeg", "merged.mp4", request.Resources[0].URL, t.TempDir(), 1, core.NewStage("ffmpeg_merge", request.Resources[0].URL, 1, 1, 3, false, nil, "")), nil
	})
	if err := bridge.Start(Settings{Enabled: true, Token: "secret", Port: 0}); err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop(nil)

	conn, err := websocket.Dial("ws://"+bridge.Addr()+"/", "", "http://127.0.0.1/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendJSON(t, conn, map[string]any{
		"type":            "hello",
		"protocolVersion": ProtocolVersion,
		"token":           "secret",
	})
	_ = receiveJSON(t, conn)
	sendJSON(t, conn, map[string]any{
		"type":      "create_task",
		"requestId": "create-merge",
		"source":    "resource_merge",
		"payload": map[string]any{
			"resources": []any{
				map[string]any{"url": "https://example.test/video.mp4", "filename": "video.mp4"},
				map[string]any{"url": "https://example.test/audio.m4a", "filename": "audio.m4a"},
			},
		},
	})
	response := receiveJSON(t, conn)
	if response["type"] != "create_task_result" || response["ok"] != true || response["taskId"] == "" {
		t.Fatalf("response=%#v", response)
	}
}

func TestTaskSnapshotPayloadUsesPackIDAndFileAvailability(t *testing.T) {
	registry := core.NewRegistry()
	scheduler := core.NewScheduler(registry, nil, 1)
	dir := t.TempDir()
	stage := core.NewStage("m3u8", "https://example.test/master.m3u8", 1, 1, 3, false, nil, "")
	task := core.NewTask("m3u8", "movie.mp4", "https://example.test/master.m3u8", dir, 1, stage)
	task.Status = core.StatusCompleted
	task.Progress = 100
	scheduler.Add(task)

	payload := taskSnapshotPayload(scheduler)
	tasks, ok := payload["tasks"].([]map[string]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("payload=%#v", payload)
	}
	if tasks[0]["packName"] != "m3u8" {
		t.Fatalf("packName=%#v", tasks[0])
	}
	if tasks[0]["canOpenFile"] != false {
		t.Fatalf("file should not be openable before it exists: %#v", tasks[0])
	}
}

type blockingWorker struct{}

func (blockingWorker) Run(ctx context.Context, task core.Task, report func(core.ProgressUpdate)) error {
	<-ctx.Done()
	return ctx.Err()
}

func sendJSON(t *testing.T, conn *websocket.Conn, payload map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(conn, string(body)); err != nil {
		t.Fatal(err)
	}
}

func receiveJSON(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	var raw string
	if err := websocket.Message.Receive(conn, &raw); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
