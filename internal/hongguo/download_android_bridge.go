package hongguo

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed download_android.js
var androidRPCScript string

const androidPackage = "com.phoenix.read"

type androidDownload struct {
	address, tools string
	gate           chan struct{}
}

// EnableAndroidDownload 在启动时配置专用 Android；不参与普通来源优先级。
func (c *Client) EnableAndroidDownload(address, tools string) {
	if address != "" {
		c.android = &androidDownload{address: address, tools: tools, gate: make(chan struct{}, 1)}
	}
}

func (c *Client) AndroidDownloadEnabled() bool { return c.android != nil }

var androidHost = regexp.MustCompile(`^[a-zA-Z0-9._:-]+$`)

func validAndroidAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	n, numberErr := strconv.Atoi(port)
	return err == nil && numberErr == nil && n > 0 && n <= 65535 && host != "" && androidHost.MatchString(host) && !strings.HasPrefix(host, "-")
}

// 每次请求使用独立 ADB socket、密钥目录和远端脚本；清理完成后才允许下一目标调用 App。
func (a *androidDownload) resolve(parent context.Context, _, videoID string) (DownloadMedia, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	select {
	case a.gate <- struct{}{}:
		defer func() { <-a.gate }()
	case <-ctx.Done():
		return DownloadMedia{}, ctx.Err()
	}
	if !validAndroidAddress(a.address) {
		return DownloadMedia{}, errors.New("Android ADB 地址无效")
	}
	adb, err := exec.LookPath("adb")
	if err != nil {
		return DownloadMedia{}, errors.New("运行 MediaStationGo 的环境未找到 adb，请安装工具并配置 PATH，参见 docs/hongguo-android.md")
	}
	dir, err := os.MkdirTemp("/tmp", "hg-android-")
	if err != nil {
		return DownloadMedia{}, errors.New("Android 临时目录不可写")
	}
	defer os.RemoveAll(dir)
	// Alpine ADB 使用 HOME 存放密钥；仅设置子进程用户目录，不改变宿主进程环境。
	env := append(os.Environ(), "ADB_SERVER_SOCKET=localfilesystem:"+filepath.Join(dir, "adb.sock"), "HOME="+dir, "TMPDIR="+dir)
	command := func(commandCtx context.Context, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(commandCtx, adb, args...)
		cmd.Env = env
		cmd.WaitDelay = time.Second
		return cmd
	}
	run := func(commandCtx context.Context, args ...string) ([]byte, error) {
		out, err := command(commandCtx, args...).Output()
		if err != nil {
			if commandCtx.Err() != nil {
				return nil, commandCtx.Err()
			}
			return nil, errors.New("Android 控制失败，请检查容器与 App 初始化")
		}
		return out, nil
	}
	remote := "/data/local/tmp/" + filepath.Base(dir)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		// PID 与唯一脚本路径同时匹配，避免取消时误杀已复用的 PID。
		_, _ = run(cleanup, "-s", a.address, "shell", "su 0 sh -c 'g=$(head -n 1 "+remote+"/pids 2>/dev/null); case $g in *[!0-9]*|\"\") ;; *) for p in $(cat "+remote+"/pids); do case $p in *[!0-9]*|\"\") continue ;; esac; case $(cat /proc/$p/cmdline 2>/dev/null) in *"+remote+"/inject*) kill -9 -$g; break ;; esac; done ;; esac; rm -rf "+remote+"'")
		_, _ = run(cleanup, "-s", a.address, "shell", "am force-stop "+androidPackage)
		_, _ = run(cleanup, "kill-server")
	}()
	if _, err = run(ctx, "connect", a.address); err != nil {
		return DownloadMedia{}, err
	}
	device := func(args ...string) ([]byte, error) { return run(ctx, append([]string{"-s", a.address}, args...)...) }
	version, err := device("shell", "dumpsys package "+androidPackage)
	if err != nil {
		return DownloadMedia{}, err
	}
	if !strings.Contains(string(version), "versionCode=73932 ") {
		return DownloadMedia{}, errors.New("Android 需要已初始化的官方红果 App 7.3.9.32，请按部署文档安装")
	}
	if _, err = device("shell", "mkdir -p "+remote); err != nil {
		return DownloadMedia{}, err
	}
	tool := "/data/local/tmp/mediastation-inject-16.7.19"
	if _, err = device("shell", "test -x "+tool); err != nil {
		if _, err = device("push", filepath.Join(a.tools, "frida-inject-android"), tool); err != nil {
			return DownloadMedia{}, err
		}
		if _, err = device("shell", "chmod 755 "+tool); err != nil {
			return DownloadMedia{}, err
		}
	}
	script := filepath.Join(dir, "rpc.js")
	if err = os.WriteFile(script, []byte(strings.ReplaceAll(androidRPCScript, "__VIDEO_ID__", strconv.Quote(videoID))), 0o600); err != nil {
		return DownloadMedia{}, errors.New("Android 取模型脚本不可写")
	}
	if _, err = device("push", script, remote+"/rpc.js"); err != nil {
		return DownloadMedia{}, err
	}
	if _, err = device("shell", "ln -s "+tool+" "+remote+"/inject; am force-stop "+androidPackage+"; am start -n "+androidPackage+"/com.dragon.read.pages.splash.SplashActivity; input keyevent 3"); err != nil {
		return DownloadMedia{}, err
	}
	if err = androidPause(ctx, 6*time.Second); err != nil {
		return DownloadMedia{}, err
	}
	pid, err := device("shell", "ps -A -o PID,PPID,NAME")
	if err != nil {
		return DownloadMedia{}, err
	}
	pidText := androidMainPID(string(pid))
	if pidText == "" {
		return DownloadMedia{}, errors.New("Android App 主进程未就绪")
	}
	rpcCtx, stopRPC := context.WithCancel(ctx)
	defer stopRPC()
	// 独立进程组和远端看门狗确保 ADB 断线时也不会无限遗留取模型进程。
	cmd := command(rpcCtx, "-s", a.address, "exec-out", "su 0 setsid sh -c 'echo $$ > "+remote+"/pids; (sleep 115; kill -9 -$$) >/dev/null 2>&1 & echo $! >> "+remote+"/pids; exec "+remote+"/inject -p "+pidText+" -s "+remote+"/rpc.js'")
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return DownloadMedia{}, errors.New("Android 取模型进程无法启动")
	}
	events := make(chan androidModelEvent, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(events)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), maxResponseBytes)
		for scanner.Scan() {
			event, ok := parseAndroidEvent(scanner.Bytes(), videoID)
			if ok {
				select {
				case events <- event:
				case <-rpcCtx.Done():
					return
				}
			}
		}
	}()
	defer func() { stopRPC(); _ = cmd.Wait(); <-readDone }()
	wait := func(kind string) (androidModelEvent, error) {
		for {
			select {
			case <-ctx.Done():
				return androidModelEvent{}, ctx.Err()
			case event, ok := <-events:
				if !ok || event.Kind == "error" {
					if ctx.Err() != nil {
						return androidModelEvent{}, ctx.Err()
					}
					return androidModelEvent{}, errors.New("Android 取模型进程未返回有效模型")
				}
				if event.Kind == kind {
					return event, nil
				}
			}
		}
	}
	if _, err = wait("ready"); err != nil {
		return DownloadMedia{}, err
	}
	event, err := wait("model")
	if err != nil {
		return DownloadMedia{}, err
	}
	return event.Media, nil
}

// App 会 fork 同名子进程；只连接父进程，不能直接把 pidof 的多 PID 输出当成一个 PID。
func androidMainPID(output string) string {
	parents := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == androidPackage {
			pid, err := strconv.Atoi(fields[0])
			if err == nil && pid > 0 {
				parents[fields[0]] = fields[1]
			}
		}
	}
	main := ""
	for pid, parent := range parents {
		if _, child := parents[parent]; !child {
			if main != "" {
				return ""
			}
			main = pid
		}
	}
	return main
}

type androidModelEvent struct {
	Kind    string          `json:"kind"`
	VideoID string          `json:"video_id"`
	Model   json.RawMessage `json:"model"`
	Media   DownloadMedia   `json:"-"`
}

func parseAndroidEvent(line []byte, videoID string) (androidModelEvent, bool) {
	var message struct {
		Type    string            `json:"type"`
		Payload androidModelEvent `json:"payload"`
	}
	if json.Unmarshal(line, &message) != nil || message.Type != "send" {
		return androidModelEvent{}, false
	}
	event := message.Payload
	if event.Kind == "model" {
		if event.VideoID != videoID {
			return androidModelEvent{}, false
		}
		var model map[string]any
		decoder := json.NewDecoder(bytes.NewReader(event.Model))
		decoder.UseNumber()
		if decoder.Decode(&model) != nil {
			return androidModelEvent{}, false
		}
		media, err := parseDownloadAppMedia(model)
		event.Media = media
		return event, err == nil
	}
	return event, event.Kind == "ready" || event.Kind == "error"
}

func androidPause(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
