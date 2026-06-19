package cli

import (
	"fmt"

	"github.com/madeinoz67/go-rag/internal/daemon"
)

// printDashboard renders a muninn-style status panel when go-rag is invoked with
// no subcommand. It probes the daemon, shows service health with colored dots,
// and summarises the database — a visual at-a-glance, not the full status dump.
func printDashboard() {
	running, pid, addr := daemon.Status(dbPath)

	// Header
	fmt.Println()
	if running {
		fmt.Printf("  go-rag  \033[32m●\033[0m  running\n\n")
		fmt.Printf("    %-10s pid %-6d  \033[32m●\033[0m\n", "daemon", pid)
		fmt.Printf("    %-10s %-10s  \033[32m●\033[0m\n", "mcp", addr)
	} else {
		fmt.Printf("  go-rag  \033[31m○\033[0m  stopped\n\n")
	}

	// Database stats — via the daemon (HTTP) if running, else direct.
	if running {
		counts, _ := daemon.CallTool(addr, daemon.ReadToken(dbPath), "go_rag_status", nil)
		var docs, chunks, embs int
		fmt.Sscanf(counts, "documents: %d, chunks: %d, embeddings: %d", &docs, &chunks, &embs)
		if docs > 0 {
			pct := embs * 100 / docs
			fmt.Printf("    %-10s %d docs     \033[32m●\033[0m\n", "database", docs)
			dot := greenDot
			if pct < 100 {
				dot = "\033[33m●\033[0m" // yellow for partial
			}
			fmt.Printf("    %-10s %d%%         %s\n", "embedded", pct, dot)
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

		fmt.Printf("    %-10s %d docs     \033[32m●\033[0m\n", "database", info.Documents)
		dot := greenDot
		if info.EmbeddedPct < 100 {
			dot = "\033[33m●\033[0m"
		}
		fmt.Printf("    %-10s %d%%         %s\n", "embedded", info.EmbeddedPct, dot)
		hDot := greenDot
		if info.Health == "degraded" {
			hDot = "\033[31m●\033[0m" // red
		}
		fmt.Printf("    %-10s %-10s  %s\n", "ollama", info.Health, hDot)

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

const greenDot = "\033[32m●\033[0m"

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
