package repo

import (
	"context"
	"fmt"
	"time"

	"reasonix/internal/novel/db"
)

// StatsRepo produces the writing-statistics rollup the novel_stats
// tool returns. Aggregations are intentionally cheap (single SQL
// query each) so the dashboard can call them on every page load.
type StatsRepo struct{ db *db.DB }

func NewStatsRepo(d *db.DB) *StatsRepo { return &StatsRepo{db: d} }

// Stats is the stats snapshot.
type Stats struct {
	TotalChapters    int            `json:"total_chapters"`
	TotalWords       int            `json:"total_words"`
	AverageWords     float64        `json:"average_words"`
	ChaptersByStatus map[string]int `json:"chapters_by_status"`
	ChaptersByArc    []*ArcStat     `json:"chapters_by_arc"`
	DailyWords       []*DailyStat   `json:"daily_words"`
}

// ArcStat is per-arc chapter + word totals.
type ArcStat struct {
	ArcID   string `json:"arc_id"`
	ArcName string `json:"arc_name"`
	Chapters int   `json:"chapters"`
	Words   int    `json:"words"`
}

// DailyStat is per-day word totals over the recent window.
type DailyStat struct {
	Day   string `json:"day"`
	Words int    `json:"words"`
}

// Build returns the full stats snapshot for a project.
func (r *StatsRepo) Build(ctx context.Context, projectID string) (*Stats, error) {
	out := &Stats{
		ChaptersByStatus: map[string]int{},
		ChaptersByArc:    []*ArcStat{},
		DailyWords:       []*DailyStat{},
	}

	// Overall totals.
	row := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(word_count), 0) FROM chapters WHERE project_id = ?`, projectID)
	if err := row.Scan(&out.TotalChapters, &out.TotalWords); err != nil {
		return nil, fmt.Errorf("StatsRepo.Build totals: %w", err)
	}
	if out.TotalChapters > 0 {
		out.AverageWords = float64(out.TotalWords) / float64(out.TotalChapters)
	}

	// Per-status counts.
	rows, err := r.db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM chapters WHERE project_id = ? GROUP BY status`, projectID)
	if err != nil {
		return nil, fmt.Errorf("StatsRepo.Build by status: %w", err)
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out.ChaptersByStatus[s] = n
	}
	rows.Close()

	// Per-arc counts.
	rows, err = r.db.QueryContext(ctx,
		`SELECT o.id, o.title, COUNT(c.id), COALESCE(SUM(c.word_count), 0)
		 FROM outlines o
		 LEFT JOIN chapters c ON c.arc_id = o.id AND c.project_id = o.project_id
		 WHERE o.project_id = ?
		 GROUP BY o.id, o.title
		 ORDER BY o.level, o.order_index`, projectID)
	if err != nil {
		return nil, fmt.Errorf("StatsRepo.Build by arc: %w", err)
	}
	for rows.Next() {
		var as ArcStat
		if err := rows.Scan(&as.ArcID, &as.ArcName, &as.Chapters, &as.Words); err != nil {
			rows.Close()
			return nil, err
		}
		out.ChaptersByArc = append(out.ChaptersByArc, &as)
	}
	rows.Close()

	// Per-day word counts over the last 30 days. SQLite stores
	// modified_at in seconds; we group by date(created_at, 'unixepoch').
	rows, err = r.db.QueryContext(ctx,
		`SELECT date(modified_at, 'unixepoch') AS day, COALESCE(SUM(word_count), 0) AS words
		 FROM chapters
		 WHERE project_id = ? AND modified_at > 0
		 GROUP BY day
		 ORDER BY day DESC
		 LIMIT 30`, projectID)
	if err != nil {
		return nil, fmt.Errorf("StatsRepo.Build daily: %w", err)
	}
	for rows.Next() {
		var d DailyStat
		var day *string
		if err := rows.Scan(&day, &d.Words); err != nil {
			rows.Close()
			return nil, err
		}
		if day != nil {
			d.Day = *day
		}
		out.DailyWords = append(out.DailyWords, &d)
	}
	rows.Close()
	if out.DailyWords == nil {
		out.DailyWords = []*DailyStat{}
	}

	// _ = time.Now reserved for future "as-of" annotations.
	_ = time.Now
	return out, nil
}
