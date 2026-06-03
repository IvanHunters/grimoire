package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/ivanohotnikov/markdown-editor/internal/models"
	"github.com/ivanohotnikov/markdown-editor/internal/storage"
)

// TopFolder is the system folder under which all skills appear.
const TopFolder = "Skills"

// MainFile is the canonical entry-point filename for a skill.
const MainFile = "SKILL.md"

// Syncer mirrors ~/.claude/skills/ into Mongo and propagates editor writes
// back to disk. The filesystem is the source of truth.
type Syncer struct {
	root     string
	store    *storage.MongoStorage
	logger   *slog.Logger
	settings *SettingsStore

	watcher *fsnotify.Watcher
	stop    chan struct{}
}

// NewSyncer creates a Syncer. root should be ~/.claude/skills/. The settings
// store is used for enable/disable through skillOverrides.
func NewSyncer(root string, store *storage.MongoStorage, settings *SettingsStore, logger *slog.Logger) *Syncer {
	return &Syncer{
		root:     root,
		store:    store,
		logger:   logger,
		settings: settings,
		stop:     make(chan struct{}),
	}
}

// Start launches the watcher goroutine. Must be called after ImportAll so
// existing files do not trigger duplicate events.
func (s *Syncer) Start() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	s.watcher = w

	if err := s.addRecursive(s.root); err != nil {
		_ = w.Close()
		return err
	}

	go s.run()
	return nil
}

// Stop terminates the watcher.
func (s *Syncer) Stop() {
	if s.watcher == nil {
		return
	}
	close(s.stop)
	_ = s.watcher.Close()
	s.watcher = nil
}

func (s *Syncer) addRecursive(dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if err := s.watcher.Add(path); err != nil {
				s.logger.Warn("watcher add failed", "path", path, "error", err)
			}
		}
		return nil
	})
}

func (s *Syncer) run() {
	debounce := map[string]time.Time{}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case ev, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) == ".DS_Store" {
				continue
			}
			debounce[ev.Name] = time.Now()
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = s.watcher.Add(ev.Name)
				}
			}
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			s.logger.Warn("watcher error", "error", err)
		case <-ticker.C:
			now := time.Now()
			for path, t := range debounce {
				if now.Sub(t) < 200*time.Millisecond {
					continue
				}
				delete(debounce, path)
				s.handleEvent(path)
			}
		}
	}
}

func (s *Syncer) handleEvent(absPath string) {
	rel, err := filepath.Rel(s.root, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 || parts[0] == "" {
		return
	}
	skill := parts[0]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := os.Stat(filepath.Join(s.root, skill)); errors.Is(err, os.ErrNotExist) {
		if err := s.removeSkill(ctx, skill); err != nil {
			s.logger.Warn("remove skill failed", "skill", skill, "error", err)
		}
		return
	}
	if err := s.importSkill(ctx, skill); err != nil {
		s.logger.Warn("refresh skill failed", "skill", skill, "error", err)
	}
}

// ImportAll seeds Mongo from the filesystem. Safe to call repeatedly; uses
// upserts keyed by path.
func (s *Syncer) ImportAll(ctx context.Context) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("mkdir skills root: %w", err)
	}
	if err := s.ensureTopFolder(ctx); err != nil {
		return err
	}

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return fmt.Errorf("read skills root: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := s.importSkill(ctx, e.Name()); err != nil {
			s.logger.Warn("import skill failed", "skill", e.Name(), "error", err)
		}
	}
	return nil
}

func (s *Syncer) ensureTopFolder(ctx context.Context) error {
	existing, err := s.store.GetFolder(ctx, TopFolder)
	if err == nil {
		if !existing.IsSystem || existing.Source != models.FolderSourceSkills {
			existing.IsSystem = true
			existing.Source = models.FolderSourceSkills
			if err := s.store.UpdateFolder(ctx, existing); err != nil {
				return fmt.Errorf("mark Skills system: %w", err)
			}
		}
		return nil
	}
	return s.store.CreateFolder(ctx, &models.Folder{
		Path:     TopFolder,
		IsSystem: true,
		Source:   models.FolderSourceSkills,
	})
}

func (s *Syncer) importSkill(ctx context.Context, name string) error {
	skillDir := filepath.Join(s.root, name)
	folderPath := TopFolder + "/" + name
	if _, err := s.store.GetFolder(ctx, folderPath); err != nil {
		if err := s.store.CreateFolder(ctx, &models.Folder{
			Path:   folderPath,
			Source: models.FolderSourceSkills,
		}); err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create folder %s: %w", folderPath, err)
		}
	}

	keepPaths := map[string]bool{}
	err := filepath.WalkDir(skillDir, func(absPath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if absPath == skillDir {
			return nil
		}
		rel, _ := filepath.Rel(skillDir, absPath)
		rel = filepath.ToSlash(rel)
		notePath := folderPath + "/" + rel

		if d.IsDir() {
			sub := notePath
			if _, err := s.store.GetFolder(ctx, sub); err != nil {
				_ = s.store.CreateFolder(ctx, &models.Folder{
					Path:   sub,
					Source: models.FolderSourceSkills,
				})
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			return err
		}
		noteType := models.NoteTypeSkillResource
		if d.Name() == MainFile && filepath.Dir(rel) == "." {
			noteType = models.NoteTypeSkill
		}
		parentFolder := folderPath
		if filepath.Dir(rel) != "." {
			parentFolder = folderPath + "/" + filepath.ToSlash(filepath.Dir(rel))
		}
		title := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))

		existing, err := s.store.GetNoteByPath(ctx, notePath)
		if err == nil {
			existing.Content = string(content)
			existing.Type = noteType
			existing.Folder = parentFolder
			existing.Title = title
			if err := s.store.UpdateNote(ctx, existing); err != nil {
				return fmt.Errorf("update note %s: %w", notePath, err)
			}
		} else {
			note := &models.Note{
				ID:      uuid.NewString(),
				Path:    notePath,
				Title:   title,
				Folder:  parentFolder,
				Content: string(content),
				Type:    noteType,
			}
			if err := s.store.CreateNote(ctx, note); err != nil {
				return fmt.Errorf("create note %s: %w", notePath, err)
			}
		}
		keepPaths[notePath] = true
		return nil
	})
	if err != nil {
		return err
	}

	stored, err := s.store.ListNotesMeta(ctx, folderPath, true)
	if err == nil {
		for _, n := range stored {
			if keepPaths[n.Path] {
				continue
			}
			if n.Type != models.NoteTypeSkill && n.Type != models.NoteTypeSkillResource {
				continue
			}
			_ = s.store.DeleteNote(ctx, n.ID)
		}
	}
	return nil
}

func (s *Syncer) removeSkill(ctx context.Context, name string) error {
	folderPath := TopFolder + "/" + name
	notes, _ := s.store.ListNotesMeta(ctx, folderPath, true)
	for _, n := range notes {
		_ = s.store.DeleteNote(ctx, n.ID)
	}
	if _, err := s.store.GetFolder(ctx, folderPath); err == nil {
		f := &models.Folder{Path: folderPath, Source: models.FolderSourceSkills}
		_ = s.store.UpdateFolder(ctx, f)
		if err := s.deleteFolderTree(ctx, folderPath); err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) deleteFolderTree(ctx context.Context, folderPath string) error {
	all, err := s.store.ListFolders(ctx)
	if err != nil {
		return err
	}
	for _, f := range all {
		if f.Path == folderPath || strings.HasPrefix(f.Path, folderPath+"/") {
			if f.IsSystem {
				continue
			}
			_ = s.store.DeleteFolder(ctx, f.Path)
		}
	}
	return nil
}

// WriteNote persists a skill note's content to disk. Called by API handlers
// before the Mongo write so the FS stays authoritative.
func (s *Syncer) WriteNote(note *models.Note) error {
	if note == nil {
		return errors.New("nil note")
	}
	if !strings.HasPrefix(note.Path, TopFolder+"/") {
		return fmt.Errorf("not a skill note: %s", note.Path)
	}
	rel := strings.TrimPrefix(note.Path, TopFolder+"/")
	abs := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(note.Content), 0o644)
}

// CreateSkill creates a new skill directory with a SKILL.md file. The skill
// is registered with Mongo via ImportAll-style refresh.
func (s *Syncer) CreateSkill(ctx context.Context, name string, fm Frontmatter, body string) error {
	if !nameRegex.MatchString(name) {
		return fmt.Errorf("invalid skill name: %s", name)
	}
	skillDir := filepath.Join(s.root, name)
	if _, err := os.Stat(skillDir); err == nil {
		return fmt.Errorf("skill already exists: %s", name)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}
	if fm == nil {
		fm = Frontmatter{}
	}
	if fm["name"] == nil {
		fm["name"] = name
	}
	out, err := Marshal(fm, body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(skillDir, MainFile), out, 0o644); err != nil {
		return err
	}
	if s.watcher != nil {
		_ = s.watcher.Add(skillDir)
	}
	return s.importSkill(ctx, name)
}

// DeleteSkill removes a skill directory and its Mongo entries.
func (s *Syncer) DeleteSkill(ctx context.Context, name string) error {
	skillDir := filepath.Join(s.root, name)
	if _, err := os.Stat(skillDir); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("skill not found: %s", name)
	}
	if err := os.RemoveAll(skillDir); err != nil {
		return err
	}
	return s.removeSkill(ctx, name)
}

// IsSkillPath reports whether a note path lives inside the Skills/ tree.
func IsSkillPath(notePath string) bool {
	return notePath == TopFolder || notePath == TopFolder+"/" || strings.HasPrefix(notePath, TopFolder+"/")
}

// SkillNameFromPath extracts the skill name from a Skills/<name>/... path.
func SkillNameFromPath(notePath string) string {
	if !IsSkillPath(notePath) {
		return ""
	}
	rest := strings.TrimPrefix(notePath, TopFolder+"/")
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i]
	}
	return rest
}

// Root returns the absolute path on disk that this Syncer mirrors.
func (s *Syncer) Root() string { return s.root }

// DefaultRoot returns ~/.claude/skills.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills"), nil
}
