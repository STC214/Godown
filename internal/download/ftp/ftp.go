package ftpdownload

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"golang.org/x/net/proxy"

	"ghost-downloader-go-win32/internal/core"
	"ghost-downloader-go-win32/internal/ratelimit"
)

const (
	stateHost       = "host"
	stateRemotePath = "remotePath"
	stateUsername   = "username"
	statePassword   = "password"
	stateTLSMode    = "tlsMode"
	stateTimeout    = "timeoutSeconds"
)

type Worker struct {
	Limiter *ratelimit.Limiter
}

type sourceInfo struct {
	host, remotePath, username, password, tlsMode string
	timeout                                       time.Duration
}

func IsSource(source string) bool {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "ftp") || strings.EqualFold(u.Scheme, "ftps") || strings.EqualFold(u.Scheme, "ftpes")
}

func Parse(ctx context.Context, source, downloadDir, proxyURL string, maxRetries int) (core.Task, error) {
	info, cleanURL, err := parseSource(source)
	if err != nil {
		return core.Task{}, err
	}
	if err := validateProxy(proxyURL); err != nil {
		return core.Task{}, err
	}
	conn, err := dial(ctx, info, proxyURL)
	if err != nil {
		return core.Task{}, err
	}
	defer conn.Quit()
	size, err := conn.FileSize(info.remotePath)
	if err != nil {
		return core.Task{}, fmt.Errorf("读取 FTP 文件大小：%w", err)
	}
	title := filepath.Base(filepath.FromSlash(info.remotePath))
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

func (w Worker) Run(ctx context.Context, task core.Task, report func(core.ProgressUpdate)) error {
	info, err := sourceFromTask(task)
	if err != nil {
		return err
	}
	if report == nil {
		report = func(core.ProgressUpdate) {}
	}
	if err := os.MkdirAll(task.Path, 0o755); err != nil {
		return err
	}
	output := task.OutputFile()
	partial := output + ".part"
	var offset int64
	if stat, statErr := os.Stat(partial); statErr == nil {
		offset = stat.Size()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
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
	report(update(received, task.FileSize, 0, "FTP 下载完成"))
	return nil
}

func (w Worker) transfer(ctx context.Context, info sourceInfo, proxyURL, partial string, received *int64, total int64, report func(core.ProgressUpdate)) error {
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
		if truncateErr := os.Truncate(partial, 0); truncateErr != nil {
			return truncateErr
		}
		response, err = conn.Retr(info.remotePath)
	}
	if err != nil {
		return fmt.Errorf("打开 FTP 数据流：%w", err)
	}
	defer response.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = response.Close()
		case <-done:
		}
	}()
	defer close(done)
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY, 0o666)
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
			return nil
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
	scheme := strings.ToLower(u.Scheme)
	if err != nil || (scheme != "ftp" && scheme != "ftps" && scheme != "ftpes") || u.Hostname() == "" {
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
	if err != nil || remotePath == "" || strings.HasSuffix(remotePath, "/") {
		return sourceInfo{}, "", errors.New("FTP 地址必须指向文件")
	}
	clean := *u
	clean.User = nil
	return sourceInfo{host: host, remotePath: path.Clean(remotePath), username: username, password: password, tlsMode: tlsMode, timeout: 30 * time.Second}, clean.String(), nil
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
	return sourceInfo{host: s[stateHost], remotePath: s[stateRemotePath], username: s[stateUsername], password: s[statePassword], tlsMode: s[stateTLSMode], timeout: timeout}, nil
}

func dial(ctx context.Context, info sourceInfo, proxyURL string) (*ftp.ServerConn, error) {
	options := []ftp.DialOption{ftp.DialWithContext(ctx), ftp.DialWithTimeout(info.timeout)}
	if strings.TrimSpace(proxyURL) != "" {
		p, _ := url.Parse(proxyURL)
		dialer, err := proxy.FromURL(p, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("FTP 代理配置无效：%w", err)
		}
		options = append(options, ftp.DialWithDialFunc(dialer.Dial))
	}
	if info.tlsMode != "" {
		host, _, _ := net.SplitHostPort(info.host)
		tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if info.tlsMode == "explicit" {
			options = append(options, ftp.DialWithExplicitTLS(tlsConfig))
		} else {
			options = append(options, ftp.DialWithTLS(tlsConfig))
		}
	}
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
