package rag

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"reasonix/internal/novel/db"
)

func openTestDB(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestSQLiteVecStore_InsertAndSearch(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}

	// Insert two orthogonal 2-D vectors.
	if err := store.Insert("ch1", []float32{1, 0}); err != nil {
		t.Fatalf("Insert ch1: %v", err)
	}
	if err := store.Insert("ch2", []float32{0, 1}); err != nil {
		t.Fatalf("Insert ch2: %v", err)
	}

	// Query aligned with ch1.
	results, err := store.Search([]float32{1, 0}, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].ChapterID != "ch1" {
		t.Errorf("first result = %q, want ch1", results[0].ChapterID)
	}
	if math.Abs(results[0].Score-1.0) > 1e-6 {
		t.Errorf("ch1 score = %v, want 1.0", results[0].Score)
	}
	if results[1].ChapterID != "ch2" {
		t.Errorf("second result = %q, want ch2", results[1].ChapterID)
	}
	if math.Abs(results[1].Score-0.0) > 1e-6 {
		t.Errorf("ch2 score = %v, want 0.0", results[1].Score)
	}
}

func TestSQLiteVecStore_SearchTopK(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}

	// Insert three vectors with decreasing alignment to the query.
	_ = store.Insert("ch1", []float32{1, 0, 0})
	_ = store.Insert("ch2", []float32{0, 1, 0})
	_ = store.Insert("ch3", []float32{-1, 0, 0})

	results, err := store.Search([]float32{1, 0, 0}, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].ChapterID != "ch1" {
		t.Errorf("first = %q, want ch1", results[0].ChapterID)
	}
	if results[1].ChapterID != "ch2" {
		t.Errorf("second = %q, want ch2", results[1].ChapterID)
	}
}

func TestSQLiteVecStore_Delete(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}

	_ = store.Insert("ch1", []float32{1, 0})
	_ = store.Insert("ch2", []float32{0, 1})

	if err := store.Delete("ch1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err := store.Search([]float32{1, 0}, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result after delete, got %d", len(results))
	}
	if results[0].ChapterID != "ch2" {
		t.Errorf("remaining = %q, want ch2", results[0].ChapterID)
	}
}

func TestSQLiteVecStore_InsertBatch(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}

	items := map[string][]float32{
		"ch1": {1, 0, 0},
		"ch2": {0, 1, 0},
		"ch3": {0, 0, 1},
	}
	if err := store.InsertBatch(items); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}

	results, err := store.Search([]float32{1, 0, 0}, 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	if results[0].ChapterID != "ch1" {
		t.Errorf("first = %q, want ch1", results[0].ChapterID)
	}
}

func TestSQLiteVecStore_InsertBatch_Empty(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}
	if err := store.InsertBatch(nil); err != nil {
		t.Fatalf("InsertBatch(nil): %v", err)
	}
	if err := store.InsertBatch(map[string][]float32{}); err != nil {
		t.Fatalf("InsertBatch(empty): %v", err)
	}
}

func TestSQLiteVecStore_UpdateExisting(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}

	_ = store.Insert("ch1", []float32{1, 0})
	_ = store.Insert("ch1", []float32{0, 1}) // overwrite

	results, err := store.Search([]float32{1, 0}, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].ChapterID != "ch1" {
		t.Errorf("result = %q, want ch1", results[0].ChapterID)
	}
	if math.Abs(results[0].Score-0.0) > 1e-6 {
		t.Errorf("updated score = %v, want 0.0", results[0].Score)
	}
}

func TestSQLiteVecStore_Search_EmptyQuery(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}
	_, err = store.Search([]float32{}, 1)
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
}

func TestSQLiteVecStore_Search_ZeroTopK(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}
	_ = store.Insert("ch1", []float32{1, 0})
	results, err := store.Search([]float32{1, 0}, 0)
	if err != nil {
		t.Fatalf("Search topK=0: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("want 0 results for topK=0, got %d", len(results))
	}
}

func TestSQLiteVecStore_Insert_InvalidInput(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}
	if err := store.Insert("", []float32{1}); err == nil {
		t.Error("expected error for empty chapterID")
	}
	if err := store.Insert("ch1", nil); err == nil {
		t.Error("expected error for nil embedding")
	}
}

func TestSQLiteVecStore_Delete_InvalidInput(t *testing.T) {
	d := openTestDB(t)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		t.Fatalf("NewSQLiteVecStore: %v", err)
	}
	if err := store.Delete(""); err == nil {
		t.Error("expected error for empty chapterID")
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
		want float64
	}{
		{"identical", []float32{1, 2, 3}, []float32{1, 2, 3}, 1.0},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"scaled", []float32{2, 0}, []float32{1, 0}, 1.0},
		{"different_dims", []float32{1, 0, 0}, []float32{0, 1}, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("cosineSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncodeDecodeFloat32s(t *testing.T) {
	original := []float32{0.1, -0.2, 3.14, 1e-6, -1e6}
	blob := encodeFloat32s(original)
	decoded, err := decodeFloat32s(blob)
	if err != nil {
		t.Fatalf("decodeFloat32s: %v", err)
	}
	if len(decoded) != len(original) {
		t.Fatalf("len(decoded)=%d, want %d", len(decoded), len(original))
	}
	for i := range original {
		if math.Abs(float64(decoded[i]-original[i])) > 1e-6 {
			t.Errorf("decoded[%d] = %v, want %v", i, decoded[i], original[i])
		}
	}
}

func TestDecodeFloat32s_InvalidLength(t *testing.T) {
	_, err := decodeFloat32s([]byte{0x01, 0x02, 0x03})
	if err == nil {
		t.Fatal("expected error for invalid blob length")
	}
}

func BenchmarkSQLiteVecStore_Search(b *testing.B) {
	d := openTestDB(b)
	store, err := NewSQLiteVecStore(d.DB)
	if err != nil {
		b.Fatalf("NewSQLiteVecStore: %v", err)
	}

	// Seed 1000 384-dimensional vectors.
	for i := 0; i < 1000; i++ {
		vec := make([]float32, 384)
		for j := range vec {
			vec[j] = float32(j%10) * 0.1
		}
		_ = store.Insert(fmt.Sprintf("ch%d", i), vec)
	}

	query := make([]float32, 384)
	for j := range query {
		query[j] = float32(j%10) * 0.1
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.Search(query, 10)
		if err != nil {
			b.Fatal(err)
		}
	}
}
