package skills

import (
	"strings"
	"testing"
)

func TestValidateSoft_Clean(t *testing.T) {
	fm := Frontmatter{
		"name":        "my-skill",
		"description": "A useful skill.",
	}
	issues := ValidateSoft(fm, "my-skill")
	if len(issues) > 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestValidateSoft_NameMismatch(t *testing.T) {
	fm := Frontmatter{
		"name":        "alias",
		"description": "Test.",
	}
	issues := ValidateSoft(fm, "real-name")
	if !hasIssue(issues, "name", "does not match directory") {
		t.Errorf("expected name mismatch issue, got %v", issues)
	}
}

func TestValidateSoft_MissingDescription(t *testing.T) {
	fm := Frontmatter{"name": "noop"}
	issues := ValidateSoft(fm, "noop")
	if !hasIssue(issues, "description", "missing description") {
		t.Errorf("expected missing description, got %v", issues)
	}
}

func TestValidateSoft_LongDescription(t *testing.T) {
	long := strings.Repeat("x", 1600)
	fm := Frontmatter{"description": long}
	issues := ValidateSoft(fm, "any")
	if !hasIssue(issues, "description", "truncated") {
		t.Errorf("expected truncation warning, got %v", issues)
	}
}

func TestValidateSoft_BadContext(t *testing.T) {
	fm := Frontmatter{"description": "ok", "context": "weird"}
	issues := ValidateSoft(fm, "any")
	if !hasIssue(issues, "context", "unknown value") {
		t.Errorf("expected context warning, got %v", issues)
	}
}

func TestValidateSoft_BadDirName(t *testing.T) {
	fm := Frontmatter{"description": "ok"}
	issues := ValidateSoft(fm, "Has Spaces")
	if !hasIssue(issues, "name", "lowercase letters") {
		t.Errorf("expected dir-name warning, got %v", issues)
	}
}

func TestValidateStrict_UnknownField(t *testing.T) {
	fm := Frontmatter{
		"name":           "ok",
		"description":    "fine",
		"random-typo":    true,
		"another-extra":  "x",
	}
	issues := ValidateStrict(fm, "ok")
	gotUnknown := 0
	for _, is := range issues {
		if is.Message == "unknown frontmatter field" {
			gotUnknown++
		}
	}
	if gotUnknown != 2 {
		t.Errorf("expected 2 unknown-field issues, got %d (all issues: %v)", gotUnknown, issues)
	}
}

func TestValidateStrict_AllKnownFieldsPass(t *testing.T) {
	fm := Frontmatter{
		"name":                     "all-fields",
		"description":              "Every known field present.",
		"when_to_use":              "always",
		"argument-hint":            "[x]",
		"arguments":                []any{"a", "b"},
		"disable-model-invocation": true,
		"user-invocable":           true,
		"allowed-tools":            "Read",
		"model":                    "inherit",
		"effort":                   "medium",
		"context":                  "fork",
		"agent":                    "Explore",
		"hooks":                    nil,
		"paths":                    []any{"**/*.go"},
		"shell":                    "bash",
	}
	issues := ValidateStrict(fm, "all-fields")
	for _, is := range issues {
		if is.Message == "unknown frontmatter field" {
			t.Errorf("known field flagged: %s", is.Field)
		}
	}
}

func hasIssue(issues []ValidationIssue, field, substr string) bool {
	for _, is := range issues {
		if is.Field == field && strings.Contains(is.Message, substr) {
			return true
		}
	}
	return false
}
