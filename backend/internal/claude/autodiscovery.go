package claude

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CurrentNote represents the note currently open in the editor
type CurrentNote struct {
	Name        string `json:"name"`
	Folder      string `json:"folder"`
	Content     string `json:"content"`
	Type        string `json:"type"`
	ProjectPath string `json:"projectPath"`
}

// DetermineWorkingDir determines the working directory for Claude subprocess
// Priority: explicit projectPath → /tmp/claude-{hash(folder/name)}
func DetermineWorkingDir(currentNote *CurrentNote, sessionID string) (string, error) {
	// Priority 1: Explicit project path from note metadata
	if currentNote != nil && currentNote.ProjectPath != "" {
		expanded := expandPath(currentNote.ProjectPath)
		if _, err := os.Stat(expanded); err == nil {
			return expanded, nil
		}
	}

	// Priority 2: Temp directory with hash of note path
	hash := hashNotePath(currentNote)
	tempDir := filepath.Join("/tmp", fmt.Sprintf("claude-%s", hash))
	if err := os.MkdirAll(tempDir, 0755); err == nil {
		return tempDir, nil
	}

	// Fallback: system temp directory
	return os.TempDir(), nil
}

// hashNotePath creates a hash from note folder and name
func hashNotePath(note *CurrentNote) string {
	if note == nil {
		return "default"
	}

	// Combine folder and name to create unique path
	notePath := filepath.Join(note.Folder, note.Name)
	if notePath == "" || notePath == "." {
		return "default"
	}

	// Create SHA256 hash
	h := sha256.Sum256([]byte(notePath))
	// Return first 12 characters of hex
	return fmt.Sprintf("%x", h)[:12]
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
