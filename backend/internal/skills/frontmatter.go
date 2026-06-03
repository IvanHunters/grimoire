package skills

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds YAML frontmatter as a generic map so unknown fields
// survive a round-trip through the editor. Field set is governed by the
// Claude Code spec (name, description, when_to_use, argument-hint, arguments,
// disable-model-invocation, user-invocable, allowed-tools, model, effort,
// context, agent, hooks, paths, shell).
type Frontmatter map[string]any

// Parse splits a SKILL.md file into frontmatter and body. If no opening
// `---` delimiter is found at the very start, frontmatter is nil and the
// whole input is treated as body.
func Parse(data []byte) (Frontmatter, string, error) {
	src := data
	if bytes.HasPrefix(src, []byte("\xef\xbb\xbf")) {
		src = src[3:]
	}

	var sep []byte
	switch {
	case bytes.HasPrefix(src, []byte("---\n")):
		sep = []byte("---\n")
	case bytes.HasPrefix(src, []byte("---\r\n")):
		sep = []byte("---\r\n")
	default:
		return nil, string(data), nil
	}

	rest := src[len(sep):]
	var endIdx int
	if i := bytes.Index(rest, []byte("\n---\n")); i >= 0 {
		endIdx = i + 1
	} else if i := bytes.Index(rest, []byte("\n---\r\n")); i >= 0 {
		endIdx = i + 1
	} else if bytes.HasSuffix(rest, []byte("\n---")) {
		endIdx = len(rest) - 3
	} else {
		return nil, string(data), nil
	}

	fmYAML := rest[:endIdx-1]
	bodyStart := endIdx + len("---")
	if bodyStart < len(rest) && (rest[bodyStart] == '\n' || rest[bodyStart] == '\r') {
		if rest[bodyStart] == '\r' && bodyStart+1 < len(rest) && rest[bodyStart+1] == '\n' {
			bodyStart += 2
		} else {
			bodyStart++
		}
	}
	body := ""
	if bodyStart <= len(rest) {
		body = string(rest[bodyStart:])
	}

	fm := Frontmatter{}
	if len(bytes.TrimSpace(fmYAML)) > 0 {
		if err := yaml.Unmarshal(fmYAML, &fm); err != nil {
			return nil, string(data), err
		}
	}
	return fm, body, nil
}

// Marshal serializes frontmatter and body back into SKILL.md bytes. An empty
// frontmatter is omitted (no delimiters written).
func Marshal(fm Frontmatter, body string) ([]byte, error) {
	if len(fm) == 0 {
		return []byte(body), nil
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(map[string]any(fm)); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("---\n")
	buf.WriteString(body)
	return buf.Bytes(), nil
}

// String returns the value of key as a string, or "" if missing or wrong type.
func (f Frontmatter) String(key string) string {
	v, ok := f[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Bool returns the value of key as a bool, or false if missing or wrong type.
func (f Frontmatter) Bool(key string) bool {
	v, ok := f[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// StringList returns the value of key as a list of strings. Accepts either a
// YAML list or a space-separated string, per Claude Code spec for arguments,
// allowed-tools, and paths.
func (f Frontmatter) StringList(key string) []string {
	v, ok := f[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case string:
		fields := strings.Fields(x)
		if len(fields) == 0 {
			return nil
		}
		return fields
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	}
	return nil
}
