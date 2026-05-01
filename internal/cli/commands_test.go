package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stacktower-io/stacktower/pkg/buildinfo"
)

// newTestCLI creates a CLI instance for command tests with quiet output.
func newTestCLI(t *testing.T) *CLI {
	t.Helper()
	resetCLIForTesting()
	c, err := New(os.Stderr, LogInfo)
	if err != nil {
		t.Fatalf("cli.New: %v", err)
	}
	c.SetQuiet(true)
	return c
}

// writeTestGraph writes a minimal graph JSON file and returns its path.
func writeTestGraph(t *testing.T, dir string) string {
	t.Helper()
	graph := `{
		"nodes": [
			{"id": "app", "meta": {"version": "1.0.0"}},
			{"id": "lib-a", "meta": {"version": "2.0.0"}},
			{"id": "lib-b", "meta": {"version": "1.5.0"}}
		],
		"edges": [
			{"from": "app", "to": "lib-a"},
			{"from": "app", "to": "lib-b"},
			{"from": "lib-a", "to": "lib-b"}
		]
	}`
	path := filepath.Join(dir, "test-graph.json")
	if err := os.WriteFile(path, []byte(graph), 0o644); err != nil {
		t.Fatalf("write test graph: %v", err)
	}
	return path
}

func TestWhyCommand_TextOutput(t *testing.T) {
	c := newTestCLI(t)
	dir := t.TempDir()
	graphPath := writeTestGraph(t, dir)
	outPath := filepath.Join(dir, "why-output.txt")

	root := c.RootCommand()
	root.SetArgs([]string{"why", graphPath, "lib-b", "-o", outPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("why command failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "lib-b") {
		t.Errorf("why output should mention lib-b, got: %s", output)
	}
}

func TestWhyCommand_JSONOutput(t *testing.T) {
	c := newTestCLI(t)
	dir := t.TempDir()
	graphPath := writeTestGraph(t, dir)
	outPath := filepath.Join(dir, "why-output.json")

	root := c.RootCommand()
	root.SetArgs([]string{"why", graphPath, "lib-b", "-f", "json", "-o", outPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("why command failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	var result struct {
		Results []struct {
			Target string     `json:"target"`
			Paths  [][]string `json:"paths"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Target != "lib-b" {
		t.Errorf("target = %q, want lib-b", result.Results[0].Target)
	}
	if len(result.Results[0].Paths) == 0 {
		t.Error("expected at least one path")
	}
}

func TestWhyCommand_PackageNotFound(t *testing.T) {
	c := newTestCLI(t)
	dir := t.TempDir()
	graphPath := writeTestGraph(t, dir)

	root := c.RootCommand()
	root.SetArgs([]string{"why", graphPath, "nonexistent-pkg"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for missing package")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestStatsCommand_TextOutput(t *testing.T) {
	c := newTestCLI(t)
	dir := t.TempDir()
	graphPath := writeTestGraph(t, dir)
	outPath := filepath.Join(dir, "stats-output.txt")

	root := c.RootCommand()
	root.SetArgs([]string{"stats", graphPath, "-o", outPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("stats command failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) == 0 {
		t.Error("stats output should not be empty")
	}
}

func TestStatsCommand_JSONOutput(t *testing.T) {
	c := newTestCLI(t)
	dir := t.TempDir()
	graphPath := writeTestGraph(t, dir)
	outPath := filepath.Join(dir, "stats-output.json")

	root := c.RootCommand()
	root.SetArgs([]string{"stats", graphPath, "-f", "json", "-o", outPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("stats command failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	var stats statsJSON
	if err := json.Unmarshal(data, &stats); err != nil {
		t.Fatalf("parse stats JSON: %v", err)
	}
	if stats.Root != "app" {
		t.Errorf("root = %q, want app", stats.Root)
	}
	if stats.Overview.TotalPackages != 3 {
		t.Errorf("total_packages = %d, want 3", stats.Overview.TotalPackages)
	}
}

func TestDiffCommand_TextOutput(t *testing.T) {
	c := newTestCLI(t)
	dir := t.TempDir()

	before := `{
		"nodes": [
			{"id": "app", "meta": {"version": "1.0"}},
			{"id": "dep-a", "meta": {"version": "1.0"}}
		],
		"edges": [{"from": "app", "to": "dep-a"}]
	}`
	after := `{
		"nodes": [
			{"id": "app", "meta": {"version": "1.0"}},
			{"id": "dep-a", "meta": {"version": "2.0"}},
			{"id": "dep-b", "meta": {"version": "1.0"}}
		],
		"edges": [{"from": "app", "to": "dep-a"}, {"from": "app", "to": "dep-b"}]
	}`

	beforePath := filepath.Join(dir, "before.json")
	afterPath := filepath.Join(dir, "after.json")
	outPath := filepath.Join(dir, "diff-output.txt")

	os.WriteFile(beforePath, []byte(before), 0o644)
	os.WriteFile(afterPath, []byte(after), 0o644)

	root := c.RootCommand()
	root.SetArgs([]string{"diff", beforePath, afterPath, "-o", outPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("diff command failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "dep-b") {
		t.Errorf("diff output should mention added dep-b, got: %s", output)
	}
}

func TestDiffCommand_FailOnVuln(t *testing.T) {
	c := newTestCLI(t)
	dir := t.TempDir()

	before := `{
		"nodes": [
			{"id": "app", "meta": {"version": "1.0"}},
			{"id": "dep-a", "meta": {"version": "1.0"}}
		],
		"edges": [{"from": "app", "to": "dep-a"}]
	}`
	after := `{
		"nodes": [
			{"id": "app", "meta": {"version": "1.0"}},
			{"id": "dep-a", "meta": {"version": "2.0", "vuln_severity": "high"}}
		],
		"edges": [{"from": "app", "to": "dep-a"}]
	}`

	beforePath := filepath.Join(dir, "before.json")
	afterPath := filepath.Join(dir, "after.json")

	os.WriteFile(beforePath, []byte(before), 0o644)
	os.WriteFile(afterPath, []byte(after), 0o644)

	root := c.RootCommand()
	root.SetArgs([]string{"diff", beforePath, afterPath, "--fail-on-vuln"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected VulnError from --fail-on-vuln")
	}

	exitCode := ExitCodeForError(err)
	if exitCode != ExitCodeVuln {
		t.Errorf("exit code = %d, want %d (ExitCodeVuln)", exitCode, ExitCodeVuln)
	}
}

func TestDiffCommand_JSONOutput(t *testing.T) {
	c := newTestCLI(t)
	dir := t.TempDir()

	before := `{
		"nodes": [{"id": "app", "meta": {"version": "1.0"}}],
		"edges": []
	}`
	after := `{
		"nodes": [
			{"id": "app", "meta": {"version": "1.0"}},
			{"id": "dep-new", "meta": {"version": "1.0"}}
		],
		"edges": [{"from": "app", "to": "dep-new"}]
	}`

	beforePath := filepath.Join(dir, "before.json")
	afterPath := filepath.Join(dir, "after.json")
	outPath := filepath.Join(dir, "diff.json")

	os.WriteFile(beforePath, []byte(before), 0o644)
	os.WriteFile(afterPath, []byte(after), 0o644)

	root := c.RootCommand()
	root.SetArgs([]string{"diff", beforePath, afterPath, "-f", "json", "-o", outPath})

	if err := root.Execute(); err != nil {
		t.Fatalf("diff -f json command failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse diff JSON: %v", err)
	}
	for _, key := range []string{"before", "after", "added", "removed", "updated", "unchanged", "new_vulns"} {
		if _, ok := result[key]; !ok {
			t.Errorf("missing key %q in diff JSON output", key)
		}
	}
}

func TestDiffCommand_RejectsBothInputsFromStdin(t *testing.T) {
	c := newTestCLI(t)

	root := c.RootCommand()
	root.SetArgs([]string{"diff", "-", "-"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for two stdin inputs")
	}
	if got := ExitCodeForError(err); got != ExitCodeUsage {
		t.Fatalf("exit code = %d, want %d", got, ExitCodeUsage)
	}
}

func TestRenderCommand_InvalidFormatIsUsageError(t *testing.T) {
	c := newTestCLI(t)

	root := c.RootCommand()
	root.SetArgs([]string{"render", "graph.json", "-f", "bogus"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected invalid format error")
	}
	if got := ExitCodeForError(err); got != ExitCodeUsage {
		t.Fatalf("exit code = %d, want %d", got, ExitCodeUsage)
	}
}

func TestCacheCommand_Path(t *testing.T) {
	c := newTestCLI(t)

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := c.RootCommand()
	root.SetArgs([]string{"cache", "path"})

	err := root.Execute()
	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("cache path failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := strings.TrimSpace(buf.String())
	if output == "" {
		t.Error("cache path should print a non-empty path")
	}
	if !strings.Contains(output, "stacktower") {
		t.Errorf("cache path should contain 'stacktower', got: %s", output)
	}
}

func TestInfoCommand(t *testing.T) {
	c := newTestCLI(t)
	root := c.RootCommand()
	root.SetArgs([]string{"info"})

	// info writes to stderr and should not error
	if err := root.Execute(); err != nil {
		t.Fatalf("info command failed: %v", err)
	}
}

func TestInfoCommand_JSONIncludesCompiledGitHubAppSlug(t *testing.T) {
	c := newTestCLI(t)
	root := c.RootCommand()
	root.SetArgs([]string{"info", "-f", "json"})

	originalSlug := buildinfo.CompiledGitHubAppSlug
	buildinfo.CompiledGitHubAppSlug = "stacktower-io-test"
	t.Cleanup(func() {
		buildinfo.CompiledGitHubAppSlug = originalSlug
	})

	// Capture stdout where JSON output is written.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	if err := root.Execute(); err != nil {
		w.Close()
		t.Fatalf("info -f json failed: %v", err)
	}
	w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read info json output: %v", err)
	}

	var got infoOutput
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("parse info json: %v", err)
	}
	if got.CompiledGitHubAppSlug != "stacktower-io-test" {
		t.Fatalf("compiled_github_app_slug = %q, want %q", got.CompiledGitHubAppSlug, "stacktower-io-test")
	}
	if got.CompiledGitHubAppURL != "https://github.com/apps/stacktower-io-test" {
		t.Fatalf("compiled_github_app_url = %q, want %q", got.CompiledGitHubAppURL, "https://github.com/apps/stacktower-io-test")
	}
	if got.GitHubRepoURL != "https://github.com/stacktower-io/stacktower" {
		t.Fatalf("github_repo_url = %q, want %q", got.GitHubRepoURL, "https://github.com/stacktower-io/stacktower")
	}
}
