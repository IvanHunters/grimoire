package claude

import (
	"os"
	"path/filepath"
	"strings"
)

// CurrentNote represents the note currently open in the editor
type CurrentNote struct {
	Name        string `json:"name"`
	Content     string `json:"content"`
	Type        string `json:"type"`
	ProjectPath string `json:"projectPath"`
}

// DetermineWorkingDir determines the working directory for Claude subprocess
// Priority: explicit projectPath → autodiscovery → ~/notes → /tmp/claude-{sessionID} → /tmp
func DetermineWorkingDir(currentNote *CurrentNote, sessionID string) (string, error) {
	// Priority 1: Explicit project path
	if currentNote != nil && currentNote.ProjectPath != "" {
		expanded := expandPath(currentNote.ProjectPath)
		if _, err := os.Stat(expanded); err == nil {
			return expanded, nil
		}
	}

	// Priority 2: Autodiscovery by note title
	if currentNote != nil && currentNote.Name != "" {
		// Extract title without .md extension
		title := strings.TrimSuffix(currentNote.Name, ".md")
		projects := findProjectByTitle(title)
		if len(projects) > 0 {
			// Use first match
			return projects[0], nil
		}
	}

	// Priority 3: ~/notes directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		notesDir := filepath.Join(homeDir, "notes")
		if _, err := os.Stat(notesDir); err == nil {
			return notesDir, nil
		}
	}

	// Priority 4: Create temp directory for session
	if homeDir, err := os.UserHomeDir(); err == nil {
		tempDir := filepath.Join(homeDir, ".claude", "sessions", sessionID)
		if err := os.MkdirAll(tempDir, 0755); err == nil {
			return tempDir, nil
		}
	}

	// Priority 5: System temp directory
	return os.TempDir(), nil
}

// findProjectByTitle searches for projects in ~/git/github.com/$USER/
// Returns list of matching project paths
func findProjectByTitle(title string) []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return []string{}
	}

	// Get current user from home directory
	parts := strings.Split(homeDir, "/")
	if len(parts) < 2 {
		return []string{}
	}
	user := parts[len(parts)-1]

	basePath := filepath.Join(homeDir, "git", "github.com", user)

	// Check if directory exists
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return []string{}
	}

	normalizedTitle := normalizeTitle(title)
	var matches []string

	// Read directories
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return []string{}
	}

	// First pass: exact matches
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()
		normalizedDir := normalizeTitle(dirName)

		if normalizedDir == normalizedTitle {
			matches = append(matches, filepath.Join(basePath, dirName))
		}
	}

	// If no exact matches, try fuzzy matching
	if len(matches) == 0 {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			dirName := entry.Name()
			normalizedDir := normalizeTitle(dirName)

			if strings.Contains(normalizedDir, normalizedTitle) {
				matches = append(matches, filepath.Join(basePath, dirName))
			}
		}
	}

	return matches
}

// normalizeTitle converts title to lowercase slug for matching
func normalizeTitle(title string) string {
	// Convert to lowercase
	s := strings.ToLower(title)

	// Replace spaces and special characters with hyphens
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)

	// Remove consecutive hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	// Trim hyphens from edges
	s = strings.Trim(s, "-")

	return s
}

// expandPath expands ~ to home directory
func expandPath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if path == "~" {
		return homeDir
	}

	return filepath.Join(homeDir, path[2:])
}
