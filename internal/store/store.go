// Package store is the embedded SQLite persistence layer.
//
// It uses modernc.org/sqlite, a pure-Go driver: no cgo, so plugin binaries,
// tests and CI stay simple. cgo is needed only by the Fyne binary.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sirius/cogdebt/internal/domain"
)

// schema is applied on every Open. Every statement is idempotent, so opening
// an existing database is a no-op and there is no migration tool to run.
const schema = `
CREATE TABLE IF NOT EXISTS concepts (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	domain      TEXT NOT NULL DEFAULT '',
	roles       TEXT NOT NULL DEFAULT '[]',
	created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS mastery (
	user_id     TEXT NOT NULL,
	concept_id  TEXT NOT NULL,
	level       REAL NOT NULL DEFAULT 0,
	frequency   REAL NOT NULL DEFAULT 0,
	updated_at  INTEGER NOT NULL,
	PRIMARY KEY (user_id, concept_id)
);

CREATE TABLE IF NOT EXISTS evidence (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id     TEXT NOT NULL,
	concept_id  TEXT NOT NULL,
	note        TEXT NOT NULL,
	score       REAL NOT NULL,
	created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS evidence_by_user ON evidence(user_id, concept_id);

CREATE TABLE IF NOT EXISTS mappings (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id     TEXT NOT NULL,
	source_id   TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	payload     TEXT NOT NULL,
	created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS mappings_by_user ON mappings(user_id);

-- Namespaced scratch space so a plugin can persist state without touching
-- the schema. See (*Store).KV.
CREATE TABLE IF NOT EXISTS plugin_kv (
	plugin      TEXT NOT NULL,
	key         TEXT NOT NULL,
	value       TEXT NOT NULL,
	updated_at  INTEGER NOT NULL,
	PRIMARY KEY (plugin, key)
);
`

// Store is the database handle. Safe for concurrent use.
type Store struct{ db *sql.DB }

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. Use ":memory:" in tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// SQLite takes one writer at a time; serialising here avoids "database is
	// locked" under concurrent tool calls.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for plugins that need their own tables.
func (s *Store) DB() *sql.DB { return s.db }

// UpsertConcepts records concepts and ensures a mastery row exists for the
// user. It reports how many rows were new versus refreshed.
func (s *Store) UpsertConcepts(ctx context.Context, userID string, cs []domain.Concept) (added, updated int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	for _, c := range cs {
		if c.ID == "" {
			c.ID = domain.SlugID(c.Name)
		}
		if c.ID == "" {
			continue
		}
		roles, _ := json.Marshal(c.Roles)

		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM mastery WHERE user_id=? AND concept_id=?`, userID, c.ID).Scan(&exists); err != nil {
			return 0, 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO concepts(id,name,domain,roles,created_at) VALUES(?,?,?,?,?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, domain=excluded.domain, roles=excluded.roles`,
			c.ID, c.Name, c.Domain, string(roles), now); err != nil {
			return 0, 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO mastery(user_id,concept_id,level,frequency,updated_at) VALUES(?,?,0,0,?)
			 ON CONFLICT(user_id,concept_id) DO UPDATE SET updated_at=excluded.updated_at`,
			userID, c.ID, now); err != nil {
			return 0, 0, err
		}
		if exists == 0 {
			added++
		} else {
			updated++
		}
	}
	return added, updated, tx.Commit()
}

// Profile returns everything known about a learner, ordered by concept name.
func (s *Store) Profile(ctx context.Context, userID string) ([]domain.ConceptMastery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.name, c.domain, c.roles, m.level, m.frequency
		   FROM mastery m JOIN concepts c ON c.id = m.concept_id
		  WHERE m.user_id = ? ORDER BY c.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ConceptMastery
	for rows.Next() {
		var cm domain.ConceptMastery
		var roles string
		if err := rows.Scan(&cm.Concept.ID, &cm.Concept.Name, &cm.Concept.Domain, &roles, &cm.Mastery.Level, &cm.Mastery.Frequency); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(roles), &cm.Concept.Roles)
		cm.Mastery.ConceptID = cm.Concept.ID
		out = append(out, cm)
	}
	return out, rows.Err()
}

// UpdateMastery moves a learner's grip on a concept toward a graded score and
// records the evidence. It returns the new level.
func (s *Store) UpdateMastery(ctx context.Context, userID, conceptID string, score float64, note string) (float64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var current float64
	switch err := tx.QueryRowContext(ctx, `SELECT level FROM mastery WHERE user_id=? AND concept_id=?`, userID, conceptID).Scan(&current); {
	case err == sql.ErrNoRows:
		current = 0
	case err != nil:
		return 0, err
	}

	next := domain.ApplyGrade(current, score, 0)
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO mastery(user_id,concept_id,level,frequency,updated_at) VALUES(?,?,?,0,?)
		 ON CONFLICT(user_id,concept_id) DO UPDATE SET level=excluded.level, updated_at=excluded.updated_at`,
		userID, conceptID, next, now); err != nil {
		return 0, err
	}
	if note != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO evidence(user_id,concept_id,note,score,created_at) VALUES(?,?,?,?,?)`,
			userID, conceptID, note, score, now); err != nil {
			return 0, err
		}
	}
	return next, tx.Commit()
}

// SetFrequency records how often a concept appears in the learner's real work.
// A retrieval plugin scanning their repositories supplies this, and it is what
// turns mastery into cognitive debt.
func (s *Store) SetFrequency(ctx context.Context, userID, conceptID string, freq float64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mastery(user_id,concept_id,level,frequency,updated_at) VALUES(?,?,0,?,?)
		 ON CONFLICT(user_id,concept_id) DO UPDATE SET frequency=excluded.frequency, updated_at=excluded.updated_at`,
		userID, conceptID, freq, time.Now().Unix())
	return err
}

// SaveMapping stores one analogy for later display.
func (s *Store) SaveMapping(ctx context.Context, userID string, m domain.Mapping) error {
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO mappings(user_id,source_id,target_id,payload,created_at) VALUES(?,?,?,?,?)`,
		userID, domain.SlugID(m.Source.Name), domain.SlugID(m.Target.Name), string(payload), time.Now().Unix())
	return err
}

// Mappings returns the analogies saved for a learner, newest first.
func (s *Store) Mappings(ctx context.Context, userID string) ([]domain.Mapping, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM mappings WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Mapping
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var m domain.Mapping
		if err := json.Unmarshal([]byte(payload), &m); err == nil {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

// KV returns a key/value space private to one plugin. Use it to persist plugin
// state without adding tables: keys are namespaced by plugin name, so two
// plugins cannot collide.
func (s *Store) KV(plugin string) *KV { return &KV{db: s.db, plugin: plugin} }

// KV is one plugin's private key/value space.
type KV struct {
	db     *sql.DB
	plugin string
}

// Get returns the value for key. The bool reports whether it was present.
func (k *KV) Get(ctx context.Context, key string) (string, bool, error) {
	var v string
	switch err := k.db.QueryRowContext(ctx, `SELECT value FROM plugin_kv WHERE plugin=? AND key=?`, k.plugin, key).Scan(&v); {
	case err == sql.ErrNoRows:
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	return v, true, nil
}

// Set writes the value for key.
func (k *KV) Set(ctx context.Context, key, value string) error {
	_, err := k.db.ExecContext(ctx,
		`INSERT INTO plugin_kv(plugin,key,value,updated_at) VALUES(?,?,?,?)
		 ON CONFLICT(plugin,key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		k.plugin, key, value, time.Now().Unix())
	return err
}

// GetJSON decodes the value for key into v.
func (k *KV) GetJSON(ctx context.Context, key string, v any) (bool, error) {
	raw, ok, err := k.Get(ctx, key)
	if err != nil || !ok {
		return ok, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}

// SetJSON encodes v as the value for key.
func (k *KV) SetJSON(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return k.Set(ctx, key, string(raw))
}
