package cli

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/madeinoz67/go-rag/internal/upgrade"
	"github.com/spf13/cobra"
)

// newUpgradeCmd builds the `go-rag upgrade` command.
//
// It resolves the latest release, verifies the asset against a published
// SHA-256, and atomically replaces the running binary. Schema migration of the
// store is separate (runs on next open) and is not triggered here.
//
// Flags:
//
//	--check   Check only; exit 1 if an update is available (scripting).
//	--yes/-y  Skip the confirmation prompt (non-interactive).
func newUpgradeCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the go-rag binary to the latest release",
		RunE: func(cmd *cobra.Command, _ []string) error {
			checkOnly, _ := cmd.Flags().GetBool("check")
			skipConfirm, _ := cmd.Flags().GetBool("yes")
			rollback, _ := cmd.Flags().GetBool("rollback")

			// Offline rollback short-circuits the version-check / upgrade flow.
			if rollback {
				if err := upgrade.Rollback(); err != nil {
					return err
				}
				fmt.Println("Rolled back to the previous go-rag binary.")
				return nil
			}

			fmt.Printf("Current version: %s\n", version)
			fmt.Print("Checking for updates...")

			latest, err := upgrade.LatestVersion(version)
			if err != nil {
				fmt.Println(" failed")
				return fmt.Errorf("could not reach GitHub: %w", err)
			}
			if latest == "" {
				fmt.Println(" skipped")
				fmt.Println("Dev build — version checks are disabled.")
				return nil
			}
			if !upgrade.NewerVersionAvailable(version, latest) {
				fmt.Println(" done")
				fmt.Printf("You're up to date (%s).\n", version)
				return nil
			}

			fmt.Println(" done")
			fmt.Printf("Update available: %s → %s\n", version, latest)
			fmt.Printf("Release notes → https://github.com/madeinoz67/go-rag/releases/tag/%s\n", latest)

			if checkOnly {
				// Scripting signal: exit non-zero when an update is available.
				os.Exit(1)
			}

			// Windows locks running executables; no in-place self-replace.
			if runtime.GOOS == "windows" {
				fmt.Printf("Windows cannot self-replace a running binary. Download %s from:\n", latest)
				fmt.Printf("  https://github.com/madeinoz67/go-rag/releases/tag/%s\n", latest)
				return nil
			}

			if !skipConfirm {
				fmt.Print("Upgrade now? [y/N] ")
				if !readConfirm() {
					fmt.Println("Upgrade cancelled.")
					return nil
				}
			}

			if upgrade.DaemonRunning() {
				// FR-010: replacing the binary is safe with the daemon running,
				// but the new code only takes effect after a daemon restart.
				fmt.Println("Note: the go-rag daemon is running. Restart it after the upgrade")
				fmt.Println("      (go-rag stop && go-rag start) for the new binary to take effect.")
			}

			if err := upgrade.SelfUpdate(latest); err != nil {
				return err
			}
			fmt.Printf("Upgraded to %s.\n", latest)
			return nil
		},
	}
	cmd.Flags().Bool("check", false, "check only; exit non-zero if an update is available")
	cmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt (non-interactive)")
	cmd.Flags().Bool("rollback", false, "restore the previous binary from <exe>.prev (offline)")
	return cmd
}

// readConfirm reads a y/N confirmation from stdin.
func readConfirm() bool {
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	resp := strings.ToLower(strings.TrimSpace(line))
	return resp == "y" || resp == "yes"
}
