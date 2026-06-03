package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse_NoFrontmatter(t *testing.T) {
	fm, body, err := Parse([]byte("# Heading\n\nplain markdown"))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter, got %v", fm)
	}
	if body != "# Heading\n\nplain markdown" {
		t.Errorf("body mismatch: %q", body)
	}
}

func TestParse_Minimal(t *testing.T) {
	in := "---\nname: test\ndescription: A test skill.\n---\nBody content.\n"
	fm, body, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if fm.String("name") != "test" {
		t.Errorf("name: got %q", fm.String("name"))
	}
	if fm.String("description") != "A test skill." {
		t.Errorf("description: got %q", fm.String("description"))
	}
	if body != "Body content.\n" {
		t.Errorf("body: got %q", body)
	}
}

func TestParse_AllFieldTypes(t *testing.T) {
	in := `---
name: complex
description: All field types in one frontmatter.
disable-model-invocation: true
user-invocable: false
allowed-tools: Read Grep
arguments:
  - first
  - second
paths:
  - "src/**/*.go"
context: fork
agent: Explore
---
content
`
	fm, _, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if !fm.Bool("disable-model-invocation") {
		t.Error("expected disable-model-invocation=true")
	}
	if fm.Bool("user-invocable") {
		t.Error("expected user-invocable=false")
	}
	if got := fm.StringList("allowed-tools"); !reflect.DeepEqual(got, []string{"Read", "Grep"}) {
		t.Errorf("allowed-tools: got %v", got)
	}
	if got := fm.StringList("arguments"); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Errorf("arguments: got %v", got)
	}
	if got := fm.StringList("paths"); !reflect.DeepEqual(got, []string{"src/**/*.go"}) {
		t.Errorf("paths: got %v", got)
	}
	if fm.String("context") != "fork" {
		t.Errorf("context: got %q", fm.String("context"))
	}
}

func TestParse_UnclosedFrontmatter(t *testing.T) {
	in := "---\nname: broken\nno closing delimiter\n"
	fm, body, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse should not error on unclosed: %v", err)
	}
	if fm != nil {
		t.Errorf("expected nil frontmatter for unclosed, got %v", fm)
	}
	if body != in {
		t.Errorf("expected raw body, got %q", body)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	in := "---\nname: test\n  bad: indentation: here:\n---\nbody\n"
	_, _, err := Parse([]byte(in))
	if err == nil {
		t.Error("expected error on invalid YAML")
	}
}

func TestMarshal_RoundTrip(t *testing.T) {
	original := "---\nname: rt\ndescription: Round trip test.\n---\n## Header\n\nbody\n"
	fm, body, err := Parse([]byte(original))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := Marshal(fm, body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	fm2, body2, err := Parse(out)
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}
	if fm2.String("name") != "rt" || fm2.String("description") != "Round trip test." {
		t.Errorf("fields lost on round trip: %v", fm2)
	}
	if body2 != body {
		t.Errorf("body changed on round trip:\norig: %q\nnew:  %q", body, body2)
	}
}

func TestMarshal_EmptyFrontmatter(t *testing.T) {
	out, err := Marshal(nil, "just body")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(out) != "just body" {
		t.Errorf("expected raw body, got %q", out)
	}
}

func TestParse_RealExistingSkills(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("UserHomeDir: %v", err)
	}
	root := filepath.Join(home, ".claude", "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("no real skills dir: %v", err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fm, body, err := Parse(data)
		if err != nil {
			t.Errorf("Parse %s: %v", path, err)
			continue
		}
		if fm == nil {
			t.Errorf("%s: frontmatter unexpectedly nil", path)
			continue
		}
		if fm.String("description") == "" && fm.String("name") == "" {
			t.Errorf("%s: both name and description missing", path)
		}
		if strings.TrimSpace(body) == "" {
			t.Errorf("%s: body empty after Parse", path)
		}

		out, err := Marshal(fm, body)
		if err != nil {
			t.Errorf("Marshal %s: %v", path, err)
			continue
		}
		fm2, _, err := Parse(out)
		if err != nil {
			t.Errorf("re-Parse %s: %v", path, err)
			continue
		}
		if fm2.String("name") != fm.String("name") || fm2.String("description") != fm.String("description") {
			t.Errorf("%s: round-trip lost fields", path)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("no SKILL.md files found to test against")
	}
	t.Logf("verified %d real SKILL.md files", checked)
}
