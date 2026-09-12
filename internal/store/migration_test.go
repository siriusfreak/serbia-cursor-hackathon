package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/sirius/cogdebt/internal/store"
)

// TestOpenCollapsesOldDuplicates covers the upgrade path: a database written
// before the unique index existed still has duplicate pairings in it, and the
// index cannot be created while they are there.
func TestOpenCollapsesOldDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
CREATE TABLE mappings (
	id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL,
	source_id TEXT NOT NULL, target_id TEXT NOT NULL,
	payload TEXT NOT NULL, created_at INTEGER NOT NULL);
INSERT INTO mappings(user_id,source_id,target_id,payload,created_at) VALUES
 ('u','backpressure','irreversibility','{"Breakdown":["old"]}',1),
 ('u','backpressure','irreversibility','{"Breakdown":["new"]}',2),
 ('u','rate-limiting','entropy','{"Breakdown":["kept"]}',3);`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("opening a pre-index database failed: %v", err)
	}
	defer db.Close()

	got, err := db.Mappings(t.Context(), "u")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d mappings after the upgrade, want 2", len(got))
	}
	if got[1].Breakdown[0] != "new" {
		t.Errorf("kept %q of the duplicate pair, want the newest row", got[1].Breakdown[0])
	}
}
