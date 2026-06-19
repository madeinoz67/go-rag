package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/spf13/cobra"
)

// newMCPCmd is a stdio→HTTP proxy: it bridges a stdio MCP client (Claude Desktop)
// to the running daemon's HTTP MCP endpoint, mirroring muninn's `muninn mcp`.
func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "mcp",
		Short:  "stdio→HTTP MCP proxy (connects a stdio MCP client to the running daemon)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			addr, _ := cmd.Flags().GetString("mcp-addr")
			return runMCPProxy(dbPath, addr)
		},
	}
	cmd.Flags().String("mcp-addr", "", "daemon MCP address (default: read from daemon.addrs)")
	return cmd
}

func runMCPProxy(dbPath, addrFlag string) error {
	addr := addrFlag
	if addr == "" {
		if a, err := daemon.ReadAddrs(dbPath); err == nil && a.MCPAddr != "" {
			addr = a.MCPAddr
		} else {
			addr = ":7878"
		}
	}
	target := daemon.MCPURL(addr)
	token := daemon.ReadToken(dbPath)
	client := &http.Client{Timeout: 35 * time.Second}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	var sessionID string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var env struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal([]byte(line), &env)

		req, err := http.NewRequest(http.MethodPost, target, bytes.NewBufferString(line))
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-rag mcp: build request: %v\n", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if sessionID != "" {
			req.Header.Set("Mcp-Session-Id", sessionID)
		}

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-rag mcp: daemon unreachable — is 'go-rag start' running? (%v)\n", err)
			writeProxyError(os.Stdout, -32000, "go-rag daemon unreachable — is 'go-rag start' running?")
			continue
		}
		if env.Method == "initialize" {
			if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
				sessionID = sid
			}
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusAccepted {
			continue // notification acknowledged — no response body
		}
		if resp.StatusCode >= 400 {
			writeProxyError(os.Stdout, -32000, fmt.Sprintf("go-rag daemon HTTP %d", resp.StatusCode))
			continue
		}
		body = bytes.TrimSpace(body)
		if len(body) > 0 {
			fmt.Fprintf(os.Stdout, "%s\n", body)
		}
	}
	return scanner.Err()
}

func writeProxyError(out io.Writer, code int, msg string) {
	resp := map[string]any{
		"jsonrpc": "2.0", "id": nil,
		"error": map[string]any{"code": code, "message": msg},
	}
	b, _ := json.Marshal(resp)
	fmt.Fprintf(out, "%s\n", b)
}
