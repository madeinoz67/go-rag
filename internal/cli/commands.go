package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// All commands live in their own files: init.go, add.go, query.go, status.go,
// config_cli.go, scan.go. Only version remains here.

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the go-rag version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(version)
		},
	}
}
