// SPDX-License-Identifier: GPL-3.0-or-later

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// FlaggedNote is a live slice note recorded after the spec's last write.
type FlaggedNote struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	By        string `json:"by"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
}

type specNoteRecord struct {
	FlaggedNote
	RetractedAt string `json:"retracted_at"`
}

func specNoteRecords(raw []byte) ([]specNoteRecord, error) {
	var out struct {
		Notes []specNoteRecord `json:"note_records"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("slicer show notes: %w", err)
	}
	return out.Notes, nil
}

// NotesAfterSpecWrite keeps live notes strictly newer than wrote.
func NotesAfterSpecWrite(raw []byte, wrote time.Time) ([]FlaggedNote, error) {
	records, err := specNoteRecords(raw)
	if err != nil {
		return nil, err
	}
	var notes []FlaggedNote
	for _, n := range records {
		if n.RetractedAt != "" {
			continue
		}
		created, err := time.Parse(time.RFC3339, n.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("note %s created_at: %w", n.ID, err)
		}
		if created.After(wrote) {
			notes = append(notes, n.FlaggedNote)
		}
	}
	return notes, nil
}

// SpecNotesAfterWrite reads the last spec write only when live notes exist.
func SpecNotesAfterWrite(ctx context.Context, d Deps, root, id string, raw []byte) ([]FlaggedNote, error) {
	records, err := specNoteRecords(raw)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(records, func(n specNoteRecord) bool { return n.RetractedAt == "" }) {
		return nil, nil
	}
	wrote, found, err := d.Slicer.LastSpecWrite(ctx, root, id)
	if err != nil || !found {
		return nil, err
	}
	return NotesAfterSpecWrite(raw, wrote)
}

// SpecFingerprintWithNotes preserves the existing key without flagged notes.
// Only flagged note identities enter the additional fingerprint (adj 608).
func SpecFingerprintWithNotes(ctx context.Context, d Deps, root, id string, raw []byte) (string, error) {
	fp, err := SpecFingerprint(raw)
	if err != nil {
		return "", err
	}
	notes, err := SpecNotesAfterWrite(ctx, d, root, id, raw)
	if err != nil {
		return "", err
	}
	return specFingerprintNotes(fp, notes), nil
}

func specFingerprintNotes(fp string, notes []FlaggedNote) string {
	if len(notes) == 0 {
		return fp
	}
	ids := make([]string, 0, len(notes))
	for _, n := range notes {
		ids = append(ids, n.ID)
	}
	slices.Sort(ids)
	b, _ := json.Marshal(struct {
		Spec  string
		Notes []string
	}{fp, ids})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
