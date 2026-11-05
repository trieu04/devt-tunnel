package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
)

func encodeJSON(out io.Writer, fields map[string]any) error {
	result := make(map[string]any, len(fields)+1)
	for key, value := range fields {
		result[key] = value
	}
	result["apiVersion"] = api.Version
	return json.NewEncoder(out).Encode(result)
}

func (a *app) writeJSON(fields map[string]any) error { return encodeJSON(a.io.Out, fields) }

func (a *app) table(header []string, rows [][]string) error {
	w := tabwriter.NewWriter(a.io.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, strings.Join(header, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(w, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return w.Flush()
}

func (a *app) diagnostic(format string, args ...any) {
	if !a.quiet {
		fmt.Fprintf(a.io.Err, format+"\n", args...)
	}
}

func (a *app) writeFindings(findings []checks.Finding) error {
	for _, finding := range findings {
		severity := string(finding.Severity)
		switch finding.Severity {
		case checks.Error:
			severity = a.color("31", severity)
		case checks.Warning:
			severity = a.color("33", severity)
		case checks.OK:
			severity = a.color("32", severity)
		}
		if _, err := fmt.Fprintf(a.io.Out, "%s: %s\n", severity, finding.Message); err != nil {
			return err
		}
	}
	return nil
}
