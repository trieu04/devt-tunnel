package cli

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	clidocs "github.com/trieu04/devt-tunnel/docs/cli"
)

func init() {
	register(docsCommand)
	register(manCommand)
}

func guideTopics() []string {
	entries, _ := fs.Glob(clidocs.Guides, "*.md")
	topics := make([]string, 0, len(entries))
	for _, name := range entries {
		topics = append(topics, strings.TrimSuffix(name, ".md"))
	}
	return topics
}

func docsCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "docs [topic]", Short: "Read embedded offline guides",
		Long:    "List available documentation topics, or print a guide as Markdown. Guides are embedded in the binary and never need the network or daemon. Use dtt help for command flags and dtt man to generate a complete manual.",
		Example: "  dtt docs\n  dtt docs quickstart\n  dtt docs troubleshooting", Args: cobra.MaximumNArgs(1),
		ValidArgs: guideTopics(),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				topics := guideTopics()
				if a.json {
					return a.writeJSON(map[string]any{"topics": topics})
				}
				rows := make([][]string, 0, len(topics))
				for _, topic := range topics {
					guide, _ := clidocs.Guides.ReadFile(topic + ".md")
					title, _, _ := strings.Cut(string(guide), "\n")
					rows = append(rows, []string{topic, strings.TrimPrefix(title, "# ")})
				}
				return a.table([]string{"TOPIC", "GUIDE"}, rows)
			}
			// Validate the topic against the embedded index, not a filesystem path.
			for _, topic := range guideTopics() {
				if args[0] != topic {
					continue
				}
				guide, err := clidocs.Guides.ReadFile(topic + ".md")
				if err != nil {
					return err
				}
				if a.json {
					return a.writeJSON(map[string]any{"topic": topic, "markdown": string(guide)})
				}
				_, err = a.io.Out.Write(guide)
				return err
			}
			return usageError("unknown documentation topic %q; use dtt docs to list topics", args[0])
		},
	}
}

func manCommand(a *app) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use: "man", Short: "Write man pages for every command",
		Long:    "Generate section 1 man pages in --dir (the current directory by default), creating the directory if necessary. Existing pages with the same names are overwritten. View the root page with man ./dtt.1, or install the pages into a man1 directory. No daemon or network is needed.",
		Example: "  dtt man --dir ./man\n  man ./man/dtt.1", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := generateManPages(cmd.Root(), dir); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"directory": dir, "section": 1})
			}
			a.diagnostic("Man pages written to %s; view with man %s/dtt.1", dir, dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "Directory for generated section 1 man pages")
	return cmd
}

// GenerateDocumentation writes deterministic reference Markdown and man pages.
// The generator and CI use the same command definitions as the running CLI.
func GenerateDocumentation(referenceDir, manDir string) error {
	root := newRoot(newApp(IO{}, Options{}))
	prepareDocumentation(root)
	if err := os.MkdirAll(referenceDir, 0o755); err != nil {
		return err
	}
	if err := doc.GenMarkdownTree(root, referenceDir); err != nil {
		return fmt.Errorf("generate command reference: %w", err)
	}
	return generateManPages(root, manDir)
}

func prepareDocumentation(root *cobra.Command) {
	root.InitDefaultHelpCmd()
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.DisableAutoGenTag = true
		cmd.InitDefaultHelpFlag()
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func generateManPages(root *cobra.Command, dir string) error {
	prepareDocumentation(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Cobra otherwise inserts the current month in .TH even without its history
	// tag. Pin it to the interface contract date, independent of TZ and the clock.
	date := time.Date(2026, time.November, 4, 0, 0, 0, 0, time.UTC)
	return doc.GenManTree(root, &doc.GenManHeader{Title: "DTT", Section: "1", Source: "dtt", Manual: "dtt manual", Date: &date}, dir)
}
