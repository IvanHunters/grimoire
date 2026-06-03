package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ivanohotnikov/markdown-editor/internal/skills"
)

// SkillSummary is one entry in the GET /api/skills response.
type SkillSummary struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	State       skills.SkillOverrideState `json:"state"`
	Enabled     bool                     `json:"enabled"`
	Valid       bool                     `json:"valid"`
	Issues      []skills.ValidationIssue `json:"issues,omitempty"`
	Frontmatter skills.Frontmatter       `json:"frontmatter,omitempty"`
}

// CreateSkillRequest is the body for POST /api/skills.
type CreateSkillRequest struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Content     string             `json:"content"`
	Frontmatter skills.Frontmatter `json:"frontmatter,omitempty"`
}

// ListSkills handles GET /api/skills.
func (h *Handler) ListSkills(w http.ResponseWriter, r *http.Request) {
	if h.skills == nil {
		http.Error(w, "skills syncer not initialized", http.StatusServiceUnavailable)
		return
	}
	root := h.skills.Root()
	entries, err := os.ReadDir(root)
	if err != nil {
		h.logger.Error("read skills root", "error", err)
		http.Error(w, "Failed to read skills directory", http.StatusInternalServerError)
		return
	}
	overrides, _ := h.skillSettings.GetAll()

	out := make([]SkillSummary, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		main := filepath.Join(root, name, skills.MainFile)
		data, err := os.ReadFile(main)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			h.logger.Warn("read SKILL.md", "skill", name, "error", err)
			continue
		}
		fm, _, err := skills.Parse(data)
		var issues []skills.ValidationIssue
		if err != nil {
			issues = append(issues, skills.ValidationIssue{Field: "", Message: err.Error()})
		} else {
			issues = skills.ValidateSoft(fm, name)
		}
		state := overrides[name]
		if state == "" {
			state = skills.OverrideOn
		}
		out = append(out, SkillSummary{
			Name:        name,
			Description: fm.String("description"),
			State:       state,
			Enabled:     state != skills.OverrideOff,
			Valid:       len(issues) == 0,
			Issues:      issues,
			Frontmatter: fm,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// CreateSkill handles POST /api/skills.
func (h *Handler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	if h.skills == nil {
		http.Error(w, "skills syncer not initialized", http.StatusServiceUnavailable)
		return
	}
	var req CreateSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	desc := strings.TrimSpace(req.Description)
	if name == "" || desc == "" {
		http.Error(w, "name and description are required", http.StatusBadRequest)
		return
	}

	fm := req.Frontmatter
	if fm == nil {
		fm = skills.Frontmatter{}
	}
	fm["name"] = name
	fm["description"] = desc

	if issues := skills.ValidateStrict(fm, name); len(issues) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": "invalid frontmatter", "issues": issues})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.skills.CreateSkill(ctx, name, fm, req.Content); err != nil {
		h.logger.Error("create skill", "name", name, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"name": name})
}

// DeleteSkill handles DELETE /api/skills/{name}.
func (h *Handler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	if h.skills == nil {
		http.Error(w, "skills syncer not initialized", http.StatusServiceUnavailable)
		return
	}
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.skills.DeleteSkill(ctx, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.skillSettings.SetState(name, skills.OverrideOn)
	w.WriteHeader(http.StatusNoContent)
}

// SetSkillState handles POST /api/skills/{name}/state.
func (h *Handler) SetSkillState(w http.ResponseWriter, r *http.Request) {
	if h.skills == nil {
		http.Error(w, "skills syncer not initialized", http.StatusServiceUnavailable)
		return
	}
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	var req struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	state := skills.SkillOverrideState(req.State)
	switch state {
	case skills.OverrideOn, skills.OverrideOff, skills.OverrideNameOnly, skills.OverrideUserInvocableOnly:
	default:
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	if err := h.skillSettings.SetState(name, state); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RefreshSkills handles POST /api/skills/refresh, re-importing the FS state.
func (h *Handler) RefreshSkills(w http.ResponseWriter, r *http.Request) {
	if h.skills == nil {
		http.Error(w, "skills syncer not initialized", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.skills.ImportAll(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
