package index

import (
	"context"
	"testing"
)

func testIndex(t *testing.T) *metadataCache {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	db, err := openIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	cache, err := loadMetadata(tx)
	if err != nil {
		t.Fatal(err)
	}
	return cache
}
