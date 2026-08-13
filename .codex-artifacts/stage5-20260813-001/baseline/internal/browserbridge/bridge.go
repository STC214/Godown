package browserbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"ghost-downloader-go-win32/internal/config"
	"ghost-downloader-go-win32/internal/core"

	"golang.org/x/net/websocket"
)

const ProtocolVersion = 1

type Settings struct {
	Enabled bool
	Token   string
	Port    int
}

type Bridge struct {
	scheduler    *core.Scheduler
	createTask   CreateTaskFunc
	pairApproval PairApprovalFunc

	mu       sync.Mutex
	settings Settings
	server   *http.Server
	listener net.Listener
	sessions map[*session]struct{}
}

type session struct {
	bridge          *Bridge
	conn            *websocket.Conn
	mu              sync.Mutex
	writeMu         sync.Mutex
	authenticated   bool
	subscribedTasks bool
	lastSnapshot    []byte
	done            chan struct{}
}

type CreateTaskFunc func(context.Context, CreateTaskRequest) (core.Task, error)

type PairApprovalFunc func(context.Context, PairRequest) (bool, error)

type CreateTaskRequest struct {
	URL           string
	Title         string
	Path          string
	Headers       map[string]string
	Resources     []MergeResourceRequest
	Size          int64
	SupportsRange bool
	PreBlockNum   int
	Source        string
}

type MergeResourceRequest struct {
	URL           string
	Filename      string
	Mime          string
	Size          int64
	Headers       map[string]string
	SupportsRange bool
}

type PairRequest struct {
	RequestID        string
	ProtocolVersion  int
	ExtensionVersion string
	ClientKind       string
	RemoteAddr       string
}

func New(scheduler *core.Scheduler) *Bridge {
	return &Bridge{
		scheduler: scheduler,
		sessions:  make(map[*session]struct{}),
	}
}

func (b *Bridge) SetCreateTaskFunc(createTask CreateTaskFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.createTask = createTask
}

func (b *Bridge) SetPairApprovalFunc(pairApproval PairApprovalFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pairApproval = pairApproval
}

func SettingsFromConfig(settings config.Settings) Settings {
	return Settings{
		Enabled: settings.BrowserExtensionEnabled,
		Token:   settings.BrowserPairToken,
		Port:    settings.BrowserBridgePort,
	}
}

func (b *Bridge) Apply(settings Settings) error {
	settings = normalizedSettings(settings)

	b.mu.Lock()
	current := b.settings
	running := b.server != nil
	if running && current == settings {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	if err := b.Stop(context.Background()); err != nil {
		return err
	}
	if !settings.Enabled {
		b.mu.Lock()
		b.settings = settings
		b.mu.Unlock()
		return nil
	}
	return b.Start(settings)
}

func (b *Bridge) Start(settings Settings) error {
	settings = normalizedSettings(settings)
	if !settings.Enabled {
		return nil
	}
	if settings.Token == "" {
		return errors.New("browser bridge token is empty")
	}

	mux := http.NewServeMux()
	mux.Handle("/", websocket.Handler(b.handleWebSocket))

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", settings.Port))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: mux}

	b.mu.Lock()
	if b.server != nil {
		b.mu.Unlock()
		_ = listener.Close()
		return errors.New("browser bridge already running")
	}
	b.settings = settings
	b.server = server
	b.listener = listener
	b.mu.Unlock()

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("browser bridge server failed", "error", err)
		}
	}()
	slog.Info("browser bridge started", "addr", listener.Addr().String())
	return nil
}

func (b *Bridge) Stop(ctx context.Context) error {
	b.mu.Lock()
	server := b.server
	sessions := make([]*session, 0, len(b.sessions))
	for session := range b.sessions {
		sessions = append(sessions, session)
	}
	b.server = nil
	b.listener = nil
	b.sessions = make(map[*session]struct{})
	b.mu.Unlock()

	for _, session := range sessions {
		_ = session.conn.Close()
	}
	if server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return server.Shutdown(ctx)
}

func (b *Bridge) Addr() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener == nil {
		return ""
	}
	return b.listener.Addr().String()
}

func (b *Bridge) handleWebSocket(conn *websocket.Conn) {
	s := &session{
		bridge: b,
		conn:   conn,
		done:   make(chan struct{}),
	}

	b.mu.Lock()
	b.sessions[s] = struct{}{}
	b.mu.Unlock()
	defer func() {
		close(s.done)
		b.mu.Lock()
		delete(b.sessions, s)
		b.mu.Unlock()
		_ = conn.Close()
	}()

	go s.pushSnapshots()

	for {
		var raw string
		if err := websocket.Message.Receive(conn, &raw); err != nil {
			return
		}
		s.handleMessage([]byte(raw))
	}
}

func (s *session) handleMessage(raw []byte) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		s.sendError("", "bad_request", "invalid JSON message")
		return
	}
	messageType := stringValue(message, "type")
	requestID := stringValue(message, "requestId")

	switch messageType {
	case "pair_request":
		s.handlePairRequest(requestID, message)
	case "hello":
		s.handleHello(requestID, message)
	case "subscribe_tasks":
		if !s.requireAuthenticated(requestID) {
			return
		}
		s.mu.Lock()
		s.subscribedTasks = true
		s.lastSnapshot = nil
		s.mu.Unlock()
		s.sendTaskSnapshot()
	case "task_action":
		if !s.requireAuthenticated(requestID) {
			return
		}
		s.handleTaskAction(requestID, message)
	case "create_task":
		if !s.requireAuthenticated(requestID) {
			return
		}
		s.handleCreateTask(requestID, message)
	default:
		if !s.requireAuthenticated(requestID) {
			return
		}
		s.sendError(requestID, "bad_request", "unsupported message type")
	}
}

func (s *session) handlePairRequest(requestID string, message map[string]any) {
	if requestID == "" {
		s.sendPairResult("", false, "", "missing requestId")
		return
	}
	protocolVersion := intValue(message, "protocolVersion")
	if protocolVersion != ProtocolVersion {
		s.sendPairResult(requestID, false, "", "protocol version mismatch")
		return
	}

	s.bridge.mu.Lock()
	pairApproval := s.bridge.pairApproval
	token := s.bridge.settings.Token
	s.bridge.mu.Unlock()
	if pairApproval == nil {
		s.sendPairResult(requestID, false, "", "pair approval is unavailable")
		return
	}

	approved, err := pairApproval(context.Background(), PairRequest{
		RequestID:        requestID,
		ProtocolVersion:  protocolVersion,
		ExtensionVersion: stringValue(message, "extensionVersion"),
		ClientKind:       stringValue(message, "clientKind"),
		RemoteAddr:       remoteAddr(s.conn),
	})
	if err != nil {
		s.sendPairResult(requestID, false, "", err.Error())
		return
	}
	if !approved {
		s.sendPairResult(requestID, false, "", "pair request rejected")
		return
	}
	s.sendPairResult(requestID, true, token, "pairing approved")
}

func (s *session) sendPairResult(requestID string, ok bool, token string, message string) {
	payload := map[string]any{
		"type":      "pair_result",
		"requestId": requestID,
		"ok":        ok,
	}
	if token != "" {
		payload["token"] = token
	}
	if message != "" {
		payload["message"] = message
	}
	s.send(payload)
}

func (s *session) handleCreateTask(requestID string, message map[string]any) {
	if requestID == "" {
		s.sendCreateTaskResult("", false, "", "missing requestId")
		return
	}
	payload, _ := message["payload"].(map[string]any)
	if payload == nil {
		s.sendCreateTaskResult(requestID, false, "", "invalid task payload")
		return
	}
	source := stringValue(message, "source")

	request := CreateTaskRequest{
		URL:           stringValue(payload, "url"),
		Title:         stringValue(message, "title"),
		Path:          stringValue(payload, "path"),
		Headers:       stringMapValue(payload, "headers"),
		Resources:     mergeResourcesValue(payload, "resources"),
		Size:          int64Value(payload, "size"),
		SupportsRange: boolValue(payload, "supportsRange"),
		PreBlockNum:   intValue(payload, "preBlockNum"),
		Source:        source,
	}
	if request.URL == "" && source != "resource_merge" {
		s.sendCreateTaskResult(requestID, false, "", "missing download URL")
		return
	}

	s.bridge.mu.Lock()
	createTask := s.bridge.createTask
	s.bridge.mu.Unlock()
	if createTask == nil || s.bridge.scheduler == nil {
		s.sendCreateTaskResult(requestID, false, "", "task creation is unavailable")
		return
	}

	task, err := createTask(context.Background(), request)
	if err != nil {
		s.sendCreateTaskResult(requestID, false, "", err.Error())
		return
	}
	s.bridge.scheduler.Add(task)
	s.sendCreateTaskResult(requestID, true, task.ID, "")
	s.sendTaskSnapshot()
}

func (s *session) sendCreateTaskResult(requestID string, ok bool, taskID string, message string) {
	payload := map[string]any{
		"type":      "create_task_result",
		"requestId": requestID,
		"ok":        ok,
	}
	if taskID != "" {
		payload["taskId"] = taskID
	}
	if message != "" {
		payload["message"] = message
	}
	s.send(payload)
}

func (s *session) handleTaskAction(requestID string, message map[string]any) {
	if requestID == "" {
		s.sendTaskActionResult("", false, "missing requestId")
		return
	}
	taskID := stringValue(message, "taskId")
	action := stringValue(message, "action")
	if taskID == "" {
		s.sendTaskActionResult(requestID, false, "missing taskId")
		return
	}
	if s.bridge.scheduler == nil {
		s.sendTaskActionResult(requestID, false, "scheduler is unavailable")
		return
	}

	switch action {
	case "toggle_pause":
		err := s.bridge.scheduler.TogglePause(taskID)
		s.sendTaskActionResult(requestID, err == nil, errorMessage(err))
	case "redownload":
		err := s.bridge.scheduler.Redownload(taskID)
		s.sendTaskActionResult(requestID, err == nil, errorMessage(err))
	case "cancel":
		err := s.bridge.scheduler.Remove(taskID)
		s.sendTaskActionResult(requestID, err == nil, errorMessage(err))
	case "open_folder":
		task, ok := findTask(s.bridge.scheduler, taskID)
		if !ok {
			s.sendTaskActionResult(requestID, false, "task not found")
			return
		}
		err := exec.Command("explorer.exe", task.Path).Start()
		s.sendTaskActionResult(requestID, err == nil, errorMessage(err))
	case "open_file":
		task, ok := findTask(s.bridge.scheduler, taskID)
		if !ok {
			s.sendTaskActionResult(requestID, false, "task not found")
			return
		}
		outputFile := filepath.Join(task.Path, task.Title)
		if _, err := os.Stat(outputFile); err != nil {
			s.sendTaskActionResult(requestID, false, "file is not available")
			return
		}
		err := OpenOutputFile(outputFile)
		s.sendTaskActionResult(requestID, err == nil, errorMessage(err))
	default:
		s.sendTaskActionResult(requestID, false, "unsupported task action")
	}
}

func findTask(scheduler *core.Scheduler, taskID string) (core.TaskSnapshot, bool) {
	for _, task := range scheduler.Snapshot() {
		if task.ID == taskID {
			return task, true
		}
	}
	return core.TaskSnapshot{}, false
}

func (s *session) sendTaskActionResult(requestID string, ok bool, message string) {
	payload := map[string]any{
		"type":      "task_action_result",
		"requestId": requestID,
		"ok":        ok,
	}
	if message != "" {
		payload["message"] = message
	}
	s.send(payload)
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *session) handleHello(requestID string, message map[string]any) {
	if intValue(message, "protocolVersion") != ProtocolVersion {
		s.sendError(requestID, "protocol_mismatch", "protocol version mismatch")
		_ = s.conn.Close()
		return
	}

	s.bridge.mu.Lock()
	token := s.bridge.settings.Token
	s.bridge.mu.Unlock()
	if stringValue(message, "token") != token {
		s.sendError(requestID, "unauthorized", "invalid pair token")
		_ = s.conn.Close()
		return
	}

	s.mu.Lock()
	s.authenticated = true
	s.mu.Unlock()
	s.send(map[string]any{
		"type":            "hello_ack",
		"protocolVersion": ProtocolVersion,
		"appVersion":      "go-win32",
		"capabilities": map[string]any{
			"taskSnapshots": true,
			"createTask":    true,
			"taskActions":   []string{"toggle_pause", "redownload", "open_file", "open_folder", "cancel"},
		},
	})
}

func (s *session) requireAuthenticated(requestID string) bool {
	s.mu.Lock()
	authenticated := s.authenticated
	s.mu.Unlock()
	if authenticated {
		return true
	}
	s.sendError(requestID, "unauthorized", "authenticate with hello first")
	_ = s.conn.Close()
	return false
}

func (s *session) pushSnapshots() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if s.shouldPushSnapshots() {
				s.sendTaskSnapshot()
			}
		}
	}
}

func (s *session) shouldPushSnapshots() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authenticated && s.subscribedTasks
}

func (s *session) sendTaskSnapshot() {
	payload := taskSnapshotPayload(s.bridge.scheduler)
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("browser bridge task snapshot marshal failed", "error", err)
		return
	}
	s.mu.Lock()
	if string(body) == string(s.lastSnapshot) {
		s.mu.Unlock()
		return
	}
	s.lastSnapshot = append(s.lastSnapshot[:0], body...)
	s.mu.Unlock()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = websocket.Message.Send(s.conn, string(body))
}

func taskSnapshotPayload(scheduler *core.Scheduler) map[string]any {
	var snapshots []core.TaskSnapshot
	if scheduler != nil {
		snapshots = scheduler.Snapshot()
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})

	tasks := make([]map[string]any, 0, len(snapshots))
	for _, task := range snapshots {
		outputFile := filepath.Join(task.Path, task.Title)
		_, fileErr := os.Stat(outputFile)
		_, folderErr := os.Stat(task.Path)
		tasks = append(tasks, map[string]any{
			"taskId":        task.ID,
			"title":         task.Title,
			"status":        string(task.Status),
			"progress":      task.Progress,
			"receivedBytes": task.Received,
			"fileSize":      task.FileSize,
			"speed":         task.Speed,
			"createdAt":     task.CreatedAt.UnixMilli(),
			"canPause":      task.Status == core.StatusRunning || task.Status == core.StatusWaiting,
			"canOpenFile":   task.Status == core.StatusCompleted && fileErr == nil,
			"canOpenFolder": task.Path != "" && folderErr == nil,
			"fileExt":       extensionWithoutDot(task.Title),
			"packName":      task.PackID,
			"outputFile":    outputFile,
		})
	}

	return map[string]any{
		"type":  "task_snapshot",
		"tasks": tasks,
	}
}

func extensionWithoutDot(name string) string {
	ext := filepath.Ext(name)
	if len(ext) <= 1 {
		return ""
	}
	return ext[1:]
}

func (s *session) sendError(requestID, code, message string) {
	payload := map[string]any{
		"type":    "error",
		"code":    code,
		"message": message,
	}
	if requestID != "" {
		payload["requestId"] = requestID
	}
	s.send(payload)
}

func (s *session) send(payload map[string]any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := websocket.JSON.Send(s.conn, payload); err != nil {
		slog.Warn("browser bridge send failed", "error", err)
	}
}

func normalizedSettings(settings Settings) Settings {
	if settings.Port < 0 {
		settings.Port = config.DefaultBrowserBridgePort
	}
	return settings
}

func stringValue(message map[string]any, key string) string {
	value, _ := message[key].(string)
	return value
}

func intValue(message map[string]any, key string) int {
	switch value := message[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func int64Value(message map[string]any, key string) int64 {
	switch value := message[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	default:
		return 0
	}
}

func boolValue(message map[string]any, key string) bool {
	value, _ := message[key].(bool)
	return value
}

func stringMapValue(message map[string]any, key string) map[string]string {
	raw, _ := message[key].(map[string]any)
	if len(raw) == 0 {
		return nil
	}
	result := make(map[string]string, len(raw))
	for name, value := range raw {
		if text, ok := value.(string); ok {
			result[name] = text
		}
	}
	return result
}

func mergeResourcesValue(message map[string]any, key string) []MergeResourceRequest {
	raw, _ := message[key].([]any)
	if len(raw) == 0 {
		return nil
	}
	result := make([]MergeResourceRequest, 0, len(raw))
	for _, item := range raw {
		resource, _ := item.(map[string]any)
		if resource == nil {
			continue
		}
		result = append(result, MergeResourceRequest{
			URL:           stringValue(resource, "url"),
			Filename:      stringValue(resource, "filename"),
			Mime:          stringValue(resource, "mime"),
			Size:          int64Value(resource, "size"),
			Headers:       stringMapValue(resource, "headers"),
			SupportsRange: boolValue(resource, "supportsRange"),
		})
	}
	return result
}

func remoteAddr(conn *websocket.Conn) string {
	if conn == nil || conn.Request() == nil {
		return ""
	}
	if conn.Request().RemoteAddr != "" {
		return conn.Request().RemoteAddr
	}
	return conn.Request().Header.Get("X-Forwarded-For")
}

func OpenOutputFile(path string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path).Start()
}
