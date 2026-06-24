package cli

import (
	"fmt"
	"time"

	"github.com/madeinoz67/go-rag/internal/audit"
	"github.com/spf13/cobra"
)

// newAuditCmd is the H18/spec 021 audit-log reader: tail + filter by type/time,
// optionally including rotated archives. Read-only (opens the config for the log
// path; never writes).
func newAuditCmd() *cobra.Command {
	var (
		tailN  int
		typ    string
		since  string
		all    bool
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Read the structured audit log (query/ingest/auth-fail events)",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, db, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			path := cfg.AuditPath
			if path == "" {
				path = audit.DefaultPath(cfg.DBPath)
			}
			opts := audit.ReadOptions{Type: typ, All: all, Tail: tailN}
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				opts.Since = d
			}
			events, err := audit.Read(path, opts)
			if err != nil {
				return err
			}
			if len(events) == 0 {
				fmt.Println("No audit events.")
				return nil
			}
			if asJSON {
				fmt.Print(audit.RenderJSONL(events))
			} else {
				fmt.Print(audit.RenderText(events))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&tailN, "tail", 20, "show the last N events (0 = all after filter)")
	cmd.Flags().StringVar(&typ, "type", "", "filter by event type: query|ingest|auth-fail")
	cmd.Flags().StringVar(&since, "since", "", "only events within this duration (e.g. 1h, 30m)")
	cmd.Flags().BoolVar(&all, "all", false, "include rotated archives (audit-N.log)")
	cmd.Flags().BoolVarP(&asJSON, "format", "f", false, "raw JSONL output (pipe to jq)")
	return cmd
}
