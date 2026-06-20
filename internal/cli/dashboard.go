package cli

import (
	"fmt"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/madeinoz67/go-rag/internal/daemon"
	"github.com/madeinoz67/go-rag/internal/vault"
)

const (
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	grey   = "\033[37m"
	reset  = "\033[0m"
)

// dashRow prints one aligned dashboard row: label, value, colored status dot.
func dashRow(label, value, color string) {
	fmt.Printf("    %-10s %-14s  %s●%s\n", label, value, color, reset)
}

// dashRowOff prints a row with a hollow dot (service disabled/off).
func dashRowOff(label, value string) {
	fmt.Printf("    %-10s %-14s  %s○%s\n", label, value, grey, reset)
}

// printDashboard renders a status panel when go-rag is invoked with no subcommand.
// When --vault is set, shows that vault's detail. When no vault, shows all vaults.
func printDashboard() {
	if vaultName != "" {
		printVaultDetail()
		return
	}
	printVaultsOverview()
}

// printVaultsOverview lists all vaults with doc counts, daemon status, and storage.
func printVaultsOverview() {
	vault.EnsureDefault()
	names := vault.List()

	// Services section
	anyRunning := false
	var runningPid int
	var runningAddr string
	for _, n := range names {
		if r, pid, addr := daemon.Status(vault.Path(n)); r {
			anyRunning = true
			runningPid = pid
			runningAddr = addr
			break
		}
	}
	ollamaHealth := pingHealth("http://localhost:11434")
	// Read MCP port from first vault's config (or default :7878)
	mcpAddr := ":7878"
	if len(names) > 0 {
		if cfg, err := config.Load(filepath.Join(vault.Path(names[0]), "config.json")); err == nil && cfg.MCPAddr != "" {
			mcpAddr = cfg.MCPAddr
		}
	}

	fmt.Println()
	if anyRunning {
		fmt.Printf("  go-rag  %s●%s  running\n\n", green, reset)
		dashRow("daemon", fmt.Sprintf("pid %d", runningPid), green)
		dashRow("mcp", runningAddr, green)
	} else {
		fmt.Printf("  go-rag  %s○%s  stopped\n\n", red, reset)
		dashRowOff("mcp", mcpAddr)
	}
	if ollamaHealth == "OK" {
		dashRow("ollama", ollamaHealth, green)
	} else {
		dashRow("ollama", ollamaHealth, red)
	}

	// Vaults section
	fmt.Printf("\n  Vaults (%d):\n\n", len(names))

	var totalDocs int
	for _, n := range names {
		vp := vault.Path(n)
		cfg, _ := config.Load(filepath.Join(vp, "config.json"))
		docs := 0
		var storage int64
		if _, db, err := openDB(vp); err == nil {
			docs = countPrefix(db, 0x02)
			storage = dirSize(filepath.Join(vp, "data"))
			db.Close()
		}
		totalDocs += docs
		running, _, _ := daemon.Status(vp)
		dot := green + "●" + reset
		if !running {
			dot = red + "○" + reset
		}
		model := cfg.EmbeddingModel
		if model == "" {
			model = "-"
		}
		fmt.Printf("    %-16s %6d docs  %-20s %s  %s\n", n, docs, model, dot, humanBytes(storage))
	}

	fmt.Printf("\n  Root:    %s\n", vault.Root())
	fmt.Printf("  Total:   %d docs across %d vault%s\n", totalDocs, len(names), plural(len(names)))
	fmt.Printf("\n  Type 'go-rag --vault <name>' for vault detail.\n")
	fmt.Printf("  Type 'go-rag help' for commands.\n\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// printVaultDetail shows the detailed dashboard for a single vault.
func printVaultDetail() {
	running, pid, addr := daemon.Status(dbPath)
	cfg, _ := config.Load(filepath.Join(dbPath, "config.json"))

	// Header
	fmt.Println()
	vaultLabel := ""
	if vaultName != "" {
		vaultLabel = " — vault: " + vaultName
	}
	if running {
		fmt.Printf("  go-rag%s  %s●%s  running\n\n", vaultLabel, green, reset)
	} else {
		fmt.Printf("  go-rag%s  %s○%s  stopped\n\n", vaultLabel, red, reset)
	}

	// Services (only when daemon is up)
	if running {
		dashRow("daemon", fmt.Sprintf("pid %d", pid), green)
		dashRow("mcp", addr, green)
	}

	// Reranker row (shown in both states)
	if cfg.RerankModel != "" {
		dashRow("reranker", cfg.RerankModel, green)
	} else {
		dashRowOff("reranker", "disabled")
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
		_, db, err := openDB(dbPath)
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
		fmt.Printf("  Model:   %s", cfg.EmbeddingModel)
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
