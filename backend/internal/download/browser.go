package download

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type captureResult struct {
	URL     string
	Headers http.Header
	Cookies string
}

// browserOptions controls one browser capture run.
type browserOptions struct {
	PageURL        string
	Path           string
	Port           int
	Timeout        time.Duration
	TempDir        string
	ProfileDir     string
	LoginURL       string
	LoginUser      string
	LoginPassword  string
	UserSelector   string
	PassSelector   string
	SubmitSelector string
	Cookies        string
}

// findBrowser returns a Chromium-based browser path.
func findBrowser(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		if p, err := exec.LookPath(explicit); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("找不到浏览器：%s", explicit)
	}
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(base, "Chromium", "Application", "chrome.exe"),
			)
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		candidates = []string{
			"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable", "/opt/google/chrome/chrome",
			"/usr/bin/chromium", "/usr/bin/chromium-browser", "/snap/bin/chromium",
			"/usr/bin/microsoft-edge",
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("没有找到 Chrome / Edge / Chromium，请安装或设置 DOWNLOAD_BROWSER")
}

// captureStream opens a headless browser, loads the page and returns the first m3u8 request.
func captureStream(ctx context.Context, opts browserOptions) (captureResult, error) {
	if opts.Port > 0 {
		return captureOnPort(ctx, opts.Port, opts)
	}
	exe, err := findBrowser(opts.Path)
	if err != nil {
		return captureResult{}, err
	}
	base := opts.TempDir
	if base == "" {
		base = tempBase()
	}
	port, stop, err := launchBrowser(exe, base, opts.ProfileDir, nil)
	if err != nil && runtime.GOOS == "windows" {
		// 受限令牌 / 作业对象里 Chrome 自带沙箱会直接崩，退回 --no-sandbox 再试一次
		port, stop, err = launchBrowser(exe, base, opts.ProfileDir, []string{"--no-sandbox"})
	}
	if err != nil {
		return captureResult{}, err
	}
	defer stop()
	return captureOnPort(ctx, port, opts)
}

func launchBrowser(exe, base, profileDir string, extra []string) (int, func(), error) {
	if base != "" {
		_ = os.MkdirAll(base, 0o755)
	}
	persistent := strings.TrimSpace(profileDir) != ""
	var profile string
	var err error
	if persistent {
		profile = profileDir
		if abs, aerr := filepath.Abs(profile); aerr == nil {
			profile = abs
		}
		if err := os.MkdirAll(profile, 0o755); err != nil {
			return 0, nil, err
		}
	} else {
		profile, err = os.MkdirTemp(base, "catalog-browser-*")
		if err != nil {
			return 0, nil, err
		}
		if abs, aerr := filepath.Abs(profile); aerr == nil {
			profile = abs
		}
	}
	logFile, err := os.CreateTemp(base, "catalog-browser-*.log")
	if err != nil {
		if !persistent {
			_ = os.RemoveAll(profile)
		}
		return 0, nil, err
	}
	cleanup := func() {
		_ = logFile.Close()
		_ = os.Remove(logFile.Name())
		if !persistent {
			_ = os.RemoveAll(profile)
		}
	}
	logTail := func() string {
		b, _ := os.ReadFile(logFile.Name())
		return tail(string(b))
	}

	args := []string{
		"--headless=new",
		"--remote-debugging-port=0",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-extensions",
		"--mute-audio",
		"--autoplay-policy=no-user-gesture-required",
		"--window-size=1280,900",
	}
	if runtime.GOOS != "windows" {
		args = append(args, "--disable-dev-shm-usage")
		if os.Getenv("DOWNLOAD_NO_SANDBOX") == "1" {
			args = append(args, "--no-sandbox")
		}
	}
	args = append(args, extra...)
	args = append(args, "about:blank")

	cmd := exec.Command(exe, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		cleanup()
		return 0, nil, fmt.Errorf("浏览器启动失败：%w", err)
	}
	waitCh := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		waitCh <- cmd.Wait()
		close(exited)
	}()
	stop := func() {
		_ = cmd.Process.Kill()
		<-waitCh
		cleanup()
	}

	portFile := filepath.Join(profile, "DevToolsActivePort")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(portFile); err == nil {
			line := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)
			var n int
			if c, err := fmt.Sscanf(line[0], "%d", &n); c == 1 && err == nil && n > 0 {
				return n, stop, nil
			}
		}
		select {
		case <-exited:
			code := -1
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}
			msg := fmt.Sprintf("浏览器启动失败（exit=%d）：%s", code, logTail())
			cleanup()
			return 0, nil, errors.New(msg)
		case <-time.After(150 * time.Millisecond):
		}
	}
	msg := fmt.Sprintf("浏览器调试端口未就绪：%s", logTail())
	stop()
	return 0, nil, errors.New(msg)
}

// captureOnPort drives an existing DevTools endpoint. It is used by the launcher
// above and by tests (or an already-running, logged-in browser).
func captureOnPort(ctx context.Context, port int, opts browserOptions) (captureResult, error) {
	pageURL := opts.PageURL
	target, err := firstPageTarget(port)
	if err != nil {
		return captureResult{}, err
	}
	conn, err := dialWS(target, 10*time.Second)
	if err != nil {
		return captureResult{}, fmt.Errorf("连接浏览器调试端口失败：%w", err)
	}
	defer conn.Close()

	cdp := newCDP(conn)
	_, _ = cdp.Call(ctx, "Page.enable", nil)
	_, _ = cdp.Call(ctx, "Network.enable", nil)
	_, _ = cdp.Call(ctx, "Runtime.enable", nil)

	if opts.Cookies != "" {
		applyCookies(ctx, cdp, pageURL, opts.Cookies)
	}
	if err := browserLogin(ctx, cdp, opts); err != nil {
		return captureResult{}, err
	}

	var (
		mu      sync.Mutex
		found   string
		headers http.Header
	)
	consider := func(raw string, hdr map[string]string) {
		if raw == "" || !strings.Contains(strings.ToLower(raw), ".m3u8") {
			return
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if found == "" {
			found = raw
			headers = http.Header{}
			for k, v := range hdr {
				headers.Set(k, v)
			}
		}
	}
	cdp.On("Network.requestWillBeSent", func(raw json.RawMessage) {
		var ev struct {
			Request struct {
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
			} `json:"request"`
		}
		if json.Unmarshal(raw, &ev) == nil {
			consider(ev.Request.URL, ev.Request.Headers)
		}
	})

	if _, err := cdp.Call(ctx, "Page.navigate", map[string]any{"url": pageURL}); err != nil {
		return captureResult{}, err
	}
	time.Sleep(1200 * time.Millisecond)
	_, _ = cdp.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": playJS, "awaitPromise": false, "returnByValue": true,
	})
	_, _ = cdp.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mousePressed", "x": 640, "y": 420, "button": "left", "clickCount": 1,
	})
	_, _ = cdp.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseReleased", "x": 640, "y": 420, "button": "left", "clickCount": 1,
	})

	wait := time.Now().Add(opts.Timeout)
	firstAt := time.Time{}
	for time.Now().Before(wait) {
		mu.Lock()
		got := found
		mu.Unlock()
		if got != "" {
			if firstAt.IsZero() {
				firstAt = time.Now()
			} else if time.Since(firstAt) > 4*time.Second {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	mu.Lock()
	streamURL, hdr := found, headers
	mu.Unlock()
	if streamURL == "" {
		return captureResult{}, errors.New("浏览器里没有捕获到 m3u8（可能需要登录，或播放器使用 blob: 直出）")
	}

	cookies := ""
	if raw, err := cdp.Call(ctx, "Network.getCookies", map[string]any{"urls": []string{pageURL}}); err == nil {
		var res struct {
			Cookies []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"cookies"`
		}
		if json.Unmarshal(raw, &res) == nil {
			var parts []string
			for _, c := range res.Cookies {
				if c.Name != "" {
					parts = append(parts, c.Name+"="+c.Value)
				}
			}
			cookies = strings.Join(parts, "; ")
		}
	}
	return captureResult{URL: streamURL, Headers: hdr, Cookies: cookies}, nil
}

func applyCookies(ctx context.Context, cdp *cdpClient, pageURL, raw string) {
	if _, err := url.Parse(pageURL); err != nil {
		return
	}
	for _, part := range strings.Split(raw, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		_, _ = cdp.Call(ctx, "Network.setCookie", map[string]any{
			"name": strings.TrimSpace(k), "value": strings.TrimSpace(v), "url": pageURL, "path": "/",
		})
	}
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

const loginJS = `(async function () {
  var q = function (s) { return s ? document.querySelector(s) : null; };
  var user = q(%s) || document.querySelector('input[type=email],input[type=tel],input[name*=user],input[name*=account],input[name*=phone],input[type=text]');
  var pass = q(%s) || document.querySelector('input[type=password]');
  if (!user || !pass) return 'no-form';
  var set = function (el, v) {
    el.focus(); el.value = v;
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  set(user, %s); set(pass, %s);
  var btn = q(%s) || document.querySelector('button[type=submit],form button');
  if (btn) { btn.click(); return 'clicked'; }
  if (pass.form) { if (pass.form.requestSubmit) pass.form.requestSubmit(); else pass.form.submit(); return 'submitted'; }
  return 'no-submit';
})()`

// browserLogin fills and submits a login form. It returns nil when the page has
// no form (already logged in, or a non-form login), and a descriptive error when
// the form exists but cannot be submitted.
func browserLogin(ctx context.Context, cdp *cdpClient, opts browserOptions) error {
	if strings.TrimSpace(opts.LoginURL) == "" || opts.LoginUser == "" {
		return nil
	}
	if _, err := cdp.Call(ctx, "Page.navigate", map[string]any{"url": opts.LoginURL}); err != nil {
		return fmt.Errorf("打开登录页失败：%w", err)
	}
	time.Sleep(1500 * time.Millisecond)
	expr := fmt.Sprintf(loginJS,
		jsString(opts.UserSelector), jsString(opts.PassSelector),
		jsString(opts.LoginUser), jsString(opts.LoginPassword), jsString(opts.SubmitSelector))
	raw, err := cdp.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": expr, "returnByValue": true, "awaitPromise": true,
	})
	if err != nil {
		return fmt.Errorf("自动登录失败：%w", err)
	}
	var res struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &res)
	if res.Result.Value == "no-submit" {
		return errors.New("登录页没有找到提交按钮，请配置 DOWNLOAD_LOGIN_SUBMIT_SELECTOR")
	}
	// 等 XHR / 跳转落定；失败时也继续，让后面的抓流给出实际原因
	time.Sleep(2500 * time.Millisecond)
	return nil
}

const playJS = `(function () {  var n = 0;
  document.querySelectorAll('video,audio').forEach(function (v) {
    try { v.muted = true; v.setAttribute('muted', ''); var p = v.play(); if (p && p.catch) p.catch(function () {}); n++; } catch (e) {}
  });
  ['[class*=play]', '[id*=play]', '.vjs-big-play-button', '.dplayer-play-icon', '.xgplayer-start'].forEach(function (s) {
    try { var el = document.querySelector(s); if (el) { el.click(); n++; } } catch (e) {}
  });
  return n;
})()`

func tempBase() string {
	if v := strings.TrimSpace(os.Getenv("DOWNLOAD_TMP")); v != "" {
		_ = os.MkdirAll(v, 0o755)
		return v
	}
	return ""
}

func firstPageTarget(port int) (string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
		if err == nil {
			var list []struct {
				Type                 string `json:"type"`
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if json.Unmarshal(body, &list) == nil {
				for _, t := range list {
					if t.Type == "page" && t.WebSocketDebuggerURL != "" {
						return t.WebSocketDebuggerURL, nil
					}
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return "", errors.New("没有拿到浏览器页面调试地址")
}

// ---- minimal RFC6455 client ----

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
}

func dialWS(raw string, timeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf("不支持的调试地址：%s", raw)
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, u.Host, key)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 64*1024)
	status, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		return nil, fmt.Errorf("WebSocket 握手失败：%s", strings.TrimSpace(status))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return &wsConn{conn: conn, r: reader}, nil
}

func (w *wsConn) Close() error { return w.conn.Close() }

func (w *wsConn) WriteText(s string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	payload := []byte(s)
	head := []byte{0x81}
	n := len(payload)
	switch {
	case n < 126:
		head = append(head, byte(0x80|n))
	case n < 65536:
		head = append(head, 0x80|126, byte(n>>8), byte(n))
	default:
		head = append(head, 0x80|127)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(n))
		head = append(head, buf[:]...)
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	head = append(head, mask...)
	masked := make([]byte, n)
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	_, err := w.conn.Write(append(head, masked...))
	return err
}

func (w *wsConn) ReadMessage() ([]byte, error) {
	var payload []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		opcode := h[0] & 0x0F
		masked := h[1]&0x80 != 0
		length := uint64(h[1] & 0x7F)
		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.r, ext[:]); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.r, ext[:]); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(ext[:])
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.r, mask[:]); err != nil {
				return nil, err
			}
		}
		chunk := make([]byte, length)
		if _, err := io.ReadFull(w.r, chunk); err != nil {
			return nil, err
		}
		if masked {
			for i := range chunk {
				chunk[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case 0x8:
			return nil, io.EOF
		case 0x9:
			_ = w.writeFrame(0xA, chunk)
			continue
		case 0xA:
			continue
		case 0x0:
			payload = append(payload, chunk...)
		default:
			payload = chunk
		}
		if fin && payload != nil {
			return payload, nil
		}
	}
}

func (w *wsConn) writeFrame(opcode byte, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	head := []byte{0x80 | opcode}
	n := len(payload)
	if n < 126 {
		head = append(head, byte(0x80|n))
	} else if n < 65536 {
		head = append(head, 0x80|126, byte(n>>8), byte(n))
	} else {
		head = append(head, 0x80|127)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(n))
		head = append(head, buf[:]...)
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	head = append(head, mask...)
	out := make([]byte, n)
	for i, b := range payload {
		out[i] = b ^ mask[i%4]
	}
	_, err := w.conn.Write(append(head, out...))
	return err
}

// ---- CDP client ----

type cdpClient struct {
	ws      *wsConn
	mu      sync.Mutex
	nextID  int
	pending map[int]chan json.RawMessage
	on      map[string][]func(json.RawMessage)
	closed  chan struct{}
}

func newCDP(ws *wsConn) *cdpClient {
	c := &cdpClient{
		ws:      ws,
		pending: map[int]chan json.RawMessage{},
		on:      map[string][]func(json.RawMessage){},
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *cdpClient) On(method string, fn func(json.RawMessage)) {
	c.mu.Lock()
	c.on[method] = append(c.on[method], fn)
	c.mu.Unlock()
}

func (c *cdpClient) readLoop() {
	defer close(c.closed)
	for {
		raw, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		if msg.ID != nil {
			c.mu.Lock()
			ch := c.pending[*msg.ID]
			delete(c.pending, *msg.ID)
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg.Params:
				default:
				}
			}
			continue
		}
		c.mu.Lock()
		handlers := append([]func(json.RawMessage){}, c.on[msg.Method]...)
		c.mu.Unlock()
		for _, fn := range handlers {
			fn(msg.Params)
		}
	}
}

func (c *cdpClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan json.RawMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if err := c.ws.WriteText(string(payload)); err != nil {
		return nil, err
	}
	select {
	case raw := <-ch:
		return raw, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(20 * time.Second):
		return nil, fmt.Errorf("浏览器命令超时：%s", method)
	case <-c.closed:
		return nil, errors.New("浏览器调试连接已关闭")
	}
}
