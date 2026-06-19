package cli

import (
	"fmt"

	"github.com/madeinoz67/go-rag/internal/daemon"
)

const (
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	reset  = "\033[0m"
)

// dashRow prints one aligned dashboard row: label, value, colored status dot.
func dashRow(label, value, color string) {
	fmt.Printf("    %-10s %-14s  %s●%s\n", label, value, color, reset)
}

// printDashboard renders a muninn-style status panel when go-rag is invoked with
// no subcommand.
func printDashboard() {
	running, pid, addr := daemon.Status(dbPath)

	// Header
	fmt.Println()
	if running {
		fmt.Printf("  go-rag  %s●%s  running\n\n", green, reset)
	} else {
		fmt.Printf("  go-rag  %s○%s  stopped\n\n", red, reset)
	}

	// Services (only when daemon is up)
	if running {
		dashRow("daemon", fmt.Sprintf("pid %d", pid), green)
		dashRow("mcp", addr, green)
	}

	// Database stats
	if running {
		counts, _ := daemon.CallTool(addr, daemon.ReadToken(dbPath), "go_rag_status", nil)
		var docs, chunks, embs int
		fmt.Sscanf(counts, "documents: %d, chunks: %d, embeddings: %d", &docs, &chunks, &embs)
		if docs > 0 {
			pct := embs * 100 / docs
			dashRow("database", fmt.Sprintf("%d docs", docs), green)
			embColor := green
			if pct < 100 {
				embColor = yellow
			}
			dashRow("embedded", fmt.Sprintf("%d%%", pct), embColor)
		}
	} else {
		cfg, db, err := openDB(dbPath)
		if err != nil {
			fmt.Printf("    %-10s not initialized\n\n", "database")
			fmt.Printf("  Run 'go-rag init' to get started.\n")
			fmt.Printf("\n  Type 'go-rag help' for commands.\n\n")
			return
		}
		defer db.Close()
		info := gatherStats(db, cfg)
		info.Health = pingHealth(cfg.OllamaURL)

		dashRow("database", fmt.Sprintf("%d docs", info.Documents), green)
		embColor := green
		if info.EmbeddedPct < 100 {
			embColor = yellow
		}
		dashRow("embedded", fmt.Sprintf("%d%%", info.EmbeddedPct), embColor)
		ollamaColor := green
		if info.Health != "OK" {
			ollamaColor = red
		}
		dashRow("ollama", info.Health, ollamaColor)

		fmt.Printf("\n  Vault:   %s\n", dbPath)
		fmt.Printf("  Model:   %s", cfg.OllamaModel)
		if info.Dimensions > 0 {
			fmt.Printf(" (%d dims)", info.Dimensions)
		}
		fmt.Println()
		fmt.Printf("  Storage: %s\n", humanBytes(info.StorageBytes))
	}

	fmt.Println()
	fmt.Printf("  Type 'go-rag help' for commands.\n")
	fmt.Println()
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
