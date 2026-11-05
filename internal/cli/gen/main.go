// Command gen regenerates the committed CLI reference and package-time man pages.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/trieu04/devt-tunnel/internal/cli"
)

func main() {
	reference := flag.String("reference-dir", "docs/cli/reference", "Command reference output directory")
	man := flag.String("man-dir", "bin/man", "Section 1 man-page output directory (not committed)")
	flag.Parse()
	if err := cli.GenerateDocumentation(*reference, *man); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
