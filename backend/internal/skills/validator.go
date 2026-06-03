package skills

import (
	"fmt"
	"regexp"
	"strings"
)

// knownFields lists every frontmatter key recognized by Claude Code.
// Used by ValidateStrict to reject typos.
var knownFields = map[string]bool{
	"name":                     true,
	"description":              true,
	"when_to_use":              true,
	"argument-hint":            true,
	"arguments":                true,
	"disable-model-invocation": true,
	"user-invocable":           true,
	"allowed-tools":            true,
	"model":                    true,
	"effort":                   true,
	"context":                  true,
	"agent":                    true,
	"hooks":                    true,
	"paths":                    true,
	"shell":                    true,
}

// nameRegex enforces the Claude Code naming rule: lowercase letters, digits,
// hyphens, max 64 characters.
var nameRegex = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// ValidationIssue is a single problem found by Validate.
type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (i ValidationIssue) String() string {
	if i.Field == "" {
		return i.Message
	}
	return fmt.Sprintf("%s: %s", i.Field, i.Message)
}

// ValidateSoft returns warnings useful for the editor UI. It never returns an
// error, only a list of issues. Empty result means the skill looks fine.
func ValidateSoft(fm Frontmatter, name string) []ValidationIssue {
	var issues []ValidationIssue

	declared := fm.String("name")
	if declared != "" && declared != name {
		issues = append(issues, ValidationIssue{
			Field:   "name",
			Message: fmt.Sprintf("declared name %q does not match directory name %q", declared, name),
		})
	}
	if declared != "" && !nameRegex.MatchString(declared) {
		issues = append(issues, ValidationIssue{
			Field:   "name",
			Message: "must be lowercase letters, digits and hyphens only (max 64 chars)",
		})
	}
	if !nameRegex.MatchString(name) {
		issues = append(issues, ValidationIssue{
			Field:   "name",
			Message: fmt.Sprintf("directory %q must be lowercase letters, digits and hyphens only (max 64 chars)", name),
		})
	}

	desc := strings.TrimSpace(fm.String("description"))
	if desc == "" {
		issues = append(issues, ValidationIssue{
			Field:   "description",
			Message: "missing description (recommended so Claude knows when to use the skill)",
		})
	}
	if len(desc) > 1536 {
		issues = append(issues, ValidationIssue{
			Field:   "description",
			Message: fmt.Sprintf("description is %d chars, will be truncated at 1536", len(desc)),
		})
	}

	if ctx := fm.String("context"); ctx != "" && ctx != "fork" {
		issues = append(issues, ValidationIssue{
			Field:   "context",
			Message: fmt.Sprintf("unknown value %q (only \"fork\" is supported)", ctx),
		})
	}
	if shell := fm.String("shell"); shell != "" && shell != "bash" && shell != "powershell" {
		issues = append(issues, ValidationIssue{
			Field:   "shell",
			Message: fmt.Sprintf("unknown value %q (expected \"bash\" or \"powershell\")", shell),
		})
	}

	return issues
}

// ValidateStrict is used by MCP create/update tools. It requires name and
// description, and rejects unknown frontmatter keys.
func ValidateStrict(fm Frontmatter, name string) []ValidationIssue {
	issues := ValidateSoft(fm, name)
	if fm.String("description") == "" {
		// already added by soft, no duplicate
	}
	for key := range fm {
		if !knownFields[key] {
			issues = append(issues, ValidationIssue{
				Field:   key,
				Message: "unknown frontmatter field",
			})
		}
	}
	return issues
}
