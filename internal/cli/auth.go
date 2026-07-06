package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/madeinoz67/go-rag/internal/auth"
	"github.com/spf13/cobra"
)

// auth.go is the spec 045 (US1/US3) auth CLI surface. The `auth` parent
// command groups credential management:
//
//	go-rag auth create --label <s> --mode read|write|admin [--expires <dur>]
//	go-rag auth list
//	go-rag auth revoke <id>
//	go-rag auth session list          (spec 045 US3)
//	go-rag auth session revoke <hash> (spec 045 US3)
//
// `auth.bootstrap` is intentionally CLI-only (not MCP) — it needs local FS
// access to seed the first admin. All commands operate on the local vault via
// openDB; no daemon is required.

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage API keys, sessions, and the admin user",
		Long: `Manage go-rag credentials: labelled API keys for programmatic clients and
admin-login sessions for the UI. The raw secret of an API key is shown exactly
once at create time and is never persisted — only its SHA-256 hash is stored.`,
	}
	cmd.AddCommand(newAuthCreateCmd(), newAuthListCmd(), newAuthRevokeCmd())
	return cmd
}

// apiKeyOut is the JSON shape for `auth list --json` and create confirmation.
// It NEVER includes the secret or the storage hash.
type apiKeyOut struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Mode      string     `json:"mode"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Enabled   bool       `json:"enabled"`
}

func toAPIKeyOut(k auth.APIKey) apiKeyOut {
	return apiKeyOut{
		ID: k.ID, Label: k.Label, Mode: k.Mode,
		CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, Enabled: k.Enabled,
	}
}

func newAuthCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key (secret printed once)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			label, _ := cmd.Flags().GetString("label")
			mode, _ := cmd.Flags().GetString("mode")
			expiresDur, _ := cmd.Flags().GetString("expires")
			asJSON, _ := cmd.Flags().GetBool("json")

			var expiresAt *time.Time
			if expiresDur != "" {
				d, err := time.ParseDuration(expiresDur)
				if err != nil {
					return fmt.Errorf("--expires: %w", err)
				}
				t := time.Now().UTC().Add(d)
				expiresAt = &t
			}

			_, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			store := auth.NewStore(db)

			display, key, err := auth.CreateAPIKey(store, label, mode, expiresAt)
			if err != nil {
				return err
			}
			// The secret is shown exactly once. Print to stdout (so it can be
			// captured) and to stderr's attention line.
			if asJSON {
				out := struct {
					Secret string     `json:"secret"`
					Key    apiKeyOut  `json:"key"`
				}{Secret: display, Key: toAPIKeyOut(key)}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			fmt.Println("Created API key — copy the secret now, it will not be shown again:")
			fmt.Println("  " + display)
			fmt.Printf("  id: %s  label: %q  mode: %s\n", key.ID, key.Label, key.Mode)
			return nil
		},
	}
	cmd.Flags().String("label", "", "human-readable label for this key (required)")
	cmd.Flags().String("mode", "read", "scope: read | write | admin")
	cmd.Flags().String("expires", "", "lifetime duration (e.g. 720h); empty = never expires")
	cmd.Flags().Bool("json", false, "emit JSON (includes the secret) for machine capture")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newAuthListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List API keys (no secrets)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			_, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			store := auth.NewStore(db)

			keys, err := auth.ListAPIKeys(store)
			if err != nil {
				return err
			}
			if asJSON {
				out := make([]apiKeyOut, 0, len(keys))
				for _, k := range keys {
					out = append(out, toAPIKeyOut(k))
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}
			printAPIKeyTable(os.Stdout, keys)
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "emit JSON")
	return cmd
}

func newAuthRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Disable an API key by its id (gorag_<id8>)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			store := auth.NewStore(db)

			if err := auth.RevokeAPIKey(store, args[0]); err != nil {
				return err
			}
			fmt.Printf("Revoked %s\n", args[0])
			return nil
		},
	}
	return cmd
}

// printAPIKeyTable renders keys as a column-aligned table. The secret column is
// intentionally absent; "—" marks a non-expiring key.
func printAPIKeyTable(w io.Writer, keys []auth.APIKey) {
	if len(keys) == 0 {
		fmt.Fprintln(w, "(no API keys)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tLABEL\tMODE\tCREATED\tEXPIRES\tENABLED")
	for _, k := range keys {
		expires := "—"
		if k.ExpiresAt != nil {
			expires = k.ExpiresAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%v\n",
			k.ID, k.Label, k.Mode,
			k.CreatedAt.UTC().Format(time.RFC3339),
			expires, k.Enabled)
	}
	tw.Flush()
}
