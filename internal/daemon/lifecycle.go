package daemon

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Start re-execs the go-rag binary as a detached daemon (`go-rag serve`) and polls
// the health endpoint until it is up (≤5s). Mirrors muninn's runStart.
func Start(dbPath, mcpAddr string) error {
	if mcpAddr == "" {
		mcpAddr = ":7878"
	}
	if pid, err := ReadPID(dbPath); err == nil && isProcessRunning(pid) {
		return fmt.Errorf("go-rag already running (pid %d)", pid)
	}
	// Clear a stale pidfile.
	if _, err := os.Stat(PIDPath(dbPath)); err == nil {
		_ = os.Remove(PIDPath(dbPath))
	}
	// Guard against another process already holding the Pebble lock.
	if isPebbleLockHeld(filepath.Join(dbPath, "data", "LOCK")) {
		return fmt.Errorf("another process is holding the go-rag database lock (is the daemon already running, or managed externally?)")
	}
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	args := []string{"serve", "--db-path", dbPath, "--mcp-addr", mcpAddr}
	cmd := exec.Command(os.Args[0], args...)
	cmd.SysProcAttr = daemonSysProcAttr()
	daemonExtraSetup(cmd)
	cmd.Stdin = nil
	lf, logErr := os.OpenFile(LogPath(dbPath), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if logErr == nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
	}
	if err := cmd.Start(); err != nil {
		if lf != nil {
			lf.Close()
		}
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	if lf != nil {
		lf.Close()
	}
	if err := WritePID(dbPath, cmd.Process.Pid); err != nil {
		return fmt.Errorf("write pid: %w", err)
	}
	_ = WriteAddrs(dbPath, Addrs{MCPAddr: mcpAddr})

	health := HealthURL(mcpAddr)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if probeHealth(health) {
			return nil
		}
	}
	return fmt.Errorf("daemon started (pid %d) but health check timed out; see %s", cmd.Process.Pid, LogPath(dbPath))
}

// Stop signals the running daemon (SIGTERM) and waits for it to exit.
func Stop(dbPath string) error {
	pid, err := ReadPID(dbPath)
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %w", err)
	}
	if err := stopProcess(proc); err != nil {
		return fmt.Errorf("failed to stop: %w", err)
	}
	if err := waitForExit(pid, 35*time.Second); err != nil {
		return fmt.Errorf("%w — force-kill with: kill -9 %d", err, pid)
	}
	_ = os.Remove(PIDPath(dbPath))
	_ = os.Remove(AddrsPath(dbPath))
	return nil
}

func waitForExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessRunning(pid) {
			time.Sleep(300 * time.Millisecond) // let the kernel release the Pebble flock
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d still running after %v", pid, timeout)
}

// IsRunning reports whether the daemon process is alive.
func IsRunning(dbPath string) bool {
	pid, err := ReadPID(dbPath)
	if err != nil {
		return false
	}
	return isProcessRunning(pid)
}

// Status reports the running state, pid, and bound MCP address (probes health).
func Status(dbPath string) (running bool, pid int, addr string) {
	pid, err := ReadPID(dbPath)
	if err != nil {
		return false, 0, ""
	}
	if !isProcessRunning(pid) {
		return false, pid, ""
	}
	addrs, _ := ReadAddrs(dbPath)
	if addrs.MCPAddr == "" {
		addrs.MCPAddr = ":7878"
	}
	return probeHealth(HealthURL(addrs.MCPAddr)), pid, addrs.MCPAddr
}

// HealthURL turns a listen address into the daemon health-probe URL.
func HealthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "127.0.0.1", "7878"
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/mcp/health"
}

// MCPURL turns a listen address into the MCP endpoint URL.
func MCPURL(addr string) string {
	return HealthURL(addr)[:len(HealthURL(addr))-len("/health")]
}

func probeHealth(url string) bool {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
