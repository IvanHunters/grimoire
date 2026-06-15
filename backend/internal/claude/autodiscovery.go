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

// DetermineWorkingDir determines the working directory for a Claude session.
//
// Priority:
//  1. Note's explicit projectPath (links a note to a repo on disk)
//  2. Folder-inherited projectPath
//  3. Persistent per-note dir at ~/.markdown-editor/sessions/<hash>/
//     (was /tmp/claude-<hash> before — wiped on reboot; now survives)
//  4. System temp dir as last-resort fallback
//
// Sessions in tier 3 accumulate over time. A periodic GC (not implemented
// here) should sweep dirs older than N days whose notes no longer exist.
func DetermineWorkingDir(currentNote *CurrentNote, folderProjectPath string, sessionID string) (string, error) {
	// Priority 1: Explicit project path from note metadata
	if currentNote != nil && currentNote.ProjectPath != "" {
		expanded := expandPath(currentNote.ProjectPath)
		if _, err := os.Stat(expanded); err == nil {
			return expanded, nil
		}
	}

	// Priority 2: Inherited project path from folder
	if folderProjectPath != "" {
		expanded := expandPath(folderProjectPath)
		if _, err := os.Stat(expanded); err == nil {
			return expanded, nil
		}
	}

	// Priority 3: Persistent per-note directory.
	if dataDir, err := sessionsDataDir(); err == nil {
		hash := hashNotePath(currentNote)
		sessionDir := filepath.Join(dataDir, hash)
		if err := os.MkdirAll(sessionDir, 0755); err == nil {
			return sessionDir, nil
		}
	}

	// Fallback: system temp directory
	return os.TempDir(), nil
}

// sessionsDataDir returns the persistent root for per-note session cwds.
// Defaults to ~/.markdown-editor/sessions/, overridable via the
// MARKDOWN_EDITOR_DATA_DIR env var (in which case the leaf is /sessions).
// The directory is created on first call.
func sessionsDataDir() (string, error) {
	root := os.Getenv("MARKDOWN_EDITOR_DATA_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		root = filepath.Join(home, ".markdown-editor")
	}
	sessions := filepath.Join(root, "sessions")
	if err := os.MkdirAll(sessions, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", sessions, err)
	}
	return sessions, nil
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
