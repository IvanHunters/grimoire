package claude

import (
	"regexp"
	"strings"
)

// ToolUse represents a detected tool use in Claude output
type ToolUse struct {
	Name string
	Args string
}

// ParseToolUse extracts tool name from Claude output
// Format: "🔧 tool_name" or "Tool: tool_name" or similar
func ParseToolUse(line string) *ToolUse {
	// Pattern 1: "🔧 tool_name(args)"
	re1 := regexp.MustCompile(`🔧\s+(\w+)(?:\((.*?)\))?`)
	matches := re1.FindStringSubmatch(line)
	if len(matches) >= 2 {
		tool := &ToolUse{Name: matches[1]}
		if len(matches) >= 3 {
			tool.Args = matches[2]
		}
		return tool
	}

	// Pattern 2: "Tool: tool_name"
	re2 := regexp.MustCompile(`Tool:\s+(\w+)`)
	matches = re2.FindStringSubmatch(line)
	if len(matches) >= 2 {
		return &ToolUse{Name: matches[1]}
	}

	// Pattern 3: "<tool_name>" in XML-like format
	re3 := regexp.MustCompile(`<(\w+)>`)
	matches = re3.FindStringSubmatch(line)
	if len(matches) >= 2 {
		// Filter out common HTML tags
		toolName := matches[1]
		if !isHTMLTag(toolName) {
			return &ToolUse{Name: toolName}
		}
	}

	return nil
}

// DetectInterrupted checks if the line indicates an interruption
func DetectInterrupted(line string) bool {
	lowerLine := strings.ToLower(line)

	keywords := []string{
		"interrupted",
		"stopped",
		"cancelled",
		"canceled",
		"^c",
	}

	for _, keyword := range keywords {
		if strings.Contains(lowerLine, keyword) {
			return true
		}
	}

	return false
}

// isHTMLTag checks if a tag name is a common HTML tag
func isHTMLTag(tag string) bool {
	htmlTags := map[string]bool{
		"div": true, "span": true, "p": true, "a": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
		"ul": true, "ol": true, "li": true,
		"table": true, "tr": true, "td": true, "th": true,
		"pre": true, "code": true,
		"b": true, "i": true, "strong": true, "em": true,
		"img": true, "br": true, "hr": true,
	}

	return htmlTags[strings.ToLower(tag)]
}

// IsMessageComplete checks if a line indicates message completion
func IsMessageComplete(line string) bool {
	// Common indicators of message completion
	indicators := []string{
		"message complete",
		"done",
		"finished",
	}

	lowerLine := strings.ToLower(strings.TrimSpace(line))

	for _, indicator := range indicators {
		if strings.Contains(lowerLine, indicator) {
			return true
		}
	}

	return false
}

// StripANSI removes ANSI escape codes from a string
func StripANSI(s string) string {
	// ANSI escape code pattern
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiRegex.ReplaceAllString(s, "")
}
