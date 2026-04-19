package tsproxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tsp "github.com/0xKrito/ts-proxy-android/tsproxy"
	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

func init() {
	if dir := os.Getenv("HOME"); dir != "" {
		os.Setenv("TMPDIR", filepath.Join(dir, "tsnet.tsproxy", "tmp"))
		os.Setenv("TS_LOGS_DIR", filepath.Join(dir, "tsnet.tsproxy", "logs"))
	} else {
		os.Setenv("TMPDIR", "/data/local/tmp")
		os.Setenv("TS_LOGS_DIR", "/data/local/tmp/tslogs")
	}
	log.Println("[init] TMPDIR =", os.Getenv("TMPDIR"))
	log.Println("[init] TS_LOGS_DIR =", os.Getenv("TS_LOGS_DIR"))
}

var (
	mu       sync.Mutex
	server   *tsnet.Server
	proxy    *tsp.TsProxy
	running  bool
	loginURL string
	logBuf   = newLogBuffer(300)
)

type logBuffer struct {
	mu   sync.Mutex
	msgs []string
	max  int
	fn   func(string)
}

func newLogBuffer(max int) *logBuffer { return &logBuffer{max: max} }

func (lb *logBuffer) write(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	timestamped := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05.000"), msg)
	lb.mu.Lock()
	lb.msgs = append(lb.msgs, timestamped)
	if len(lb.msgs) > lb.max {
		lb.msgs = lb.msgs[len(lb.msgs)-lb.max:]
	}
	fn := lb.fn
	lb.mu.Unlock()
	log.Println(msg) // logcat already has timestamps
	if fn != nil {
		fn(timestamped)
	}
}

func (lb *logBuffer) clear() {
	lb.mu.Lock()
	lb.msgs = nil
	lb.mu.Unlock()
}

func (lb *logBuffer) snapshot() []string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	out := make([]string, len(lb.msgs))
	copy(out, lb.msgs)
	return out
}

func dataDir(hostname string) string {
	if d := os.Getenv("HOME"); d != "" {
		return filepath.Join(d, "tsnet.tsproxy", hostname)
	}
	return "/data/local/tmp/tsnet.tsproxy/" + hostname
}

func Start(socksAddr, hostname, tsnetDir string) string {
	mu.Lock()
	defer mu.Unlock()

	if running {
		logBuf.write("Already running")
		return ""
	}

	if socksAddr == "" {
		socksAddr = "127.0.0.1:1080"
	}
	if hostname == "" {
		hostname = "ts-proxy-android"
	}

	if tsnetDir == "" || tsnetDir == "DEFAULT" {
		tsnetDir = dataDir(hostname)
	}

	logBuf.write("Starting socks=%s host=%s dir=%s", socksAddr, hostname, tsnetDir)

	if err := os.MkdirAll(tsnetDir, 0755); err != nil {
		return "ERROR: Cannot create dir: " + err.Error()
	}

	logsDir := filepath.Join(tsnetDir, "logs")
	tmpDir := filepath.Join(tsnetDir, "tmp")
	for _, d := range []string{logsDir, tmpDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return "ERROR: Cannot create " + d + ": " + err.Error()
		}
	}
	os.Setenv("TS_LOGS_DIR", logsDir)
	os.Setenv("TMPDIR", tmpDir)
	logBuf.write("Env: TS_LOGS_DIR=%s TMPDIR=%s", logsDir, tmpDir)

	logBuf.write("Testing net.Listen on %s...", socksAddr)
	// Wait briefly for port to release if previous instance is still closing
	time.Sleep(200 * time.Millisecond)
	testListener, err := net.Listen("tcp", socksAddr)
	if err != nil {
		logBuf.write("net.Listen FAILED: %v", err)
		return "ERROR: Cannot bind " + socksAddr + ": " + err.Error()
	}
	logBuf.write("net.Listen OK, closing test listener")
	testListener.Close()

	srv := &tsnet.Server{
		Hostname: hostname,
		Dir:      tsnetDir,
		Logf: func(format string, args ...any) {
			logBuf.write("[tsnet] "+format, args...)
		},
	}

	go bootServer(srv, socksAddr)

	return ""
}

func bootServer(srv *tsnet.Server, socksAddr string) {
	defer func() {
		if r := recover(); r != nil {
			logBuf.write("PANIC: %v", r)
		}
	}()

	logBuf.write("Step 1: Initializing tsnet...")
	if err := srv.Start(); err != nil {
		logBuf.write("srv.Start error: %v", err)
		return
	}
	logBuf.write("Step 1: Done")

	go func() {
		logBuf.write("Watching for login URL...")
		lc, err := srv.LocalClient()
		if err != nil {
			logBuf.write("LocalClient (watch): %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
		if err != nil {
			logBuf.write("WatchIPNBus error: %v", err)
			return
		}
		defer watcher.Close()
		for {
			n, err := watcher.Next()
			if err != nil {
				logBuf.write("Watcher done: %v", err)
				return
			}
			if url := n.BrowseToURL; url != nil && *url != "" {
				mu.Lock()
				loginURL = *url
				mu.Unlock()
				logBuf.write("LOGIN URL: %s", *url)
				return
			}
			if n.State != nil && *n.State == ipn.Running {
				logBuf.write("State=Running, no auth needed")
				return
			}
		}
	}()

	logBuf.write("Step 2: Creating TsProxy (will wait for auth)...")
	p, err := tsp.NewTsProxy(srv, 1100, 330, true)
	if err != nil {
		logBuf.write("NewTsProxy error: %v", err)
		return
	}
	logBuf.write("Step 2: TsProxy created")

	tsp.SetSocksLogf(logBuf.write)

	logBuf.write("Step 3: Serving SOCKS5 on %s", socksAddr)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logBuf.write("SOCKS panic: %v", r)
			}
		}()
		logBuf.write("About to call p.ServeSOCKS(%s, ...)", socksAddr)
		p.ServeSOCKS(socksAddr, "", "", "", "")
		logBuf.write("p.ServeSOCKS returned")
	}()

	time.Sleep(500 * time.Millisecond)

	logBuf.write("Verifying SOCKS5 port...")
	verifyConn, err := net.DialTimeout("tcp", socksAddr, 2*time.Second)
	if err != nil {
		logBuf.write("WARNING: Cannot connect to SOCKS5: %v", err)
	} else {
		verifyConn.Close()
		logBuf.write("SOCKS5 port is listening")
	}

	mu.Lock()
	server = srv
	proxy = p
	running = true
	loginURL = ""
	mu.Unlock()
	logBuf.write("ts-proxy is running!")
}

func GetLoginURL() string {
	mu.Lock()
	defer mu.Unlock()
	return loginURL
}

func Stop() {
	mu.Lock()

	if !running && server == nil {
		mu.Unlock()
		logBuf.write("Not running")
		return
	}
	logBuf.write("Stopping...")

	// Close tsnet server first (releases its network resources)
	srv := server
	server = nil
	proxy = nil
	running = false
	mu.Unlock()

	if srv != nil {
		srv.Close()
		logBuf.write("tsnet closed")
	}

	// Shut down SOCKS5 server synchronously (closes TCP+UDP listeners, frees port)
	// Must wait for this to finish before next Start() can bind the port
	tsp.StopSocks5()

	logBuf.write("Stopped")
}

func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return running
}

func GetTailscaleIP() string {
	mu.Lock()
	defer mu.Unlock()
	if server == nil {
		return ""
	}
	lc, err := server.LocalClient()
	if err != nil {
		return ""
	}
	st, err := lc.Status(context.Background())
	if err != nil {
		return ""
	}
	for _, ip := range st.Self.TailscaleIPs {
		if ip.Is4() {
			return ip.String()
		}
	}
	return ""
}

// GetLogs returns all accumulated log messages as a single string.
func GetLogs() string {
	return strings.Join(logBuf.snapshot(), "\n")
}

// ClearLogs removes all log messages.
func ClearLogs() {
	logBuf.clear()
}

func SetLogCallback(fn func(string)) {
	logBuf.mu.Lock()
	defer logBuf.mu.Unlock()
	logBuf.fn = fn
}
