package cli

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	clidocs "github.com/trieu04/devt-tunnel/docs/cli"
	"mvdan.cc/sh/v3/syntax"
)

func TestGeneratedDocumentation(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	if err := GenerateDocumentation(filepath.Join(first, "reference"), filepath.Join(first, "man")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1")
	if err := GenerateDocumentation(filepath.Join(second, "reference"), filepath.Join(second, "man")); err != nil {
		t.Fatal(err)
	}
	compareDocumentationDirs(t, filepath.Join(first, "reference"), "../../docs/cli/reference")
	compareDocumentationDirs(t, filepath.Join(first, "reference"), filepath.Join(second, "reference"))
	compareDocumentationDirs(t, filepath.Join(first, "man"), filepath.Join(second, "man"))
}

func compareDocumentationDirs(t *testing.T, generated, checked string) {
	t.Helper()
	want, err := os.ReadDir(generated)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadDir(checked)
	if err != nil {
		t.Fatal(err)
	}
	var wantNames, gotNames []string
	for _, entry := range want {
		wantNames = append(wantNames, entry.Name())
	}
	for _, entry := range got {
		gotNames = append(gotNames, entry.Name())
	}
	if len(want) == 0 || !reflect.DeepEqual(wantNames, gotNames) {
		t.Fatalf("documentation file set differs in %s; run go run ./internal/cli/gen\nwant %v\ngot %v", checked, wantNames, gotNames)
	}
	for _, name := range wantNames {
		want, err := os.ReadFile(filepath.Join(generated, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(checked, name))
		if err != nil {
			t.Fatal(err)
		}
		// A Windows checkout may use CRLF. Generated content is always LF.
		if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))) {
			t.Errorf("%s differs; run go run ./internal/cli/gen", filepath.Join(checked, name))
		}
	}
}

// Called by TestExitCodeTableCoversAPI so the API, CLI and both published tables
// remain a single contract check, including success, duplicate and extra rows.
func checkDocumentedExitCodes(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	_, table, found := strings.Cut(string(data), "| Code | Name | Meaning |\n")
	if !found {
		t.Fatalf("%s has no exit code table", path)
	}
	rows := map[int]string{}
	for _, line := range strings.Split(table, "\n")[1:] { // skip Markdown separator
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		if len(cells) != 5 {
			t.Fatalf("%s: malformed exit code row %q", path, line)
		}
		code, err := strconv.Atoi(strings.TrimSpace(cells[1]))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if _, duplicate := rows[code]; duplicate {
			t.Errorf("%s duplicates exit code %d", path, code)
		}
		rows[code] = strings.Trim(strings.TrimSpace(cells[2]), "`")
	}
	if len(rows) != len(ExitCodes) {
		t.Errorf("%s has %d exit rows, want %d", path, len(rows), len(ExitCodes))
	}
	for _, row := range ExitCodes {
		if name, ok := rows[row.Code]; !ok || name != row.Name {
			t.Errorf("%s: exit %d = %q (present %t), want %q", path, row.Code, name, ok, row.Name)
		}
	}
}

var inlineCode = regexp.MustCompile("`([^`]+)`")

func TestGuideExamples(t *testing.T) {
	files, err := fs.Glob(clidocs.Guides, "*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("guides: %v %v", files, err)
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			guide, err := clidocs.Guides.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			var block strings.Builder
			var inFence, shell bool
			checked := 0
			for _, line := range strings.Split(string(guide), "\n") {
				if strings.HasPrefix(line, "```") {
					if inFence && shell {
						checked += checkShellExamples(t, block.String())
					}
					inFence = !inFence
					language := strings.TrimSpace(strings.TrimPrefix(line, "```"))
					shell = strings.HasPrefix(language, "bash") || strings.HasPrefix(language, "sh")
					if strings.Contains(language, "phase19") {
						t.Error("obsolete phase19 fence: all commands are implemented")
					}
					block.Reset()
					continue
				}
				if inFence {
					if shell {
						block.WriteString(line + "\n")
					}
					continue
				}
				// Table cells contain executable replacements/fixes. Inline prose
				// such as "use `dtt secret set --stdin`" is a command mention,
				// not a complete invocation, and is not treated as an example.
				if strings.Contains(line, "<!-- phase19 -->") {
					t.Error("obsolete phase19 row marker")
				}
				if strings.HasPrefix(line, "|") {
					for _, match := range inlineCode.FindAllStringSubmatch(line, -1) {
						if strings.HasPrefix(match[1], "dtt ") {
							checked += checkShellExamples(t, match[1])
						}
					}
				}
			}
			if inFence || checked == 0 {
				t.Fatalf("unterminated fence or no checked examples (%d)", checked)
			}
		})
	}
}

func checkShellExamples(t *testing.T, source string) int {
	t.Helper()
	program, err := syntax.NewParser().Parse(strings.NewReader(source), "guide")
	if err != nil {
		t.Fatalf("invalid shell example %q: %v", source, err)
	}
	checked := 0
	syntax.Walk(program, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 || exampleWord(call.Args[0].Parts) != "dtt" {
			return true
		}
		args := make([]string, 0, len(call.Args)-1)
		for _, word := range call.Args[1:] {
			args = append(args, exampleWord(word.Parts))
		}

		if err := parseExample(args); err != nil {
			t.Errorf("dtt %s: %v", strings.Join(args, " "), err)
		}
		checked++
		return true
	})
	return checked
}

func exampleWord(parts []syntax.WordPart) string {
	var word strings.Builder
	for _, part := range parts {
		switch part := part.(type) {
		case *syntax.Lit:
			word.WriteString(part.Value)
		case *syntax.SglQuoted:
			word.WriteString(part.Value)
		case *syntax.DblQuoted:
			word.WriteString(exampleWord(part.Parts))
		default:
			// Never expand parameters, substitutions or globs. A placeholder
			// preserves their argument slot; Walk separately checks nested dtt.
			word.WriteString("example")
		}
	}
	return word.String()
}

func parseExample(args []string) error {
	root := newRoot(newApp(IO{}, Options{}))
	root.InitDefaultHelpCmd()
	cmd, rest, err := root.Find(args)
	if err != nil {
		return err
	}
	cmd.InitDefaultHelpFlag()
	if err := cmd.ParseFlags(rest); err != nil {
		return err
	}
	if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
		return err
	}
	return cmd.ValidateRequiredFlags()
}

func TestGuideParserRejectsDriftWithoutExecuting(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"ls", "--removed-flag"}, {"secret", "ls", "extra"}, {"profile", "removed"}, {"run"}} {
		if err := parseExample(args); err == nil {
			t.Errorf("accepted invalid example %v", args)
		}
	}
	for _, source := range []string{
		`dtt start db && psql -p "$(dtt addr db --port)"`,
		`op read op://dev/token | dtt secret set token --kind env --stdin`,
		`dtt show 'Production DB' --json > output.json`,
	} {
		if checked := checkShellExamples(t, source); checked == 0 {
			t.Errorf("missed example %q", source)
		}
	}
	if checked := checkShellExamples(t, "dtt forward staging auto:5432\ndtt addr db --port"); checked != 2 {
		t.Fatalf("one-off examples were not all checked: checked %d", checked)
	}
}

func TestOfflineDocumentationCommands(t *testing.T) {
	t.Setenv("DTT_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	for _, tc := range []struct {
		args []string
		code int
		text string
	}{
		{[]string{"docs"}, 0, "quickstart"},
		{[]string{"docs", "quickstart"}, 0, "# Quick start"},
		{[]string{"docs", "agents"}, 0, "# Agent guide"},
		{[]string{"docs", "troubleshooting", "--json"}, 0, `"apiVersion":2`},
		{[]string{"docs", "missing"}, 2, "unknown documentation topic"},
		{[]string{"docs", "../../README"}, 2, "unknown documentation topic"},
		{[]string{"docs", "quickstart", "extra"}, 2, "accepts at most 1"},
	} {
		var out, diagnostics bytes.Buffer
		if code := Run(context.Background(), tc.args, IO{Out: &out, Err: &diagnostics}, Options{}); code != tc.code || !strings.Contains(out.String()+diagnostics.String(), tc.text) {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", tc.args, code, &out, &diagnostics)
		}
	}
	dir := filepath.Join(t.TempDir(), "man")
	var out, diagnostics bytes.Buffer
	if code := Run(context.Background(), []string{"man", "--dir", dir}, IO{Out: &out, Err: &diagnostics}, Options{}); code != 0 {
		t.Fatalf("man: code=%d stdout=%s stderr=%s", code, &out, &diagnostics)
	}
	for _, name := range []string{"dtt.1", "dtt-docs.1", "dtt-man.1", "dtt-completion.1", "dtt-group-assign.1"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(data) == 0 || bytes.Contains(data, []byte("Auto generated")) {
			t.Errorf("%s: err=%v content=%s", name, err, data)
		}
	}
}
