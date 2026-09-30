package aria2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Manager supervises an aria2c child process and exposes a ready Client.
//
// Responsibilities:
//   - locate (or provision) the aria2c binary
//   - pick a free RPC port and start aria2c with a session file
//   - wait until the RPC endpoint answers
//   - restart the daemon if it dies unexpectedly
//   - shut everything down cleanly on exit
type Manager struct {
	log *slog.Logger

	binaryPath string
	allowFetch bool
	dir        string // session/state directory
	secret     string
	host       string

	mu       sync.Mutex
	cmd      *exec.Cmd
	port     int
	started  bool
	client   *Client
	stopping bool
	opts     Options
	exitCh   chan struct{}
	procDone chan struct{}
}

// Options configures a Manager.
type Options struct {
	Logger           *slog.Logger
	BinaryPath       string // explicit path to aria2c; may be empty
	AutoFetch        bool   // download aria2c when it cannot be located
	Dir              string // working/session directory
	Secret           string // RPC secret (generated when empty)
	Host             string // bind address for the RPC server
	MaxConc          int    // max-concurrent-downloads handed to aria2c
	MaxConnPerServer int
	MinSplitSize     string
	DiskLimit        int64 // reserved disk space in bytes (0 = default 100MB)
}

// NewManager builds a supervisor; call Start to launch aria2c.
func NewManager(opts Options) *Manager {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.MaxConnPerServer <= 0 {
		opts.MaxConnPerServer = 4
	}
	if opts.MinSplitSize == "" {
		opts.MinSplitSize = "1M"
	}
	if opts.DiskLimit <= 0 {
		opts.DiskLimit = 100 * 1024 * 1024
	}
	return &Manager{
		log:        opts.Logger,
		binaryPath: opts.BinaryPath,
		allowFetch: opts.AutoFetch,
		dir:        opts.Dir,
		secret:     opts.Secret,
		host:       opts.Host,
		port:       -1,
		opts:       opts,
		exitCh:     make(chan struct{}),
	}
}

// Client returns the RPC client (nil before Start).
func (m *Manager) Client() *Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client
}

// Port returns the RPC port in use (-1 before Start).
func (m *Manager) Port() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.port
}

// Running reports whether the supervised process is alive.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started || m.cmd == nil || m.cmd.Process == nil {
		return false
	}
	return m.procDone != nil // process handle exists; liveness checked via RPC
}

// Start resolves the binary, launches aria2c and blocks until RPC is ready.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("aria2: create state dir: %w", err)
	}

	bin, err := m.resolveBinary(ctx)
	if err != nil {
		return err
	}

	port, err := freePort(m.host)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.binaryPath = bin
	m.port = port
	m.client = NewClient(m.host, port, m.secret)
	m.mu.Unlock()

	if err := m.launch(port); err != nil {
		return err
	}
	if err := m.waitReady(ctx, 25*time.Second); err != nil {
		_ = m.shutdownProcess()
		return err
	}

	// Apply the tunables that are easier to set via RPC than on the CLI.
	if c := m.Client(); c != nil {
		rc := ctx
		if err := c.ChangeGlobalOption(rc, map[string]any{
			"max-concurrent-downloads": m.maxConc(),
		}); err != nil {
			m.log.Warn("aria2: changeGlobalOption failed", "error", err)
		}
	}

	m.log.Info("aria2 ready",
		"binary", bin, "host", m.host, "port", port, "stateDir", m.dir)
	return nil
}

func (m *Manager) maxConc() int {
	if m.opts.MaxConc >= 1 && m.opts.MaxConc <= 512 {
		return m.opts.MaxConc
	}
	return 16
}

// launch starts the aria2c process.
func (m *Manager) launch(port int) error {
	abs, err := filepath.Abs(m.binaryPath)
	if err != nil {
		abs = m.binaryPath
	}
	sessionFile := filepath.Join(m.dir, "aria2-session.txt")
	logFile := filepath.Join(m.dir, "aria2.log")
	// aria2 refuses to start when the session file is empty or missing.
	if f, err := os.OpenFile(sessionFile, os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		_ = f.Close()
	}

	args := []string{
		"--enable-rpc=true",
		"--rpc-listen-all=false",
		"--rpc-listen-port=" + strconv.Itoa(port),
		"--rpc-secret=" + m.secret,
		"--rpc-max-request-size=1024M",
		"--dir=" + m.dir,
		"--continue=true",
		"--auto-file-renaming=true",
		"--allow-overwrite=false",
		"--check-certificate=false",
		"--console-log-level=warn",
		"--log-level=notice",
		"--log=" + logFile,
		"--save-session=" + sessionFile,
		"--save-session-interval=30",
		"--input-file=" + sessionFile,
		// aria2c has no `--dht` switch; BitTorrent is disabled via
		// --enable-dht / --enable-dht6 / --bt-enable-lpd.
		"--enable-dht=false",
		"--enable-dht6=false",
		"--bt-enable-lpd=false",
		"--max-concurrent-downloads=" + strconv.Itoa(m.maxConc()),
		"--max-connection-per-server=4",
		"--min-split-size=1M",
		"--split=4",
		"--file-allocation=none",
		"--disk-cache=32M",
		"--optimize-concurrent-downloads=true",
		"--summary-interval=0",
		"--daemon=false",
	}

	cmd := exec.Command(abs, args...)
	cmd.Dir = m.dir
	// aria2c is happier without inheriting a console on Windows.
	cmd.Stdout = nil
	cmd.Stderr = nil
	configureProcess(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("aria2: start %s: %w", abs, err)
	}

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	m.mu.Lock()
	m.cmd = cmd
	m.started = true
	m.procDone = done
	m.mu.Unlock()

	go m.supervise(done)
	return nil
}

// supervise restarts aria2c if it exits unexpectedly.
func (m *Manager) supervise(done chan struct{}) {
	<-done
	m.mu.Lock()
	stopping := m.stopping
	started := m.started
	m.mu.Unlock()
	if stopping || !started {
		return
	}
	m.log.Warn("aria2 process exited; attempting restart in 2s")
	time.Sleep(2 * time.Second)

	port, err := freePort(m.host)
	if err == nil {
		m.mu.Lock()
		m.port = port
		m.client = NewClient(m.host, port, m.secret)
		m.mu.Unlock()
		if err := m.launch(port); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			if err := m.waitReady(ctx, 20*time.Second); err == nil {
				m.log.Info("aria2 restarted", "port", port)
			}
			cancel()
			return
		}
	}
	m.log.Error("aria2 restart failed; downloads are paused")
}

// waitReady polls the RPC endpoint until aria2c answers.
func (m *Manager) waitReady(ctx context.Context, timeout time.Duration) error {
	c := m.Client()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := c.GetVersion(rctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		// Fail fast when the process is already gone.
		m.mu.Lock()
		gone := false
		if m.procDone != nil {
			select {
			case <-m.procDone:
				gone = true
			default:
			}
		}
		m.mu.Unlock()
		if gone {
			return fmt.Errorf("aria2: process exited during startup (last error: %v)", lastErr)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("aria2: rpc not ready within %s (last error: %v)", timeout, lastErr)
}

// Version returns the running aria2c version (empty when unavailable).
func (m *Manager) Version(ctx context.Context) string {
	c := m.Client()
	if c == nil {
		return ""
	}
	v, err := c.GetVersion(ctx)
	if err != nil {
		return ""
	}
	return v.Version
}

// Healthy reports whether the RPC endpoint currently answers.
func (m *Manager) Healthy(ctx context.Context) bool {
	c := m.Client()
	if c == nil {
		return false
	}
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := c.GetVersion(rctx)
	return err == nil
}

// Stop terminates aria2c and waits for it to exit.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.stopping = true
	started := m.started
	m.started = false
	m.mu.Unlock()
	if !started {
		return nil
	}
	return m.shutdownProcess()
}

func (m *Manager) shutdownProcess() error {
	m.mu.Lock()
	cmd := m.cmd
	done := m.procDone
	m.cmd = nil
	m.procDone = nil
	m.started = false
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Ask politely first so the session file is flushed.
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			return errors.New("aria2: process did not exit")
		}
		return nil
	}
}

// resolveBinary finds aria2c on PATH, at the configured path, or downloads it.
func (m *Manager) resolveBinary(ctx context.Context) (string, error) {
	if m.binaryPath != "" {
		if isExecutable(m.binaryPath) {
			return m.binaryPath, nil
		}
		return "", fmt.Errorf("aria2: configured binary %q is not executable", m.binaryPath)
	}
	exeName := "aria2c"
	if runtime.GOOS == "windows" {
		exeName = "aria2c.exe"
	}

	// 1. A previously provisioned copy inside the state dir.
	candidate := filepath.Join(m.dir, exeName)
	if isExecutable(candidate) {
		return candidate, nil
	}
	// 2. System PATH.
	if p, err := exec.LookPath(exeName); err == nil {
		if isExecutable(p) {
			return p, nil
		}
	}
	// 3. Well-known install locations on Windows.
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "aria2", exeName),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "aria2", exeName),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages", exeName),
		} {
			if p != "" && isExecutable(p) {
				return p, nil
			}
		}
	}
	// 4. Download the official build.
	if !m.allowFetch {
		return "", fmt.Errorf("aria2: %s not found and auto-fetch is disabled", exeName)
	}
	m.log.Info("aria2c not found locally; downloading official build", "target", m.dir)
	if err := FetchBinary(ctx, m.dir, m.log); err != nil {
		return "", fmt.Errorf("aria2: provision binary: %w", err)
	}
	if !isExecutable(candidate) {
		return "", fmt.Errorf("aria2: binary missing after fetch (%s)", candidate)
	}
	return candidate, nil
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode()&0o111 != 0
}

// freePort asks the OS for an unused TCP port.
func freePort(host string) (int, error) {
	lc, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, fmt.Errorf("aria2: reserve port: %w", err)
	}
	defer lc.Close()
	addr, ok := lc.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("aria2: cannot determine free port")
	}
	return addr.Port, nil
}
