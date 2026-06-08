package project

import (
	"context"
	"testing"

	"reasonix/internal/novel/domain"
)

// insertProjectRow seeds the projects table with a single row. Used by
// tests that exercise Manager.Project / Manager.SetPhase without going
// through the full novel_init tool.
func insertProjectRow(t *testing.T, mgr *Manager) error {
	t.Helper()
	const q = `INSERT INTO projects (id, name, genre, pipeline_phase, created_at, modified_at)
	           VALUES (?, ?, ?, ?, ?, ?)`
	_, err := mgr.DB().ExecContext(context.Background(), q,
		"test-project", "测试小说", domain.GenreFantasy,
		domain.PhaseSetting, 1700000000, 1700000000)
	return err
}

func domainPhase(t *testing.T) string { t.Helper(); return domain.PhaseSetting }
