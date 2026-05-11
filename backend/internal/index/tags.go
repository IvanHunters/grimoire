package index

import (
	"sort"
	"strings"
	"sync"

	"github.com/ivanohotnikov/markdown-editor/internal/models"
)

// TagsIndex provides in-memory inverted index for fast tag-based search
type TagsIndex struct {
	tagToNotes map[string]map[string]struct{} // tag -> set of note IDs
	notes      map[string]*models.Note        // all notes in memory
	mu         sync.RWMutex
}

// NewTagsIndex creates a new tags index
func NewTagsIndex() *TagsIndex {
	return &TagsIndex{
		tagToNotes: make(map[string]map[string]struct{}),
		notes:      make(map[string]*models.Note),
	}
}

// Build builds the index from a list of notes
func (idx *TagsIndex) Build(notes []*models.Note) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Clear existing index
	idx.tagToNotes = make(map[string]map[string]struct{})
	idx.notes = make(map[string]*models.Note)

	// Build inverted index
	for _, note := range notes {
		idx.notes[note.ID] = note

		for _, tag := range note.Tags {
			normalizedTag := normalizeTag(tag)
			if normalizedTag == "" {
				continue
			}

			if _, ok := idx.tagToNotes[normalizedTag]; !ok {
				idx.tagToNotes[normalizedTag] = make(map[string]struct{})
			}
			idx.tagToNotes[normalizedTag][note.ID] = struct{}{}
		}
	}
}

// AddNote adds or updates a note in the index
func (idx *TagsIndex) AddNote(note *models.Note) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old tags if note exists
	if oldNote, ok := idx.notes[note.ID]; ok {
		for _, tag := range oldNote.Tags {
			normalizedTag := normalizeTag(tag)
			if noteSet, ok := idx.tagToNotes[normalizedTag]; ok {
				delete(noteSet, note.ID)
				if len(noteSet) == 0 {
					delete(idx.tagToNotes, normalizedTag)
				}
			}
		}
	}

	// Add new note
	idx.notes[note.ID] = note

	// Add new tags
	for _, tag := range note.Tags {
		normalizedTag := normalizeTag(tag)
		if normalizedTag == "" {
			continue
		}

		if _, ok := idx.tagToNotes[normalizedTag]; !ok {
			idx.tagToNotes[normalizedTag] = make(map[string]struct{})
		}
		idx.tagToNotes[normalizedTag][note.ID] = struct{}{}
	}
}

// RemoveNote removes a note from the index
func (idx *TagsIndex) RemoveNote(noteID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	note, ok := idx.notes[noteID]
	if !ok {
		return
	}

	// Remove from tag index
	for _, tag := range note.Tags {
		normalizedTag := normalizeTag(tag)
		if noteSet, ok := idx.tagToNotes[normalizedTag]; ok {
			delete(noteSet, noteID)
			if len(noteSet) == 0 {
				delete(idx.tagToNotes, normalizedTag)
			}
		}
	}

	// Remove from notes map
	delete(idx.notes, noteID)
}

// SearchResult represents a note with match score
type SearchResult struct {
	Note       *models.Note
	MatchCount int // number of tags matched
}

// SearchByTags searches notes by tags (parallel, ranked by match count)
func (idx *TagsIndex) SearchByTags(tags []string, limit int) []*models.Note {
	if len(tags) == 0 {
		return []*models.Note{}
	}

	if limit <= 0 {
		limit = 50 // default
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Normalize query tags
	normalizedTags := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		normalizedTag := normalizeTag(tag)
		if normalizedTag != "" {
			normalizedTags[normalizedTag] = struct{}{}
		}
	}

	if len(normalizedTags) == 0 {
		return []*models.Note{}
	}

	// Count matches for each note (parallel processing)
	resultMap := make(map[string]*SearchResult)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for tag := range normalizedTags {
		noteIDs, ok := idx.tagToNotes[tag]
		if !ok {
			continue
		}

		wg.Add(1)
		go func(tag string, noteIDs map[string]struct{}) {
			defer wg.Done()

			localResults := make(map[string]*SearchResult, len(noteIDs))

			for noteID := range noteIDs {
				note, ok := idx.notes[noteID]
				if !ok {
					continue
				}

				if result, ok := localResults[noteID]; ok {
					result.MatchCount++
				} else {
					localResults[noteID] = &SearchResult{
						Note:       note,
						MatchCount: 1,
					}
				}
			}

			mu.Lock()
			for noteID, result := range localResults {
				if existing, ok := resultMap[noteID]; ok {
					existing.MatchCount += result.MatchCount
				} else {
					resultMap[noteID] = result
				}
			}
			mu.Unlock()
		}(tag, noteIDs)
	}

	wg.Wait()

	// Convert to slice and sort by match count
	results := make([]*SearchResult, 0, len(resultMap))
	for _, result := range resultMap {
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		// Sort by match count (descending), then by updated time (descending)
		if results[i].MatchCount != results[j].MatchCount {
			return results[i].MatchCount > results[j].MatchCount
		}
		return results[i].Note.UpdatedAt.After(results[j].Note.UpdatedAt)
	})

	// Take top N results
	if len(results) > limit {
		results = results[:limit]
	}

	// Extract notes
	notes := make([]*models.Note, len(results))
	for i, result := range results {
		notes[i] = result.Note
	}

	return notes
}

// GetAllTags returns all unique tags with note counts
func (idx *TagsIndex) GetAllTags() map[string]int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tagCounts := make(map[string]int, len(idx.tagToNotes))
	for tag, noteSet := range idx.tagToNotes {
		tagCounts[tag] = len(noteSet)
	}

	return tagCounts
}

// normalizeTag normalizes tag for indexing (lowercase, trim)
func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}
