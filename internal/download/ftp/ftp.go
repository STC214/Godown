package ftpdownload

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"
	"golang.org/x/net/proxy"

	"ghost-downloader-go-win32/internal/core"
	"ghost-downloader-go-win32/internal/ratelimit"
)

const (
	stateHost           = "host"
	stateRemotePath     = "remotePath"
	stateUsername       = "username"
	statePassword       = "password"
	stateTLSMode        = "tlsMode"
	stateTimeout        = "timeoutSeconds"
	stateDirectory      = "directory"
	stateEntries        = "entries"
	maxDirectoryEntries = 100000
)

type Worker struct {
	Limiter   *ratelimit.Limiter
	tlsConfig *tls.Config
}

type sourceInfo struct {
	host, remotePath, username, password, tlsMode string
	timeout                                       time.Duration
	tlsConfig                                     *tls.Config
	directory                                     bool
}

type directoryEntry struct {
	RemotePath   string `json:"remotePath"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
	Directory    bool   `json:"directory,omitempty"`
}

func IsSource(source string) bool {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "ftp") || strings.EqualFold(u.Scheme, "ftps") || strings.EqualFold(u.Scheme, "ftpes")
}

func Parse(ctx context.Context, source, downloadDir, proxyURL string, maxRetries int) (core.Task, error) {
	return parse(ctx, source, downloadDir, proxyURL, maxRetries, nil)
}

func parse(ctx context.Context, source, downloadDir, proxyURL string, maxRetries int, tlsConfig *tls.Config) (core.Task, error) {
	info, cleanURL, err := parseSource(source)
	if err != nil {
		return core.Task{}, err
	}
	if err := validateProxy(proxyURL); err != nil {
		return core.Task{}, err
	}
	info.tlsConfig = tlsConfig
	conn, err := dial(ctx, info, proxyURL)
	if err != nil {
		return core.Task{}, err
	}
	defer conn.Quit()
	if info.directory {
		return newDirectoryTask(conn, info, cleanURL, downloadDir, proxyURL, maxRetries)
	}
	size, err := conn.FileSize(info.remotePath)
	if err != nil {
		if task, directoryErr := newDirectoryTask(conn, info, cleanURL, downloadDir, proxyURL, maxRetries); directoryErr == nil {
			return task, nil
		}
		if !sizeCommandUnsupported(err) {
			return core.Task{}, fmt.Errorf("读取 FTP 文件大小：%w", err)
		}
		size = 0
	}
	title := safeFileName(filepath.Base(filepath.FromSlash(info.remotePath)))
	if title == "." || title == string(filepath.Separator) || title == "" {
		return core.Task{}, errors.New("FTP 地址缺少文件名")
	}
	stage := core.NewStage("ftp", cleanURL, size, 1, maxRetries, true, nil, proxyURL)
	stage.State = map[string]string{
		stateHost: info.host, stateRemotePath: info.remotePath,
		stateUsername: info.username, statePassword: info.password,
		stateTLSMode: info.tlsMode, stateTimeout: strconv.Itoa(int(info.timeout / time.Second)),
	}
	return core.NewTask("ftp", title, cleanURL, downloadDir, size, stage), nil
}

func newDirectoryTask(conn *ftp.ServerConn, info sourceInfo, cleanURL, downloadDir, proxyURL string, maxRetries int) (core.Task, error) {
	entries, total, err := listDirectory(conn, info.remotePath)
	if err != nil {
		return core.Task{}, fmt.Errorf("读取 FTP 目录：%w", err)
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return core.Task{}, fmt.Errorf("保存 FTP 目录清单：%w", err)
	}
	title := safeFileName(path.Base(info.remotePath))
	if info.remotePath == "/" || title == "" {
		title = "ftp-root"
	}
	stage := core.NewStage("ftp", cleanURL, total, 1, maxRetries, true, nil, proxyURL)
	stage.State = map[string]string{
		stateHost: info.host, stateRemotePath: info.remotePath,
		stateUsername: info.username, statePassword: info.password,
		stateTLSMode: info.tlsMode, stateTimeout: strconv.Itoa(int(info.timeout / time.Second)),
		stateDirectory: "true", stateEntries: string(encoded),
	}
	return core.NewTask("ftp", title, cleanURL, downloadDir, total, stage), nil
}

func listDirectory(conn *ftp.ServerConn, root string) ([]directoryEntry, int64, error) {
	walker := conn.Walk(root)
	root = path.Clean(root)
	prefix := strings.TrimSuffix(root, "/") + "/"
	if root == "/" {
		prefix = "/"
	}
	entries := make([]directoryEntry, 0)
	seen := make(map[string]bool)
	var total int64
	totalKnown := true
	for walker.Next() {
		entry := walker.Stat()
		if entry == nil || entry.Type == ftp.EntryTypeLink {
			continue
		}
		remotePath := path.Clean(walker.Path())
		if !strings.HasPrefix(remotePath, prefix) {
			return nil, 0, fmt.Errorf("目录项超出根目录：%q", walker.Path())
		}
		relative := strings.TrimPrefix(remotePath, prefix)
		if relative == "" {
			return nil, 0, fmt.Errorf("FTP 目录项缺少相对路径：%q", walker.Path())
		}
		local, err := safeRelativePath(relative, entry.Type == ftp.EntryTypeFolder, seen)
		if err != nil {
			return nil, 0, err
		}
		item := directoryEntry{RemotePath: remotePath, RelativePath: local, Directory: entry.Type == ftp.EntryTypeFolder}
		if !item.Directory {
			if err := validateDirectoryEntrySize(entry.Size); err != nil {
				return nil, 0, fmt.Errorf("FTP 文件 %q：%w", remotePath, err)
			}
			item.Size = int64(entry.Size)
			if entry.Size == 0 {
				if probed, sizeErr := conn.FileSize(remotePath); sizeErr == nil {
					item.Size = probed
				} else {
					item.Size = -1
					totalKnown = false
				}
			}
			if item.Size >= 0 && uint64(item.Size) > uint64(1<<63-1)-uint64(total) {
				return nil, 0, errors.New("FTP 目录总大小超出支持范围")
			}
			if item.Size >= 0 {
				total += item.Size
			}
		}
		entries = append(entries, item)
		if len(entries) > maxDirectoryEntries {
			return nil, 0, fmt.Errorf("FTP 目录项超过上限 %d", maxDirectoryEntries)
		}
	}
	if err := walker.Err(); err != nil {
		return nil, 0, err
	}
	if !totalKnown {
		total = 0
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].RelativePath < entries[j].RelativePath })
	return entries, total, nil
}

func validateDirectoryEntrySize(size uint64) error {
	if size > uint64(1<<63-1) {
		return errors.New("文件大小超出支持范围")
	}
	return nil
}

func safeRelativePath(remote string, directory bool, seen map[string]bool) (string, error) {
	parts := strings.Split(remote, "/")
	local := make([]string, len(parts))
	for index, part := range parts {
		local[index] = safeFileName(part)
		if index == 0 && strings.EqualFold(local[index], core.FTPDirectoryOwnerMarker) {
			return "", fmt.Errorf("FTP 目录项使用了保留名称：%q", remote)
		}
		key := strings.ToLower(strings.Join(local[:index+1], "/"))
		isDirectory := index < len(parts)-1 || directory
		if previousDirectory, exists := seen[key]; exists && previousDirectory != isDirectory {
			return "", fmt.Errorf("FTP 目录项本地路径冲突：%q", remote)
		} else if exists && index == len(parts)-1 {
			return "", fmt.Errorf("FTP 目录项清理后重名：%q", remote)
		}
		seen[key] = isDirectory
	}
	return strings.Join(local, "/"), nil
}

func sizeCommandUnsupported(err error) bool {
	var protocolError *textproto.Error
	if !errors.As(err, &protocolError) {
		return false
	}
	return protocolError.Code == 500 || protocolError.Code == 502 || protocolError.Code == 504
}

func safeFileName(name string) string {
	name = strings.Map(func(value rune) rune {
		if value < 32 || strings.ContainsRune(`<>:"/\|?*`, value) {
			return '_'
		}
		return value
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." {
		return "download"
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
		(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
		name = "_" + name
	}
	return name
}

func (w Worker) Run(ctx context.Context, task core.Task, report func(core.ProgressUpdate)) error {
	info, err := sourceFromTask(task)
	if err != nil {
		return err
	}
	info.tlsConfig = w.tlsConfig
	if report == nil {
		report = func(core.ProgressUpdate) {}
	}
	if err := os.MkdirAll(task.Path, 0o755); err != nil {
		return err
	}
	if info.directory {
		return w.runDirectory(ctx, task, info, report)
	}
	output := task.OutputFile()
	partial := output + ".part"
	var offset int64
	if stat, statErr := os.Stat(partial); statErr == nil {
		offset = stat.Size()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if task.FileSize <= 0 && offset > 0 {
		if err := os.Truncate(partial, 0); err != nil {
			return err
		}
		offset = 0
	}
	if task.FileSize > 0 && offset > task.FileSize {
		if err := os.Truncate(partial, 0); err != nil {
			return err
		}
		offset = 0
	}
	received := offset
	report(update(received, task.FileSize, 0, "正在连接 FTP 服务器…"))
	failures := 0
	for {
		start := received
		err = w.transfer(ctx, info, task.Stage.ProxyURL, partial, &received, task.FileSize, report)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if received > start {
			failures = 0
		}
		failures++
		if failures > task.Stage.MaxRetries {
			return err
		}
		delay := time.Duration(failures) * time.Second
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	if task.FileSize > 0 && received != task.FileSize {
		return fmt.Errorf("FTP 文件大小不匹配：收到 %d，预期 %d", received, task.FileSize)
	}
	if err := os.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(partial, output); err != nil {
		return fmt.Errorf("完成 FTP 文件：%w", err)
	}
	completed := update(received, received, 0, "FTP 下载完成")
	completed.Progress = 100 // Empty files also finish at 100%.
	report(completed)
	return nil
}

func (w Worker) runDirectory(ctx context.Context, task core.Task, info sourceInfo, report func(core.ProgressUpdate)) error {
	var entries []directoryEntry
	if err := json.Unmarshal([]byte(task.Stage.State[stateEntries]), &entries); err != nil {
		return fmt.Errorf("读取 FTP 目录清单：%w", err)
	}
	root := task.OutputFile()
	if err := prepareDirectoryRoot(root, task.ID); err != nil {
		return err
	}
	outputRoot, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer outputRoot.Close()
	metadataRoot, err := os.OpenRoot(task.Path)
	if err != nil {
		return err
	}
	defer metadataRoot.Close()
	var received int64
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		normalized, err := safeRelativePath(entry.RelativePath, entry.Directory, seen)
		if err != nil || normalized != entry.RelativePath || entry.Size < -1 {
			return fmt.Errorf("FTP 目录清单项无效：%q", entry.RelativePath)
		}
		_, err = directoryOutputPath(root, entry.RelativePath)
		if err != nil {
			return err
		}
		localPath := filepath.FromSlash(entry.RelativePath)
		if entry.Directory {
			if err := outputRoot.MkdirAll(localPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if !remoteChild(info.remotePath, entry.RemotePath) {
			return fmt.Errorf("FTP 目录项超出远程根目录：%q", entry.RemotePath)
		}
		if err := outputRoot.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return err
		}
		fileInfo := info
		fileInfo.remotePath = entry.RemotePath
		completionPath, err := directoryOutputPath(filepath.Join(task.Path, ".gd3_ftp", task.ID), entry.RelativePath+".complete")
		if err != nil {
			return err
		}
		completionPath, err = filepath.Rel(task.Path, completionPath)
		if err != nil {
			return err
		}
		fileReceived, err := w.downloadDirectoryFile(ctx, outputRoot, metadataRoot, fileInfo, task.Stage.ProxyURL, localPath, completionPath, entry.Size, task.Stage.MaxRetries, func(current, speed int64, detail string) {
			report(update(received+current, task.FileSize, speed, detail+"："+entry.RelativePath))
		})
		if err != nil {
			return err
		}
		received += fileReceived
	}
	completed := update(received, received, 0, "FTP 目录下载完成")
	completed.Progress = 100
	report(completed)
	return nil
}

func prepareDirectoryRoot(root, taskID string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(root, 0o755); err != nil {
			return err
		}
		marker, err := os.OpenFile(filepath.Join(root, core.FTPDirectoryOwnerMarker), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := marker.WriteString(taskID + "\n"); err != nil {
			_ = marker.Close()
			return err
		}
		return marker.Close()
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("FTP directory %q is not a task-owned directory", root)
	}
	marker := filepath.Join(root, core.FTPDirectoryOwnerMarker)
	info, err = os.Lstat(marker)
	if err != nil {
		return fmt.Errorf("FTP directory ownership marker: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("FTP directory %q has an invalid ownership marker", root)
	}
	owner, err := os.ReadFile(marker)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(owner)) != taskID {
		return fmt.Errorf("FTP directory %q belongs to another task", root)
	}
	return nil
}

func (w Worker) downloadDirectoryFile(ctx context.Context, root, metadata *os.Root, info sourceInfo, proxyURL, output, completionPath string, size int64, maxRetries int, report func(int64, int64, string)) (int64, error) {
	if completedFileMatches(root, metadata, output, completionPath, size) {
		stat, err := root.Stat(output)
		if err != nil {
			return 0, err
		}
		return stat.Size(), nil
	} else if _, err := root.Stat(output); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	partial := output + ".part"
	var received int64
	if stat, err := root.Stat(partial); err == nil {
		received = stat.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if (size < 0 && received > 0) || (size >= 0 && received > size) {
		file, err := root.OpenFile(partial, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return 0, err
		}
		if err := file.Close(); err != nil {
			return 0, err
		}
		received = 0
	}
	report(received, 0, "正在连接 FTP 服务器")
	failures := 0
	for {
		start := received
		err := w.transfer(ctx, info, proxyURL, partial, &received, size, func(progress core.ProgressUpdate) {
			report(progress.Received, progress.Speed, "正在下载 FTP 文件")
		}, root)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return received, ctx.Err()
		}
		if received > start {
			failures = 0
		}
		failures++
		if failures > maxRetries {
			return received, err
		}
		select {
		case <-ctx.Done():
			return received, ctx.Err()
		case <-time.After(time.Duration(failures) * time.Second):
		}
	}
	if size >= 0 && received != size {
		return received, fmt.Errorf("FTP 文件大小不匹配：%q 收到 %d，预期 %d", info.remotePath, received, size)
	}
	if err := root.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
		return received, err
	}
	if err := root.Rename(partial, output); err != nil {
		return received, fmt.Errorf("完成 FTP 文件 %q：%w", info.remotePath, err)
	}
	if err := writeCompletionMarker(root, metadata, output, completionPath); err != nil {
		return received, fmt.Errorf("记录 FTP 文件完成状态 %q：%w", info.remotePath, err)
	}
	return received, nil
}

func completedFileMatches(root, metadata *os.Root, output, completionPath string, size int64) bool {
	stat, err := root.Stat(output)
	if err != nil || stat.IsDir() || (size >= 0 && stat.Size() != size) {
		return false
	}
	want, err := metadata.ReadFile(completionPath)
	if err != nil {
		return false
	}
	got, err := fileSHA256(root, output)
	return err == nil && strings.TrimSpace(string(want)) == got
}

func writeCompletionMarker(root, metadata *os.Root, output, completionPath string) error {
	digest, err := fileSHA256(root, output)
	if err != nil {
		return err
	}
	if err := metadata.MkdirAll(filepath.Dir(completionPath), 0o755); err != nil {
		return err
	}
	temporary := completionPath + ".tmp"
	if err := metadata.WriteFile(temporary, []byte(digest+"\n"), 0o600); err != nil {
		return err
	}
	defer metadata.Remove(temporary)
	if err := metadata.Remove(completionPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return metadata.Rename(temporary, completionPath)
}

func fileSHA256(root *os.Root, name string) (string, error) {
	file, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func directoryOutputPath(root, relative string) (string, error) {
	if relative == "" || path.IsAbs(relative) || strings.Contains(relative, `\`) {
		return "", fmt.Errorf("FTP 目录项本地路径无效：%q", relative)
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("FTP 目录项本地路径越界：%q", relative)
	}
	return cleanTarget, nil
}

func remoteChild(root, candidate string) bool {
	root = path.Clean(root)
	candidate = path.Clean(candidate)
	prefix := strings.TrimSuffix(root, "/") + "/"
	if root == "/" {
		prefix = "/"
	}
	return strings.HasPrefix(candidate, prefix) && candidate != root
}

func (w Worker) transfer(ctx context.Context, info sourceInfo, proxyURL, partial string, received *int64, total int64, report func(core.ProgressUpdate), roots ...*os.Root) error {
	var root *os.Root
	if len(roots) > 0 {
		root = roots[0]
	}
	conn, err := dial(ctx, info, proxyURL)
	if err != nil {
		return err
	}
	defer conn.Quit()

	var response *ftp.Response
	if *received > 0 {
		response, err = conn.RetrFrom(info.remotePath, uint64(*received))
	} else {
		response, err = conn.Retr(info.remotePath)
	}
	if err != nil && *received > 0 {
		*received = 0
		var truncateErr error
		if root == nil {
			truncateErr = os.Truncate(partial, 0)
		} else {
			var truncated *os.File
			truncated, truncateErr = root.OpenFile(partial, os.O_WRONLY|os.O_TRUNC, 0)
			if truncateErr == nil {
				truncateErr = truncated.Close()
			}
		}
		if truncateErr != nil {
			return truncateErr
		}
		response, err = conn.Retr(info.remotePath)
	}
	if err != nil {
		return fmt.Errorf("打开 FTP 数据流：%w", err)
	}
	var closeOnce sync.Once
	var closeErr error
	closeResponse := func() error {
		closeOnce.Do(func() { closeErr = response.Close() })
		return closeErr
	}
	defer closeResponse()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = closeResponse()
		case <-done:
		}
	}()
	defer close(done)
	var file *os.File
	if root == nil {
		file, err = os.OpenFile(partial, os.O_CREATE|os.O_WRONLY, 0o666)
	} else {
		file, err = root.OpenFile(partial, os.O_CREATE|os.O_WRONLY, 0o666)
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(*received, io.SeekStart); err != nil {
		return err
	}
	buffer := make([]byte, 64*1024)
	lastAt, lastBytes := time.Now(), *received
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := response.Read(buffer)
		if n > 0 {
			if w.Limiter != nil {
				if err := waitLimited(ctx, w.Limiter, n); err != nil {
					return err
				}
			}
			written, writeErr := file.Write(buffer[:n])
			*received += int64(written)
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
			now := time.Now()
			if now.Sub(lastAt) >= time.Second {
				speed := int64(float64(*received-lastBytes) / now.Sub(lastAt).Seconds())
				report(update(*received, total, speed, "正在下载 FTP 文件…"))
				lastAt, lastBytes = now, *received
			}
		}
		if errors.Is(readErr, io.EOF) {
			if err := closeResponse(); err != nil {
				return fmt.Errorf("FTP 服务器未确认传输完成：%w", err)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return file.Close()
		}
		if readErr != nil {
			return readErr
		}
	}
}

func waitLimited(ctx context.Context, limiter *ratelimit.Limiter, bytes int) error {
	for bytes > 0 {
		rate := limiter.Rate()
		if rate <= 0 {
			return nil
		}
		chunk := bytes
		if int64(chunk) > rate {
			chunk = int(rate)
		}
		if err := limiter.Wait(ctx, chunk); err != nil {
			return err
		}
		bytes -= chunk
	}
	return nil
}

func parseSource(source string) (sourceInfo, string, error) {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil || u == nil {
		return sourceInfo{}, "", errors.New("FTP 地址格式无效")
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "ftp" && scheme != "ftps" && scheme != "ftpes") || u.Hostname() == "" {
		return sourceInfo{}, "", errors.New("FTP 地址格式无效")
	}
	tlsMode := ""
	defaultPort := "21"
	if scheme == "ftps" {
		tlsMode, defaultPort = "implicit", "990"
	} else if scheme == "ftpes" {
		tlsMode = "explicit"
	}
	host := net.JoinHostPort(u.Hostname(), defaultPort)
	if u.Port() != "" {
		host = net.JoinHostPort(u.Hostname(), u.Port())
	}
	username, password := "anonymous", "anonymous@"
	if u.User != nil {
		if u.User.Username() != "" {
			username = u.User.Username()
		}
		if value, ok := u.User.Password(); ok {
			password = value
		}
	}
	remotePath, err := url.PathUnescape(u.EscapedPath())
	if err != nil || remotePath == "" {
		return sourceInfo{}, "", errors.New("FTP 地址必须指向文件或目录")
	}
	directory := strings.HasSuffix(remotePath, "/")
	clean := *u
	clean.User = nil
	return sourceInfo{host: host, remotePath: path.Clean(remotePath), username: username, password: password, tlsMode: tlsMode, timeout: 30 * time.Second, directory: directory}, clean.String(), nil
}

func sourceFromTask(task core.Task) (sourceInfo, error) {
	s := task.Stage.State
	if s == nil || s[stateHost] == "" || s[stateRemotePath] == "" {
		return sourceInfo{}, errors.New("FTP 任务缺少连接信息")
	}
	timeout := 30 * time.Second
	if seconds, err := strconv.Atoi(s[stateTimeout]); err == nil && seconds > 0 {
		timeout = time.Duration(seconds) * time.Second
	}
	return sourceInfo{host: s[stateHost], remotePath: s[stateRemotePath], username: s[stateUsername], password: s[statePassword], tlsMode: s[stateTLSMode], timeout: timeout, directory: s[stateDirectory] == "true"}, nil
}

// Closing the underlying socket interrupts both data reads and final control
// replies, without waiting for Response.Close's serialized protocol cleanup.
type contextConn struct {
	net.Conn
	stop    func() bool
	timeout time.Duration
}

func (c *contextConn) Read(buffer []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(buffer)
}

func (c *contextConn) Write(buffer []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(buffer)
}

func (c *contextConn) Close() error {
	c.stop()
	return c.Conn.Close()
}

func dial(ctx context.Context, info sourceInfo, proxyURL string) (*ftp.ServerConn, error) {
	options := []ftp.DialOption{ftp.DialWithContext(ctx), ftp.DialWithTimeout(info.timeout), ftp.DialWithShutTimeout(info.timeout)}
	baseDialer := &net.Dialer{Timeout: info.timeout}
	dialContext := baseDialer.DialContext
	if strings.TrimSpace(proxyURL) != "" {
		p, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("FTP 代理配置无效：%w", err)
		}
		dialer, err := proxy.FromURL(p, baseDialer)
		if err != nil {
			return nil, fmt.Errorf("FTP 代理配置无效：%w", err)
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("FTP 代理缺少可取消的连接接口")
		}
		dialContext = contextDialer.DialContext
	}
	var tlsConfig *tls.Config
	if info.tlsMode != "" {
		host, _, _ := net.SplitHostPort(info.host)
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		if info.tlsConfig != nil {
			tlsConfig = info.tlsConfig.Clone()
		}
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = host
		}
		if info.tlsMode == "explicit" {
			options = append(options, ftp.DialWithExplicitTLS(tlsConfig))
		} else {
			options = append(options, ftp.DialWithTLS(tlsConfig))
		}
	}
	control := true // The library dials control first, then passive data sockets.
	options = append(options, ftp.DialWithDialFunc(func(network, address string) (net.Conn, error) {
		raw, err := dialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		conn := &contextConn{Conn: raw, stop: context.AfterFunc(ctx, func() { _ = raw.Close() }), timeout: info.timeout}
		useTLS := tlsConfig != nil && (!control || info.tlsMode == "implicit")
		control = false
		if useTLS {
			// Keep data TLS handshakes lazy: servers may wait for RETR first.
			// Explicit control TLS is upgraded by the FTP library after AUTH.
			return tls.Client(conn, tlsConfig), nil
		}
		return conn, nil
	}))
	conn, err := ftp.Dial(info.host, options...)
	if err != nil {
		return nil, fmt.Errorf("连接 FTP 服务器：%w", err)
	}
	if err := conn.Login(info.username, info.password); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("FTP 登录失败：%w", err)
	}
	return conn, nil
}

func validateProxy(proxyURL string) error {
	if strings.TrimSpace(proxyURL) == "" {
		return nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil || (u.Scheme != "socks5" && u.Scheme != "socks5h") {
		return errors.New("FTP 当前仅支持直连或 SOCKS5 代理")
	}
	return nil
}

func update(received, total, speed int64, detail string) core.ProgressUpdate {
	progress := float64(0)
	if total > 0 {
		progress = float64(received) / float64(total) * 100
		if progress > 100 {
			progress = 100
		}
	}
	return core.ProgressUpdate{Received: received, FileSize: total, Speed: speed, Progress: progress, Detail: detail}
}
