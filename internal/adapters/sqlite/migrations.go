package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// migration represents a single schema migration.
type migration struct {
	Version     int
	Description string
	SQL         string
}

// migrations is the ordered list of all schema migrations.
// Each migration must be idempotent and forward-only.
var migrations = []migration{
	{
		Version:     1,
		Description: "initial canonical and derived schema",
		SQL: `
-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
	version INTEGER NOT NULL,
	applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ============================================================
-- Canonical tables
-- ============================================================

-- events: append-only raw interaction history
CREATE TABLE IF NOT EXISTS events (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	source_system TEXT NOT NULL,
	surface TEXT,
	session_id TEXT,
	project_id TEXT,
	agent_id TEXT,
	actor_type TEXT,
	actor_id TEXT,
	privacy_level TEXT NOT NULL DEFAULT 'private',
	content TEXT NOT NULL,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	hash TEXT,
	occurred_at TEXT NOT NULL,
	ingested_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_kind ON events(kind);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_project_id ON events(project_id);
CREATE INDEX IF NOT EXISTS idx_events_occurred_at ON events(occurred_at);

-- summaries: compression layer objects
CREATE TABLE IF NOT EXISTS summaries (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	scope TEXT NOT NULL,
	project_id TEXT,
	session_id TEXT,
	agent_id TEXT,
	title TEXT,
	body TEXT NOT NULL,
	tight_description TEXT NOT NULL,
	privacy_level TEXT NOT NULL DEFAULT 'private',
	source_span_json TEXT NOT NULL DEFAULT '{}',
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_summaries_kind ON summaries(kind);
CREATE INDEX IF NOT EXISTS idx_summaries_scope ON summaries(scope);
CREATE INDEX IF NOT EXISTS idx_summaries_project_id ON summaries(project_id);
CREATE INDEX IF NOT EXISTS idx_summaries_session_id ON summaries(session_id);

-- summary_edges: hierarchy for summary expansion
CREATE TABLE IF NOT EXISTS summary_edges (
	parent_summary_id TEXT NOT NULL,
	child_kind TEXT NOT NULL,
	child_id TEXT NOT NULL,
	edge_order INTEGER,
	PRIMARY KEY(parent_summary_id, child_kind, child_id)
);
CREATE INDEX IF NOT EXISTS idx_summary_edges_child ON summary_edges(child_kind, child_id);

-- memories: canonical typed durable memory records
CREATE TABLE IF NOT EXISTS memories (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL,
	scope TEXT NOT NULL,
	project_id TEXT,
	session_id TEXT,
	agent_id TEXT,
	subject TEXT,
	body TEXT NOT NULL,
	tight_description TEXT NOT NULL,
	confidence REAL NOT NULL DEFAULT 0.5,
	importance REAL NOT NULL DEFAULT 0.5,
	privacy_level TEXT NOT NULL DEFAULT 'private',
	status TEXT NOT NULL DEFAULT 'active',
	observed_at TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	valid_from TEXT,
	valid_to TEXT,
	last_confirmed_at TEXT,
	supersedes TEXT,
	superseded_by TEXT,
	superseded_at TEXT,
	source_event_ids_json TEXT NOT NULL DEFAULT '[]',
	source_summary_ids_json TEXT NOT NULL DEFAULT '[]',
	source_artifact_ids_json TEXT NOT NULL DEFAULT '[]',
	tags_json TEXT NOT NULL DEFAULT '[]',
	metadata_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope);
CREATE INDEX IF NOT EXISTS idx_memories_project_id ON memories(project_id);
CREATE INDEX IF NOT EXISTS idx_memories_status ON memories(status);
CREATE INDEX IF NOT EXISTS idx_memories_observed_at ON memories(observed_at);

-- claims: structured atomic assertions
CREATE TABLE IF NOT EXISTS claims (
	id TEXT PRIMARY KEY,
	memory_id TEXT NOT NULL,
	subject_entity_id TEXT,
	predicate TEXT NOT NULL,
	object_value TEXT,
	object_entity_id TEXT,
	confidence REAL NOT NULL DEFAULT 0.5,
	source_event_id TEXT,
	source_summary_id TEXT,
	observed_at TEXT,
	valid_from TEXT,
	valid_to TEXT,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	FOREIGN KEY(memory_id) REFERENCES memories(id)
);
CREATE INDEX IF NOT EXISTS idx_claims_memory_id ON claims(memory_id);
CREATE INDEX IF NOT EXISTS idx_claims_subject_entity_id ON claims(subject_entity_id);
CREATE INDEX IF NOT EXISTS idx_claims_predicate ON claims(predicate);

-- entities: canonical entities
CREATE TABLE IF NOT EXISTS entities (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL,
	canonical_name TEXT NOT NULL,
	aliases_json TEXT NOT NULL DEFAULT '[]',
	description TEXT,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);
CREATE INDEX IF NOT EXISTS idx_entities_canonical_name ON entities(canonical_name);

-- memory_entities: join table
CREATE TABLE IF NOT EXISTS memory_entities (
	memory_id TEXT NOT NULL,
	entity_id TEXT NOT NULL,
	role TEXT,
	PRIMARY KEY(memory_id, entity_id)
);

-- episodes: narrative memory units
CREATE TABLE IF NOT EXISTS episodes (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	summary TEXT NOT NULL,
	tight_description TEXT NOT NULL,
	scope TEXT NOT NULL,
	project_id TEXT,
	session_id TEXT,
	importance REAL NOT NULL DEFAULT 0.5,
	privacy_level TEXT NOT NULL DEFAULT 'private',
	started_at TEXT,
	ended_at TEXT,
	source_span_json TEXT NOT NULL DEFAULT '{}',
	source_summary_ids_json TEXT NOT NULL DEFAULT '[]',
	participants_json TEXT NOT NULL DEFAULT '[]',
	related_entities_json TEXT NOT NULL DEFAULT '[]',
	outcomes_json TEXT NOT NULL DEFAULT '[]',
	unresolved_items_json TEXT NOT NULL DEFAULT '[]',
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

-- artifacts: ingested non-message source material
CREATE TABLE IF NOT EXISTS artifacts (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	source_system TEXT,
	project_id TEXT,
	path TEXT,
	content TEXT,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
);

-- jobs: maintenance/worker queue
CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	status TEXT NOT NULL,
	payload_json TEXT NOT NULL DEFAULT '{}',
	result_json TEXT NOT NULL DEFAULT '{}',
	error_text TEXT,
	scheduled_at TEXT,
	started_at TEXT,
	finished_at TEXT,
	created_at TEXT NOT NULL
);

-- ingestion_policies: policy rules
CREATE TABLE IF NOT EXISTS ingestion_policies (
	id TEXT PRIMARY KEY,
	pattern_type TEXT NOT NULL,
	pattern TEXT NOT NULL,
	mode TEXT NOT NULL,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

-- ============================================================
-- Derived / index tables
-- ============================================================

-- FTS5 indexes
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
	id UNINDEXED,
	type,
	subject,
	body,
	tight_description,
	tags
);

CREATE VIRTUAL TABLE IF NOT EXISTS summaries_fts USING fts5(
	id UNINDEXED,
	kind,
	title,
	body,
	tight_description
);

CREATE VIRTUAL TABLE IF NOT EXISTS episodes_fts USING fts5(
	id UNINDEXED,
	title,
	summary,
	tight_description
);

CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
	id UNINDEXED,
	kind,
	content
);

-- FTS sync triggers: memories
CREATE TRIGGER IF NOT EXISTS memories_fts_insert AFTER INSERT ON memories BEGIN
	INSERT INTO memories_fts(id, type, subject, body, tight_description, tags)
	VALUES (NEW.id, NEW.type, NEW.subject, NEW.body, NEW.tight_description, NEW.tags_json);
END;

CREATE TRIGGER IF NOT EXISTS memories_fts_update AFTER UPDATE ON memories BEGIN
	DELETE FROM memories_fts WHERE id = OLD.id;
	INSERT INTO memories_fts(id, type, subject, body, tight_description, tags)
	VALUES (NEW.id, NEW.type, NEW.subject, NEW.body, NEW.tight_description, NEW.tags_json);
END;

CREATE TRIGGER IF NOT EXISTS memories_fts_delete AFTER DELETE ON memories BEGIN
	DELETE FROM memories_fts WHERE id = OLD.id;
END;

-- FTS sync triggers: summaries
CREATE TRIGGER IF NOT EXISTS summaries_fts_insert AFTER INSERT ON summaries BEGIN
	INSERT INTO summaries_fts(id, kind, title, body, tight_description)
	VALUES (NEW.id, NEW.kind, NEW.title, NEW.body, NEW.tight_description);
END;

CREATE TRIGGER IF NOT EXISTS summaries_fts_update AFTER UPDATE ON summaries BEGIN
	DELETE FROM summaries_fts WHERE id = OLD.id;
	INSERT INTO summaries_fts(id, kind, title, body, tight_description)
	VALUES (NEW.id, NEW.kind, NEW.title, NEW.body, NEW.tight_description);
END;

CREATE TRIGGER IF NOT EXISTS summaries_fts_delete AFTER DELETE ON summaries BEGIN
	DELETE FROM summaries_fts WHERE id = OLD.id;
END;

-- FTS sync triggers: episodes
CREATE TRIGGER IF NOT EXISTS episodes_fts_insert AFTER INSERT ON episodes BEGIN
	INSERT INTO episodes_fts(id, title, summary, tight_description)
	VALUES (NEW.id, NEW.title, NEW.summary, NEW.tight_description);
END;

CREATE TRIGGER IF NOT EXISTS episodes_fts_update AFTER UPDATE ON episodes BEGIN
	DELETE FROM episodes_fts WHERE id = OLD.id;
	INSERT INTO episodes_fts(id, title, summary, tight_description)
	VALUES (NEW.id, NEW.title, NEW.summary, NEW.tight_description);
END;

CREATE TRIGGER IF NOT EXISTS episodes_fts_delete AFTER DELETE ON episodes BEGIN
	DELETE FROM episodes_fts WHERE id = OLD.id;
END;

-- FTS sync triggers: events (append-only, only INSERT trigger)
CREATE TRIGGER IF NOT EXISTS events_fts_insert AFTER INSERT ON events BEGIN
	INSERT INTO events_fts(id, kind, content)
	VALUES (NEW.id, NEW.kind, NEW.content);
END;

-- Optional embeddings (derived)
CREATE TABLE IF NOT EXISTS embeddings (
	object_id TEXT NOT NULL,
	object_kind TEXT NOT NULL,
	embedding_json TEXT NOT NULL,
	model TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY(object_id, object_kind, model)
);

-- Retrieval cache (derived)
CREATE TABLE IF NOT EXISTS retrieval_cache (
	cache_key TEXT PRIMARY KEY,
	result_json TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT
);

-- Recall history (for repetition suppression)
CREATE TABLE IF NOT EXISTS recall_history (
	session_id TEXT NOT NULL,
	item_id TEXT NOT NULL,
	item_kind TEXT NOT NULL,
	shown_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_recall_history_session_item ON recall_history(session_id, item_id, item_kind);
CREATE INDEX IF NOT EXISTS idx_recall_history_shown_at ON recall_history(shown_at);
`,
	},
	{
		Version:     2,
		Description: "add CHECK constraint triggers for enum columns",
		SQL: `
-- Validate scope values on memories
CREATE TRIGGER IF NOT EXISTS check_memories_scope_insert
BEFORE INSERT ON memories
BEGIN
  SELECT RAISE(ABORT, 'invalid scope: must be global, project, or session')
  WHERE NEW.scope NOT IN ('global', 'project', 'session');
END;

CREATE TRIGGER IF NOT EXISTS check_memories_scope_update
BEFORE UPDATE ON memories
BEGIN
  SELECT RAISE(ABORT, 'invalid scope: must be global, project, or session')
  WHERE NEW.scope NOT IN ('global', 'project', 'session');
END;

-- Validate privacy_level values on memories
CREATE TRIGGER IF NOT EXISTS check_memories_privacy_insert
BEFORE INSERT ON memories
BEGIN
  SELECT RAISE(ABORT, 'invalid privacy_level: must be private, shared, or public_safe')
  WHERE NEW.privacy_level NOT IN ('private', 'shared', 'public_safe');
END;

CREATE TRIGGER IF NOT EXISTS check_memories_privacy_update
BEFORE UPDATE ON memories
BEGIN
  SELECT RAISE(ABORT, 'invalid privacy_level: must be private, shared, or public_safe')
  WHERE NEW.privacy_level NOT IN ('private', 'shared', 'public_safe');
END;

-- Validate memory status
CREATE TRIGGER IF NOT EXISTS check_memories_status_insert
BEFORE INSERT ON memories
BEGIN
  SELECT RAISE(ABORT, 'invalid status: must be active, superseded, archived, or retracted')
  WHERE NEW.status NOT IN ('active', 'superseded', 'archived', 'retracted');
END;

CREATE TRIGGER IF NOT EXISTS check_memories_status_update
BEFORE UPDATE ON memories
BEGIN
  SELECT RAISE(ABORT, 'invalid status: must be active, superseded, archived, or retracted')
  WHERE NEW.status NOT IN ('active', 'superseded', 'archived', 'retracted');
END;

-- Validate events privacy_level
CREATE TRIGGER IF NOT EXISTS check_events_privacy_insert
BEFORE INSERT ON events
BEGIN
  SELECT RAISE(ABORT, 'invalid privacy_level')
  WHERE NEW.privacy_level NOT IN ('private', 'shared', 'public_safe');
END;

-- Validate summaries scope and privacy
CREATE TRIGGER IF NOT EXISTS check_summaries_scope_insert
BEFORE INSERT ON summaries
BEGIN
  SELECT RAISE(ABORT, 'invalid scope')
  WHERE NEW.scope NOT IN ('global', 'project', 'session');
END;

CREATE TRIGGER IF NOT EXISTS check_summaries_privacy_insert
BEFORE INSERT ON summaries
BEGIN
  SELECT RAISE(ABORT, 'invalid privacy_level')
  WHERE NEW.privacy_level NOT IN ('private', 'shared', 'public_safe');
END;

-- Validate episodes scope and privacy
CREATE TRIGGER IF NOT EXISTS check_episodes_scope_insert
BEFORE INSERT ON episodes
BEGIN
  SELECT RAISE(ABORT, 'invalid scope')
  WHERE NEW.scope NOT IN ('global', 'project', 'session');
END;

CREATE TRIGGER IF NOT EXISTS check_episodes_privacy_insert
BEFORE INSERT ON episodes
BEGIN
  SELECT RAISE(ABORT, 'invalid privacy_level')
  WHERE NEW.privacy_level NOT IN ('private', 'shared', 'public_safe');
END;

-- Validate ingestion_policies mode
CREATE TRIGGER IF NOT EXISTS check_policies_mode_insert
BEFORE INSERT ON ingestion_policies
BEGIN
  SELECT RAISE(ABORT, 'invalid mode: must be full, read_only, or ignore')
  WHERE NEW.mode NOT IN ('full', 'read_only', 'ignore');
END;

-- Validate jobs status
CREATE TRIGGER IF NOT EXISTS check_jobs_status_insert
BEFORE INSERT ON jobs
BEGIN
  SELECT RAISE(ABORT, 'invalid status')
  WHERE NEW.status NOT IN ('pending', 'running', 'completed', 'failed');
END;

CREATE TRIGGER IF NOT EXISTS check_jobs_status_update
BEFORE UPDATE ON jobs
BEGIN
  SELECT RAISE(ABORT, 'invalid status')
  WHERE NEW.status NOT IN ('pending', 'running', 'completed', 'failed');
END;
`,
	},
	{
		Version:     3,
		Description: "add reflected_at tracking for events to prevent reprocessing",
		SQL: `
-- Add reflected_at column to track which events have been processed by reflect
ALTER TABLE events ADD COLUMN reflected_at TEXT;

-- Index for efficient querying of unreflected events
CREATE INDEX IF NOT EXISTS idx_events_reflected_at ON events(reflected_at);

-- Index for querying events by rowid range with reflection status
CREATE INDEX IF NOT EXISTS idx_events_rowid_reflected ON events(rowid, reflected_at);
`,
	},
	{
		Version:     4,
		Description: "add events sequence_id for portable ordered pagination",
		SQL: `
-- Add explicit sequence_id to replace SQLite-specific rowid usage
ALTER TABLE events ADD COLUMN sequence_id INTEGER;

-- Backfill sequence_id for existing rows
UPDATE events SET sequence_id = rowid;

-- Indexes for sequence-based pagination and reflection scans
CREATE INDEX IF NOT EXISTS idx_events_sequence_id ON events(sequence_id);
CREATE INDEX IF NOT EXISTS idx_events_sequence_id_reflected ON events(sequence_id, reflected_at);
`,
	},
	{
		Version:     5,
		Description: "add projects and relationships tables",
		SQL: `
-- projects: project registry with metadata
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	path TEXT,
	description TEXT,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
CREATE INDEX IF NOT EXISTS idx_projects_path ON projects(path);

-- relationships: entity-to-entity relationships
CREATE TABLE IF NOT EXISTS relationships (
	id TEXT PRIMARY KEY,
	from_entity_id TEXT NOT NULL,
	to_entity_id TEXT NOT NULL,
	relationship_type TEXT NOT NULL,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	FOREIGN KEY(from_entity_id) REFERENCES entities(id),
	FOREIGN KEY(to_entity_id) REFERENCES entities(id)
);
CREATE INDEX IF NOT EXISTS idx_relationships_from ON relationships(from_entity_id);
CREATE INDEX IF NOT EXISTS idx_relationships_to ON relationships(to_entity_id);
CREATE INDEX IF NOT EXISTS idx_relationships_type ON relationships(relationship_type);
`,
	},
	{
		Version:     6,
		Description: "add priority to ingestion policies and match_mode column",
		SQL: `
ALTER TABLE ingestion_policies ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ingestion_policies ADD COLUMN match_mode TEXT NOT NULL DEFAULT 'glob';
CREATE INDEX IF NOT EXISTS idx_policies_priority ON ingestion_policies(priority DESC);
`,
	},
	{
		Version:     7,
		Description: "add relevance feedback table for implicit ranking signals",
		SQL: `
CREATE TABLE IF NOT EXISTS relevance_feedback (
	session_id TEXT NOT NULL,
	item_id TEXT NOT NULL,
	item_kind TEXT NOT NULL,
	action TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (session_id, item_id, action)
);
CREATE INDEX IF NOT EXISTS idx_relevance_feedback_item ON relevance_feedback(item_id);
`,
	},
	{
		Version:     8,
		Description: "add derived entity graph projection table",
		SQL: `
CREATE TABLE IF NOT EXISTS entity_graph_projection (
	entity_id TEXT NOT NULL,
	related_entity_id TEXT NOT NULL,
	hop_distance INTEGER NOT NULL,
	relationship_path TEXT,
	score REAL NOT NULL DEFAULT 1.0,
	created_at TEXT NOT NULL,
	PRIMARY KEY (entity_id, related_entity_id)
);
CREATE INDEX IF NOT EXISTS idx_entity_graph_proj_entity ON entity_graph_projection(entity_id);
`,
	},
	{
		Version:     9,
		Description: "add depth and condensed_kind to summaries",
		SQL: `
ALTER TABLE summaries ADD COLUMN depth INTEGER NOT NULL DEFAULT 0;
ALTER TABLE summaries ADD COLUMN condensed_kind TEXT NOT NULL DEFAULT '';
`,
	},
}

// Migrate runs all pending migrations.
func Migrate(ctx context.Context, db *DB) error {
	conn := db.Conn()

	// Ensure schema_version table exists for tracking.
	_, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	// Get current version.
	var currentVersion int
	row := conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// Apply pending migrations.
	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}
		if err := execMigration(ctx, tx, m); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.Version, m.Description, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_version (version) VALUES (?)", m.Version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}
	return nil
}

func execMigration(ctx context.Context, tx *sql.Tx, m migration) error {
	if m.Version == 3 {
		_, err := tx.ExecContext(ctx, `
ALTER TABLE events ADD COLUMN reflected_at TEXT;
CREATE INDEX IF NOT EXISTS idx_events_reflected_at ON events(reflected_at);
`)
		return err
	}

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	return nil
}
