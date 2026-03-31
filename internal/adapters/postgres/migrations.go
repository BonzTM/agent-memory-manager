package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

type migration struct {
	Version     int
	Description string
	SQL         string
}

var migrations = []migration{
	{
		Version:     1,
		Description: "initial postgres canonical and derived schema",
		SQL: `
CREATE TABLE IF NOT EXISTS schema_version (
	version INTEGER PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS events (
	sequence_id BIGSERIAL PRIMARY KEY,
	id TEXT NOT NULL UNIQUE,
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
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	hash TEXT,
	occurred_at TIMESTAMPTZ NOT NULL,
	ingested_at TIMESTAMPTZ NOT NULL,
	reflected_at TIMESTAMPTZ,
	events_fts tsvector GENERATED ALWAYS AS (
		to_tsvector('simple', coalesce(kind,'') || ' ' || coalesce(content,''))
	) STORED
);
CREATE INDEX IF NOT EXISTS idx_events_kind ON events(kind);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_project_id ON events(project_id);
CREATE INDEX IF NOT EXISTS idx_events_occurred_at ON events(occurred_at);
CREATE INDEX IF NOT EXISTS idx_events_reflected_at ON events(reflected_at);
CREATE INDEX IF NOT EXISTS idx_events_sequence_id_reflected ON events(sequence_id, reflected_at);
CREATE INDEX IF NOT EXISTS idx_events_fts ON events USING GIN(events_fts);

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
	source_span_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	summaries_fts tsvector GENERATED ALWAYS AS (
		to_tsvector('simple', coalesce(kind,'') || ' ' || coalesce(title,'') || ' ' || coalesce(body,'') || ' ' || coalesce(tight_description,''))
	) STORED
);
CREATE INDEX IF NOT EXISTS idx_summaries_kind ON summaries(kind);
CREATE INDEX IF NOT EXISTS idx_summaries_scope ON summaries(scope);
CREATE INDEX IF NOT EXISTS idx_summaries_project_id ON summaries(project_id);
CREATE INDEX IF NOT EXISTS idx_summaries_session_id ON summaries(session_id);
CREATE INDEX IF NOT EXISTS idx_summaries_fts ON summaries USING GIN(summaries_fts);

CREATE TABLE IF NOT EXISTS summary_edges (
	parent_summary_id TEXT NOT NULL,
	child_kind TEXT NOT NULL,
	child_id TEXT NOT NULL,
	edge_order INTEGER,
	PRIMARY KEY(parent_summary_id, child_kind, child_id)
);
CREATE INDEX IF NOT EXISTS idx_summary_edges_child ON summary_edges(child_kind, child_id);

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
	confidence DOUBLE PRECISION NOT NULL DEFAULT 0.5,
	importance DOUBLE PRECISION NOT NULL DEFAULT 0.5,
	privacy_level TEXT NOT NULL DEFAULT 'private',
	status TEXT NOT NULL DEFAULT 'active',
	observed_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	valid_from TIMESTAMPTZ,
	valid_to TIMESTAMPTZ,
	last_confirmed_at TIMESTAMPTZ,
	supersedes TEXT,
	superseded_by TEXT,
	superseded_at TIMESTAMPTZ,
	source_event_ids TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	source_summary_ids TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	source_artifact_ids TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	tags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	memories_fts tsvector GENERATED ALWAYS AS (
		to_tsvector('simple', coalesce(type,'') || ' ' || coalesce(subject,'') || ' ' || coalesce(body,'') || ' ' || coalesce(tight_description,''))
	) STORED
);
CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope);
CREATE INDEX IF NOT EXISTS idx_memories_project_id ON memories(project_id);
CREATE INDEX IF NOT EXISTS idx_memories_status ON memories(status);
CREATE INDEX IF NOT EXISTS idx_memories_observed_at ON memories(observed_at);
CREATE INDEX IF NOT EXISTS idx_memories_source_event_ids_gin ON memories USING GIN(source_event_ids);
CREATE INDEX IF NOT EXISTS idx_memories_fts ON memories USING GIN(memories_fts);
CREATE INDEX IF NOT EXISTS idx_memories_agent_id ON memories(agent_id);

CREATE TABLE IF NOT EXISTS claims (
	id TEXT PRIMARY KEY,
	memory_id TEXT NOT NULL REFERENCES memories(id),
	subject_entity_id TEXT,
	predicate TEXT NOT NULL,
	object_value TEXT,
	object_entity_id TEXT,
	confidence DOUBLE PRECISION NOT NULL DEFAULT 0.5,
	source_event_id TEXT,
	source_summary_id TEXT,
	observed_at TIMESTAMPTZ,
	valid_from TIMESTAMPTZ,
	valid_to TIMESTAMPTZ,
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_claims_memory_id ON claims(memory_id);
CREATE INDEX IF NOT EXISTS idx_claims_subject_entity_id ON claims(subject_entity_id);
CREATE INDEX IF NOT EXISTS idx_claims_predicate ON claims(predicate);

CREATE TABLE IF NOT EXISTS entities (
	id TEXT PRIMARY KEY,
	type TEXT NOT NULL,
	canonical_name TEXT NOT NULL,
	aliases TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	description TEXT,
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	entities_fts tsvector GENERATED ALWAYS AS (
		to_tsvector('simple', coalesce(canonical_name,'') || ' ' || coalesce(description,''))
	) STORED
);
CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);
CREATE INDEX IF NOT EXISTS idx_entities_canonical_name ON entities(canonical_name);
CREATE INDEX IF NOT EXISTS idx_entities_fts ON entities USING GIN(entities_fts);

CREATE TABLE IF NOT EXISTS memory_entities (
	memory_id TEXT NOT NULL,
	entity_id TEXT NOT NULL,
	role TEXT,
	PRIMARY KEY(memory_id, entity_id)
);
CREATE INDEX IF NOT EXISTS idx_memory_entities_entity_id ON memory_entities(entity_id);

CREATE TABLE IF NOT EXISTS relationships (
	id TEXT PRIMARY KEY,
	from_entity_id TEXT NOT NULL,
	to_entity_id TEXT NOT NULL,
	relationship_type TEXT NOT NULL,
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_relationships_from ON relationships(from_entity_id);
CREATE INDEX IF NOT EXISTS idx_relationships_to ON relationships(to_entity_id);
CREATE INDEX IF NOT EXISTS idx_relationships_type ON relationships(relationship_type);

CREATE TABLE IF NOT EXISTS entity_graph_projection (
	entity_id TEXT NOT NULL,
	related_entity_id TEXT NOT NULL,
	hop_distance INTEGER NOT NULL,
	relationship_path TEXT,
	score DOUBLE PRECISION NOT NULL DEFAULT 1.0,
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY(entity_id, related_entity_id)
);
CREATE INDEX IF NOT EXISTS idx_entity_graph_proj_entity ON entity_graph_projection(entity_id);

CREATE TABLE IF NOT EXISTS episodes (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	summary TEXT NOT NULL,
	tight_description TEXT NOT NULL,
	scope TEXT NOT NULL,
	project_id TEXT,
	session_id TEXT,
	importance DOUBLE PRECISION NOT NULL DEFAULT 0.5,
	privacy_level TEXT NOT NULL DEFAULT 'private',
	started_at TIMESTAMPTZ,
	ended_at TIMESTAMPTZ,
	source_span_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	source_summary_ids TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	participants TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	related_entities TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	outcomes TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	unresolved_items TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	episodes_fts tsvector GENERATED ALWAYS AS (
		to_tsvector('simple', coalesce(title,'') || ' ' || coalesce(summary,'') || ' ' || coalesce(tight_description,''))
	) STORED
);
CREATE INDEX IF NOT EXISTS idx_episodes_fts ON episodes USING GIN(episodes_fts);

CREATE TABLE IF NOT EXISTS artifacts (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	source_system TEXT,
	project_id TEXT,
	path TEXT,
	content TEXT,
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	status TEXT NOT NULL,
	payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	result_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	error_text TEXT,
	scheduled_at TIMESTAMPTZ,
	started_at TIMESTAMPTZ,
	finished_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ingestion_policies (
	id TEXT PRIMARY KEY,
	pattern_type TEXT NOT NULL,
	pattern TEXT NOT NULL,
	mode TEXT NOT NULL,
	priority INTEGER NOT NULL DEFAULT 0,
	match_mode TEXT NOT NULL DEFAULT 'glob',
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_policies_priority ON ingestion_policies(priority DESC);

CREATE TABLE IF NOT EXISTS recall_history (
	session_id TEXT NOT NULL,
	item_id TEXT NOT NULL,
	item_kind TEXT NOT NULL,
	shown_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_recall_history_session_item ON recall_history(session_id, item_id, item_kind);
CREATE INDEX IF NOT EXISTS idx_recall_history_shown_at ON recall_history(shown_at);

CREATE TABLE IF NOT EXISTS relevance_feedback (
	session_id TEXT NOT NULL,
	item_id TEXT NOT NULL,
	item_kind TEXT NOT NULL,
	action TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (session_id, item_id, action)
);
CREATE INDEX IF NOT EXISTS idx_relevance_feedback_item ON relevance_feedback(item_id);

CREATE TABLE IF NOT EXISTS embeddings (
	object_id TEXT NOT NULL,
	object_kind TEXT NOT NULL,
	embedding BYTEA NOT NULL,
	model TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (object_id, object_kind, model)
);

CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	path TEXT,
	description TEXT,
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS retrieval_cache (
	cache_key TEXT PRIMARY KEY,
	result_json JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ
);
`,
	},
	{
		Version:     2,
		Description: "add depth and condensed_kind to summaries",
		SQL: `
ALTER TABLE summaries ADD COLUMN depth INTEGER NOT NULL DEFAULT 0;
ALTER TABLE summaries ADD COLUMN condensed_kind TEXT NOT NULL DEFAULT '';
`,
	},
	{
		Version:     3,
		Description: "attempt to enable vector extension for future ANN search",
		SQL: `
-- Best-effort: try to enable a vector extension. The embedding_vec column
-- and HNSW index are NOT created here — they require knowing the embedding
-- dimension at setup time and should be created via a separate admin command.
-- This migration only ensures the extension is available for when the operator
-- explicitly enables ANN search.
DO $$
BEGIN
  BEGIN
    CREATE EXTENSION IF NOT EXISTS vectors;
  EXCEPTION WHEN OTHERS THEN
    BEGIN
      CREATE EXTENSION IF NOT EXISTS vectorchord;
    EXCEPTION WHEN OTHERS THEN
      BEGIN
        CREATE EXTENSION IF NOT EXISTS vchord;
      EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'No vector extension available (vectors/vectorchord/vchord). ANN search will use brute-force fallback.';
      END;
    END;
  END;
END
$$;
`,
	},
}

func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	var currentVersion int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&currentVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, m := range migrations {
		if m.Version <= currentVersion {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.Version, m.Description, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES ($1)`, m.Version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}
	return nil
}
