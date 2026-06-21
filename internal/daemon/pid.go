// Package daemon manages the go-rag background MCP server lifecycle: start
// (re-exec detached), stop (SIGTERM), status (health probe), plus pidfile and
// address-sidecar bookkeeping. Mirrors muninn's daemon model.
package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Addrs records the addresses the daemon bound to, so stop/status and the stdio
// proxy can find each transport even with non-default --mcp-addr/--rest-addr/
// --grpc-addr. RESTAddr/GRPCAddr are empty when those transports are disabled.
type Addrs struct {
	MCPAddr  string `json:"mcp_addr"`
	RESTAddr string `json:"rest_addr"`
	GRPCAddr string `json:"grpc_addr"`
}

const (
	pidFileName   = "daemon.pid"
	addrsFileName = "daemon.addrs"
	logFileName   = "daemon.log"
	tokenFileName = "mcp.token"
)

func PIDPath(dbPath string) string    { return filepath.Join(dbPath, pidFileName) }
func AddrsPath(dbPath string) string  { return filepath.Join(dbPath, addrsFileName) }
func LogPath(dbPath string) string    { return filepath.Join(dbPath, logFileName) }
func TokenPath(dbPath string) string  { return filepath.Join(dbPath, tokenFileName) }

// WritePID records pid to the daemon.pid file.
func WritePID(dbPath string, pid int) error {
	return os.WriteFile(PIDPath(dbPath), []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

// ReadPID reads the daemon pid, or an error if absent/unparseable.
func ReadPID(dbPath string) (int, error) {
	b, err := os.ReadFile(PIDPath(dbPath))
	if err != nil {
		return 0, fmt.Errorf("no pid file at %s — go-rag may not be running (try 'go-rag start'): %w", PIDPath(dbPath), err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file: %w", err)
	}
	return pid, nil
}

// WriteAddrs records the bound transport addresses.
func WriteAddrs(dbPath string, a Addrs) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return os.WriteFile(AddrsPath(dbPath), b, 0o600)
}

// ReadAddrs reads the bound transport addresses (fields empty if absent).
func ReadAddrs(dbPath string) (Addrs, error) {
	b, err := os.ReadFile(AddrsPath(dbPath))
	if err != nil {
		return Addrs{}, err
	}
	var a Addrs
	if err := json.Unmarshal(b, &a); err != nil {
		return Addrs{}, err
	}
	return a, nil
}

// ReadToken reads the optional bearer token from mcp.token ("" if absent).
func ReadToken(dbPath string) string {
	b, err := os.ReadFile(TokenPath(dbPath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
