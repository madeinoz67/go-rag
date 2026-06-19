// Package main is the go-rag binary entrypoint (PRD §1, §5).
//
// go-rag is a single-binary local RAG database. This is the one and only main
// package; all real logic lives under internal/.
package main

import (
	"fmt"
	"os"

	"github.com/madeinoz67/go-rag/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=..." (see Makefile).
var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, "go-rag:", err)
		os.Exit(1)
	}
}
