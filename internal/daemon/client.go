package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CallTool invokes one MCP tool on the running daemon over HTTP and returns the
// text content of its result. Used by CLI commands that prefer to route through
// the daemon (avoiding a Pebble-lock conflict with it).
func CallTool(addr, token, tool string, args map[string]any) (string, error) {
	body := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, MCPURL(addr), bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}
	var env struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return "", err
	}
	if env.Error != nil {
		return "", fmt.Errorf("daemon tool error: %v", env.Error)
	}
	if len(env.Result.Content) == 0 {
		return "", nil
	}
	return env.Result.Content[0].Text, nil
}
