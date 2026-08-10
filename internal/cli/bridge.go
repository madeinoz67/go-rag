package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/madeinoz67/go-rag/internal/config"
	"github.com/spf13/cobra"
)

// newBridgeCmd is the spec-060 MuninnDB bridge CLI. v1 exposes init (configure +
// enable) and status (config-level). The pause/resume/sync live controls are US2;
// live runtime status is the management console (US3). The `muninn` subgroup leaves
// room for future bridge backends; today MuninnDB is the only one.
func newBridgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bridge",
		Short: "MuninnDB bridge — promote chunks into long-term memory",
	}
	muninn := &cobra.Command{
		Use:   "muninn",
		Short: "the MuninnDB bridge",
	}
	muninn.AddCommand(newBridgeInitCmd(), newBridgeStatusCmd())
	cmd.AddCommand(muninn)
	return cmd
}

// configPath resolves the config.json path from the global dbPath.
func configPath() string { return filepath.Join(dbPath, "config.json") }

// newBridgeInitCmd configures + enables the MuninnDB bridge. The target-vault key
// is NOT a flag — it is read from the GORAG_BRIDGE_TOKEN env at runtime (referenced,
// never inlined in config.json). `init` warns if that env var is unset.
func newBridgeInitCmd() *cobra.Command {
	var endpoint, sourceVault, targetVault string
	var maxInFlight, ratePerSec int
	var disable bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Configure and enable the MuninnDB bridge",
		RunE: func(_ *cobra.Command, _ []string) error {
			path := configPath()
			cfg, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("load config %q: %w", path, err)
			}
			if endpoint != "" {
				cfg.BridgeEndpoint = endpoint
			}
			if sourceVault != "" {
				cfg.BridgeSourceVault = sourceVault
			}
			if targetVault != "" {
				cfg.BridgeTargetVault = targetVault
			}
			if maxInFlight > 0 {
				cfg.BridgeMaxInFlight = maxInFlight
			}
			if ratePerSec > 0 {
				cfg.BridgeRatePerSec = ratePerSec
			}
			cfg.BridgeEnabled = !disable

			// Validate before saving so a non-loopback endpoint or bad value fails
			// loudly here, not at daemon start.
			if err := cfg.Validate(); err != nil {
				return err
			}
			if cfg.BridgeEnabled && os.Getenv("GORAG_BRIDGE_TOKEN") == "" {
				fmt.Fprintln(os.Stderr, "WARN: bridge enabled but GORAG_BRIDGE_TOKEN is unset — the daemon will start the bridge but promotion will fail until the env var is set")
			}
			if err := config.Save(path, cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			printBridgeConfig(cfg)
			return nil
		},
	}
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "MuninnDB gRPC endpoint (loopback only; default 127.0.0.1:8477)")
	cmd.Flags().StringVar(&sourceVault, "source-vault", "", "go-rag vault to bridge (default: default)")
	cmd.Flags().StringVar(&targetVault, "target-vault", "", "dedicated MuninnDB target vault (default: go-rag)")
	cmd.Flags().IntVar(&maxInFlight, "max-in-flight", 0, "max concurrent BatchWrite calls / storm-limit (default 8; 0 = default)")
	cmd.Flags().IntVar(&ratePerSec, "rate-per-sec", 0, "token-bucket promotions/sec cap (0 = unbounded)")
	cmd.Flags().BoolVar(&disable, "disable", false, "disable the bridge (set bridge_enabled=false)")
	return cmd
}

// newBridgeStatusCmd prints the bridge configuration (config-level, not live
// runtime state — live health/backfill progress is the management console's job).
func newBridgeStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the MuninnDB bridge configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			printBridgeConfig(cfg)
			return nil
		},
	}
}

// printBridgeConfig renders the bridge config (the target-vault key is deliberately
// absent — it lives in the env, never the config file or this output).
func printBridgeConfig(cfg config.Config) {
	state := "disabled"
	if cfg.EffectiveBridgeEnabled() {
		state = "enabled"
	}
	token := "unset"
	if os.Getenv("GORAG_BRIDGE_TOKEN") != "" {
		token = "set"
	}
	fmt.Printf("bridge:        %s\n", state)
	fmt.Printf("endpoint:      %s (loopback)\n", cfg.EffectiveBridgeEndpoint())
	fmt.Printf("source vault:  %s\n", cfg.EffectiveBridgeSourceVault())
	fmt.Printf("target vault:  %s\n", cfg.EffectiveBridgeTargetVault())
	fmt.Printf("token (env):   %s\n", token)
	if cfg.EffectiveBridgeEnabled() {
		fmt.Printf("max-in-flight: %d\n", cfg.EffectiveBridgeMaxInFlight())
		fmt.Printf("workers:       %d\n", cfg.EffectiveBridgeWorkers())
		fmt.Printf("batch size:    %d\n", cfg.EffectiveBridgeBatchSize())
		if cfg.BridgeRatePerSec > 0 {
			fmt.Printf("rate cap:      %d/sec\n", cfg.BridgeRatePerSec)
		}
	}
}
