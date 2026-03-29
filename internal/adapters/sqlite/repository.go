package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// sanitizeFTS5Query strips FTS5 operators and special characters from user input
// so it can be safely passed to a MATCH clause. Returns plain space-separated tokens.
func sanitizeFTS5Query(query string) string {
	var cleaned strings.Builder
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			cleaned.WriteRune(r)
		} else {
			cleaned.WriteRune(' ')
		}
	}

	words := strings.Fields(cleaned.String())
	var filtered []string
	for _, w := range words {
		upper := strings.ToUpper(w)
		if upper == "NOT" || upper == "OR" || upper == "AND" || upper == "NEAR" {
			continue
		}
		filtered = append(filtered, w)
	}
	return strings.Join(filtered, " ")
}

func generateID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("generateID: crypto/rand failed: %v", err))
	}
	return prefix + hex.EncodeToString(b)
}

// SQLiteRepository implements core.Repository backed by SQLite.
type SQLiteRepository struct {
	*DB
}

// Compile-time check that SQLiteRepository implements core.Repository.
var _ core.Repository = (*SQLiteRepository)(nil)

// NewSQLiteRepository creates a new repository (DB not yet opened).
func NewSQLiteRepository() *SQLiteRepository {
	return &SQLiteRepository{}
}

// ---------- Lifecycle ----------

func (r *SQLiteRepository) Open(ctx context.Context, dbPath string) error {
	db, err := Open(ctx, dbPath)
	if err != nil {
		return err
	}
	r.DB = db
	return nil
}

func (r *SQLiteRepository) Close() error {
	if r.DB == nil {
		return nil
	}
	return r.DB.Close()
}

func (r *SQLiteRepository) Migrate(ctx context.Context) error {
	return Migrate(ctx, r.DB)
}

func (r *SQLiteRepository) IsInitialized(ctx context.Context) (bool, error) {
	row := r.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_version'")
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	row = r.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_version")
	var ver int
	if err := row.Scan(&ver); err != nil {
		return false, err
	}
	return ver > 0, nil
}

// ---------- helpers ----------

func defaultLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

func marshalJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func emptyMapJSON() string   { return "{}" }
func emptySliceJSON() string { return "[]" }

func marshalMapJSON(m map[string]string) string {
	if m == nil {
		return emptyMapJSON()
	}
	return marshalJSON(m)
}

func marshalSliceJSON(s []string) string {
	if s == nil {
		return emptySliceJSON()
	}
	return marshalJSON(s)
}

func unmarshalMap(data string) map[string]string {
	m := make(map[string]string)
	_ = json.Unmarshal([]byte(data), &m)
	return m
}

func unmarshalSlice(data string) []string {
	var s []string
	_ = json.Unmarshal([]byte(data), &s)
	return s
}

func marshalEmbeddingJSON(v []float32) string {
	if v == nil {
		return emptySliceJSON()
	}
	return marshalJSON(v)
}

func unmarshalEmbeddingJSON(data string) []float32 {
	var v []float32
	_ = json.Unmarshal([]byte(data), &v)
	return v
}

func timeToStr(t time.Time) string {
	return t.Format(time.RFC3339)
}

func ptrTimeToStr(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format(time.RFC3339), Valid: true}
}

// strToTime parses an RFC3339 string to time.Time.
// Returns zero time if parsing fails; callers must tolerate zero values for
// data that predates strict format enforcement.
func strToTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func nullStrToPtrTime(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t := strToTime(ns.String)
	return &t
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// placeholders returns a comma-separated list of n placeholders for SQL IN clauses.
// placeholders generates a comma-separated list of SQL placeholders (?).
// Used to construct IN clause queries dynamically.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	result := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			result = append(result, ',')
		}
		result = append(result, '?')
	}
	return string(result)
}

// ---------- Events ----------

func (r *SQLiteRepository) InsertEvent(ctx context.Context, event *core.Event) error {
	if event.ID == "" {
		event.ID = generateID("evt_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO events (id, kind, source_system, surface, session_id, project_id,
			agent_id, actor_type, actor_id, privacy_level, content, metadata_json, hash,
			occurred_at, ingested_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.ID, event.Kind, event.SourceSystem,
		nullStr(event.Surface), nullStr(event.SessionID), nullStr(event.ProjectID),
		nullStr(event.AgentID), nullStr(event.ActorType), nullStr(event.ActorID),
		string(event.PrivacyLevel), event.Content,
		marshalMapJSON(event.Metadata), nullStr(event.Hash),
		timeToStr(event.OccurredAt), timeToStr(event.IngestedAt),
	)
	if err != nil {
		return err
	}
	_, err = r.ExecContext(ctx, `UPDATE events SET sequence_id = rowid WHERE id = ?`, event.ID)
	return err
}

func (r *SQLiteRepository) GetEvent(ctx context.Context, id string) (*core.Event, error) {
	row := r.QueryRowContext(ctx, `
		SELECT COALESCE(sequence_id, rowid), id, kind, source_system, COALESCE(surface,''), COALESCE(session_id,''),
			COALESCE(project_id,''), COALESCE(agent_id,''), COALESCE(actor_type,''),
			COALESCE(actor_id,''), privacy_level, content, metadata_json,
			COALESCE(hash,''), occurred_at, ingested_at, COALESCE(reflected_at,'')
		FROM events WHERE id = ?`, id)

	var e core.Event
	var metaJSON, occurredAt, ingestedAt, reflectedAt string
	err := row.Scan(&e.SequenceID, &e.ID, &e.Kind, &e.SourceSystem, &e.Surface, &e.SessionID,
		&e.ProjectID, &e.AgentID, &e.ActorType, &e.ActorID, &e.PrivacyLevel,
		&e.Content, &metaJSON, &e.Hash, &occurredAt, &ingestedAt, &reflectedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("event not found: %s", id)
		}
		return nil, err
	}
	e.Metadata = unmarshalMap(metaJSON)
	e.OccurredAt = strToTime(occurredAt)
	e.IngestedAt = strToTime(ingestedAt)
	if reflectedAt != "" {
		t := strToTime(reflectedAt)
		e.ReflectedAt = &t
	}
	return &e, nil
}

func (r *SQLiteRepository) ListEvents(ctx context.Context, opts core.ListEventsOptions) ([]core.Event, error) {
	query := `SELECT COALESCE(sequence_id, rowid), id, kind, source_system, COALESCE(surface,''), COALESCE(session_id,''),
		COALESCE(project_id,''), COALESCE(agent_id,''), COALESCE(actor_type,''),
		COALESCE(actor_id,''), privacy_level, content, metadata_json,
		COALESCE(hash,''), occurred_at, ingested_at, COALESCE(reflected_at,'')
		FROM events WHERE 1=1`
	var args []interface{}

	if opts.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, opts.SessionID)
	}
	if opts.ProjectID != "" {
		query += " AND project_id = ?"
		args = append(args, opts.ProjectID)
	}
	if opts.Kind != "" {
		query += " AND kind = ?"
		args = append(args, opts.Kind)
	}
	if opts.Before != "" {
		query += " AND occurred_at < ?"
		args = append(args, opts.Before)
	}
	if opts.BeforeSequenceID > 0 {
		query += " AND sequence_id <= ?"
		args = append(args, opts.BeforeSequenceID)
	}
	if opts.After != "" {
		query += " AND occurred_at > ?"
		args = append(args, opts.After)
	}
	if opts.AfterSequenceID > 0 {
		query += " AND sequence_id > ?"
		args = append(args, opts.AfterSequenceID)
	}
	if opts.UnreflectedOnly {
		query += " AND reflected_at IS NULL"
	}
	if opts.AfterSequenceID > 0 || opts.BeforeSequenceID > 0 || opts.UnreflectedOnly {
		query += " ORDER BY sequence_id ASC LIMIT ?"
	} else {
		query += " ORDER BY occurred_at DESC, id DESC LIMIT ?"
	}
	args = append(args, defaultLimit(opts.Limit))

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		var metaJSON, occurredAt, ingestedAt, reflectedAt string
		if err := rows.Scan(&e.SequenceID, &e.ID, &e.Kind, &e.SourceSystem, &e.Surface, &e.SessionID,
			&e.ProjectID, &e.AgentID, &e.ActorType, &e.ActorID, &e.PrivacyLevel,
			&e.Content, &metaJSON, &e.Hash, &occurredAt, &ingestedAt, &reflectedAt); err != nil {
			return nil, err
		}
		e.Metadata = unmarshalMap(metaJSON)
		e.OccurredAt = strToTime(occurredAt)
		e.IngestedAt = strToTime(ingestedAt)
		if reflectedAt != "" {
			t := strToTime(reflectedAt)
			e.ReflectedAt = &t
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (r *SQLiteRepository) SearchEvents(ctx context.Context, query string, limit int) ([]core.Event, error) {
	q := sanitizeFTS5Query(query)
	if q == "" {
		return nil, nil
	}
	rows, err := r.QueryContext(ctx, `
		SELECT COALESCE(e.sequence_id, e.rowid), e.id, e.kind, e.source_system, COALESCE(e.surface,''), COALESCE(e.session_id,''),
			COALESCE(e.project_id,''), COALESCE(e.agent_id,''), COALESCE(e.actor_type,''),
			COALESCE(e.actor_id,''), e.privacy_level, e.content, e.metadata_json,
			COALESCE(e.hash,''), e.occurred_at, e.ingested_at, COALESCE(e.reflected_at,'')
		FROM events_fts f JOIN events e ON f.id = e.id
		WHERE events_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, q, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		var metaJSON, occurredAt, ingestedAt, reflectedAt string
		if err := rows.Scan(&e.SequenceID, &e.ID, &e.Kind, &e.SourceSystem, &e.Surface, &e.SessionID,
			&e.ProjectID, &e.AgentID, &e.ActorType, &e.ActorID, &e.PrivacyLevel,
			&e.Content, &metaJSON, &e.Hash, &occurredAt, &ingestedAt, &reflectedAt); err != nil {
			return nil, err
		}
		e.Metadata = unmarshalMap(metaJSON)
		e.OccurredAt = strToTime(occurredAt)
		e.IngestedAt = strToTime(ingestedAt)
		if reflectedAt != "" {
			t := strToTime(reflectedAt)
			e.ReflectedAt = &t
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (r *SQLiteRepository) UpdateEvent(ctx context.Context, event *core.Event) error {
	_, err := r.ExecContext(ctx, `
		UPDATE events SET
			kind = ?, source_system = ?, surface = ?, session_id = ?, project_id = ?,
			agent_id = ?, actor_type = ?, actor_id = ?, privacy_level = ?, content = ?,
			metadata_json = ?, hash = ?, occurred_at = ?, ingested_at = ?, reflected_at = ?
		WHERE id = ?`,
		event.Kind, event.SourceSystem,
		nullStr(event.Surface), nullStr(event.SessionID), nullStr(event.ProjectID),
		nullStr(event.AgentID), nullStr(event.ActorType), nullStr(event.ActorID),
		string(event.PrivacyLevel), event.Content,
		marshalMapJSON(event.Metadata), nullStr(event.Hash),
		timeToStr(event.OccurredAt), timeToStr(event.IngestedAt),
		ptrTimeToStr(event.ReflectedAt),
		event.ID,
	)
	return err
}

func (r *SQLiteRepository) CountUnreflectedEvents(ctx context.Context) (int64, error) {
	row := r.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE reflected_at IS NULL`)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// ClaimUnreflectedEvents atomically selects and marks unreflected events in a single
// UPDATE ... RETURNING statement. Because the write and read happen in one statement,
// there is no window for concurrent callers to claim the same rows. This approach is
// portable across SQLite (3.35+) and PostgreSQL without requiring Go-level mutexes or
// database-specific transaction modes.
func (r *SQLiteRepository) ClaimUnreflectedEvents(ctx context.Context, limit int) ([]core.Event, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := r.QueryContext(ctx, `
		UPDATE events
		SET reflected_at = ?
		WHERE id IN (
			SELECT id FROM events
			WHERE reflected_at IS NULL
			ORDER BY sequence_id ASC
			LIMIT ?
		)
		RETURNING COALESCE(sequence_id, rowid), id, kind, source_system, COALESCE(surface,''), COALESCE(session_id,''),
			COALESCE(project_id,''), COALESCE(agent_id,''), COALESCE(actor_type,''),
			COALESCE(actor_id,''), privacy_level, content, metadata_json,
			COALESCE(hash,''), occurred_at, ingested_at`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim unreflected events: %w", err)
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		var metaJSON, occurredAt, ingestedAt string
		if err := rows.Scan(&e.SequenceID, &e.ID, &e.Kind, &e.SourceSystem, &e.Surface, &e.SessionID,
			&e.ProjectID, &e.AgentID, &e.ActorType, &e.ActorID, &e.PrivacyLevel,
			&e.Content, &metaJSON, &e.Hash, &occurredAt, &ingestedAt); err != nil {
			return nil, err
		}
		e.Metadata = unmarshalMap(metaJSON)
		e.OccurredAt = strToTime(occurredAt)
		e.IngestedAt = strToTime(ingestedAt)
		events = append(events, e)
	}
	return events, rows.Err()
}
func (r *SQLiteRepository) InsertSummary(ctx context.Context, summary *core.Summary) error {
	if summary.ID == "" {
		summary.ID = generateID("sum_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO summaries (id, kind, scope, project_id, session_id, agent_id,
			title, body, tight_description, privacy_level, source_span_json,
			metadata_json, depth, condensed_kind, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		summary.ID, summary.Kind, string(summary.Scope),
		nullStr(summary.ProjectID), nullStr(summary.SessionID), nullStr(summary.AgentID),
		nullStr(summary.Title), summary.Body, summary.TightDescription,
		string(summary.PrivacyLevel), marshalJSON(summary.SourceSpan),
		marshalMapJSON(summary.Metadata), summary.Depth, summary.CondensedKind,
		timeToStr(summary.CreatedAt), timeToStr(summary.UpdatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetSummary(ctx context.Context, id string) (*core.Summary, error) {
	row := r.QueryRowContext(ctx, `
		SELECT id, kind, scope, COALESCE(project_id,''), COALESCE(session_id,''),
			COALESCE(agent_id,''), COALESCE(title,''), body, tight_description,
			privacy_level, source_span_json, metadata_json, depth, COALESCE(condensed_kind,''), created_at, updated_at
		FROM summaries WHERE id = ?`, id)

	var s core.Summary
	var spanJSON, metaJSON, createdAt, updatedAt string
	err := row.Scan(&s.ID, &s.Kind, &s.Scope, &s.ProjectID, &s.SessionID,
		&s.AgentID, &s.Title, &s.Body, &s.TightDescription,
		&s.PrivacyLevel, &spanJSON, &metaJSON, &s.Depth, &s.CondensedKind, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("summary not found: %s", id)
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(spanJSON), &s.SourceSpan)
	s.Metadata = unmarshalMap(metaJSON)
	s.CreatedAt = strToTime(createdAt)
	s.UpdatedAt = strToTime(updatedAt)
	return &s, nil
}

func (r *SQLiteRepository) ListSummaries(ctx context.Context, opts core.ListSummariesOptions) ([]core.Summary, error) {
	query := `SELECT id, kind, scope, COALESCE(project_id,''), COALESCE(session_id,''),
		COALESCE(agent_id,''), COALESCE(title,''), body, tight_description,
		privacy_level, source_span_json, metadata_json, depth, COALESCE(condensed_kind,''), created_at, updated_at
		FROM summaries WHERE 1=1`
	var args []interface{}

	if opts.Kind != "" {
		query += " AND kind = ?"
		args = append(args, opts.Kind)
	}
	if opts.Scope != "" {
		query += " AND scope = ?"
		args = append(args, string(opts.Scope))
	}
	if opts.ProjectID != "" {
		query += " AND project_id = ?"
		args = append(args, opts.ProjectID)
	}
	if opts.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, opts.SessionID)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, defaultLimit(opts.Limit))

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []core.Summary
	for rows.Next() {
		var s core.Summary
		var spanJSON, metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.Kind, &s.Scope, &s.ProjectID, &s.SessionID,
			&s.AgentID, &s.Title, &s.Body, &s.TightDescription,
			&s.PrivacyLevel, &spanJSON, &metaJSON, &s.Depth, &s.CondensedKind, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(spanJSON), &s.SourceSpan)
		s.Metadata = unmarshalMap(metaJSON)
		s.CreatedAt = strToTime(createdAt)
		s.UpdatedAt = strToTime(updatedAt)
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

func (r *SQLiteRepository) SearchSummaries(ctx context.Context, query string, limit int) ([]core.Summary, error) {
	q := sanitizeFTS5Query(query)
	if q == "" {
		return nil, nil
	}
	rows, err := r.QueryContext(ctx, `
		SELECT s.id, s.kind, s.scope, COALESCE(s.project_id,''), COALESCE(s.session_id,''),
			COALESCE(s.agent_id,''), COALESCE(s.title,''), s.body, s.tight_description,
			s.privacy_level, s.source_span_json, s.metadata_json, s.depth, COALESCE(s.condensed_kind,''), s.created_at, s.updated_at
		FROM summaries_fts f JOIN summaries s ON f.id = s.id
		WHERE summaries_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, q, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []core.Summary
	for rows.Next() {
		var s core.Summary
		var spanJSON, metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.Kind, &s.Scope, &s.ProjectID, &s.SessionID,
			&s.AgentID, &s.Title, &s.Body, &s.TightDescription,
			&s.PrivacyLevel, &spanJSON, &metaJSON, &s.Depth, &s.CondensedKind, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(spanJSON), &s.SourceSpan)
		s.Metadata = unmarshalMap(metaJSON)
		s.CreatedAt = strToTime(createdAt)
		s.UpdatedAt = strToTime(updatedAt)
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

func (r *SQLiteRepository) GetSummaryChildren(ctx context.Context, parentID string) ([]core.SummaryEdge, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT parent_summary_id, child_kind, child_id, COALESCE(edge_order, 0)
		FROM summary_edges WHERE parent_summary_id = ?
		ORDER BY edge_order`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []core.SummaryEdge
	for rows.Next() {
		var e core.SummaryEdge
		if err := rows.Scan(&e.ParentSummaryID, &e.ChildKind, &e.ChildID, &e.EdgeOrder); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

func (r *SQLiteRepository) ListParentedSummaryIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT DISTINCT child_id
		FROM summary_edges
		WHERE child_kind = 'summary'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}

	return ids, rows.Err()
}

func (r *SQLiteRepository) InsertSummaryEdge(ctx context.Context, edge *core.SummaryEdge) error {
	_, err := r.ExecContext(ctx, `
		INSERT INTO summary_edges (parent_summary_id, child_kind, child_id, edge_order)
		VALUES (?,?,?,?)`,
		edge.ParentSummaryID, edge.ChildKind, edge.ChildID, edge.EdgeOrder)
	return err
}

// ---------- Memories ----------

func (r *SQLiteRepository) InsertMemory(ctx context.Context, memory *core.Memory) error {
	if memory.ID == "" {
		memory.ID = generateID("mem_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO memories (id, type, scope, project_id, session_id, agent_id,
			subject, body, tight_description, confidence, importance, privacy_level,
			status, observed_at, created_at, updated_at, valid_from, valid_to,
			last_confirmed_at, supersedes, superseded_by, superseded_at,
			source_event_ids_json, source_summary_ids_json, source_artifact_ids_json,
			tags_json, metadata_json)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		memory.ID, string(memory.Type), string(memory.Scope),
		nullStr(memory.ProjectID), nullStr(memory.SessionID), nullStr(memory.AgentID),
		nullStr(memory.Subject), memory.Body, memory.TightDescription,
		memory.Confidence, memory.Importance, string(memory.PrivacyLevel),
		string(memory.Status),
		ptrTimeToStr(memory.ObservedAt),
		timeToStr(memory.CreatedAt), timeToStr(memory.UpdatedAt),
		ptrTimeToStr(memory.ValidFrom), ptrTimeToStr(memory.ValidTo),
		ptrTimeToStr(memory.LastConfirmedAt),
		nullStr(memory.Supersedes), nullStr(memory.SupersededBy),
		ptrTimeToStr(memory.SupersededAt),
		marshalSliceJSON(memory.SourceEventIDs),
		marshalSliceJSON(memory.SourceSummaryIDs),
		marshalSliceJSON(memory.SourceArtifactIDs),
		marshalSliceJSON(memory.Tags),
		marshalMapJSON(memory.Metadata),
	)
	return err
}

func (r *SQLiteRepository) scanMemory(scanner interface {
	Scan(dest ...interface{}) error
}) (*core.Memory, error) {
	var m core.Memory
	var observedAt, validFrom, validTo, lastConfirmed, supersededAt sql.NullString
	var metaJSON, evtJSON, sumJSON, artJSON, tagsJSON string
	var createdAt, updatedAt string

	err := scanner.Scan(
		&m.ID, &m.Type, &m.Scope, &m.ProjectID, &m.SessionID, &m.AgentID,
		&m.Subject, &m.Body, &m.TightDescription, &m.Confidence, &m.Importance,
		&m.PrivacyLevel, &m.Status,
		&observedAt, &createdAt, &updatedAt, &validFrom, &validTo,
		&lastConfirmed, &m.Supersedes, &m.SupersededBy, &supersededAt,
		&evtJSON, &sumJSON, &artJSON, &tagsJSON, &metaJSON,
	)
	if err != nil {
		return nil, err
	}
	m.ObservedAt = nullStrToPtrTime(observedAt)
	m.CreatedAt = strToTime(createdAt)
	m.UpdatedAt = strToTime(updatedAt)
	m.ValidFrom = nullStrToPtrTime(validFrom)
	m.ValidTo = nullStrToPtrTime(validTo)
	m.LastConfirmedAt = nullStrToPtrTime(lastConfirmed)
	m.SupersededAt = nullStrToPtrTime(supersededAt)
	m.SourceEventIDs = unmarshalSlice(evtJSON)
	m.SourceSummaryIDs = unmarshalSlice(sumJSON)
	m.SourceArtifactIDs = unmarshalSlice(artJSON)
	m.Tags = unmarshalSlice(tagsJSON)
	m.Metadata = unmarshalMap(metaJSON)
	return &m, nil
}

const memoryCols = `id, type, scope, COALESCE(project_id,''), COALESCE(session_id,''),
	COALESCE(agent_id,''), COALESCE(subject,''), body, tight_description,
	confidence, importance, privacy_level, status,
	observed_at, created_at, updated_at, valid_from, valid_to,
	last_confirmed_at, COALESCE(supersedes,''), COALESCE(superseded_by,''),
	superseded_at,
	source_event_ids_json, source_summary_ids_json, source_artifact_ids_json,
	tags_json, metadata_json`

func (r *SQLiteRepository) GetMemory(ctx context.Context, id string) (*core.Memory, error) {
	row := r.QueryRowContext(ctx,
		"SELECT "+memoryCols+" FROM memories WHERE id = ?", id)
	m, err := r.scanMemory(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("memory not found: %s", id)
		}
		return nil, err
	}
	return m, nil
}

func (r *SQLiteRepository) GetMemoriesByIDs(ctx context.Context, ids []string) (map[string]*core.Memory, error) {
	memories := make(map[string]*core.Memory, len(ids))
	if len(ids) == 0 {
		return memories, nil
	}

	placeholder := strings.Repeat("?,", len(ids)-1) + "?"
	query := "SELECT " + memoryCols + " FROM memories WHERE id IN (" + placeholder + ")"
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories[m.ID] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memories, nil
}

func (r *SQLiteRepository) UpdateMemory(ctx context.Context, memory *core.Memory) error {
	_, err := r.ExecContext(ctx, `
		UPDATE memories SET type=?, scope=?, project_id=?, session_id=?, agent_id=?,
			subject=?, body=?, tight_description=?, confidence=?, importance=?,
			privacy_level=?, status=?, observed_at=?, updated_at=?,
			valid_from=?, valid_to=?, last_confirmed_at=?,
			supersedes=?, superseded_by=?, superseded_at=?,
			source_event_ids_json=?, source_summary_ids_json=?, source_artifact_ids_json=?,
			tags_json=?, metadata_json=?
		WHERE id=?`,
		string(memory.Type), string(memory.Scope),
		nullStr(memory.ProjectID), nullStr(memory.SessionID), nullStr(memory.AgentID),
		nullStr(memory.Subject), memory.Body, memory.TightDescription,
		memory.Confidence, memory.Importance, string(memory.PrivacyLevel),
		string(memory.Status),
		ptrTimeToStr(memory.ObservedAt), timeToStr(memory.UpdatedAt),
		ptrTimeToStr(memory.ValidFrom), ptrTimeToStr(memory.ValidTo),
		ptrTimeToStr(memory.LastConfirmedAt),
		nullStr(memory.Supersedes), nullStr(memory.SupersededBy),
		ptrTimeToStr(memory.SupersededAt),
		marshalSliceJSON(memory.SourceEventIDs),
		marshalSliceJSON(memory.SourceSummaryIDs),
		marshalSliceJSON(memory.SourceArtifactIDs),
		marshalSliceJSON(memory.Tags),
		marshalMapJSON(memory.Metadata),
		memory.ID,
	)
	return err
}

func (r *SQLiteRepository) UpdateMemoriesBatch(ctx context.Context, memories []*core.Memory) error {
	if len(memories) == 0 {
		return nil
	}
	tx, err := r.Conn().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update memories batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE memories SET type=?, scope=?, project_id=?, session_id=?, agent_id=?,
			subject=?, body=?, tight_description=?, confidence=?, importance=?,
			privacy_level=?, status=?, observed_at=?, updated_at=?,
			valid_from=?, valid_to=?, last_confirmed_at=?,
			supersedes=?, superseded_by=?, superseded_at=?,
			source_event_ids_json=?, source_summary_ids_json=?, source_artifact_ids_json=?,
			tags_json=?, metadata_json=?
		WHERE id=?`)
	if err != nil {
		return fmt.Errorf("prepare update memories batch: %w", err)
	}
	defer stmt.Close()

	for _, memory := range memories {
		if _, err := stmt.ExecContext(ctx,
			string(memory.Type), string(memory.Scope),
			nullStr(memory.ProjectID), nullStr(memory.SessionID), nullStr(memory.AgentID),
			nullStr(memory.Subject), memory.Body, memory.TightDescription,
			memory.Confidence, memory.Importance, string(memory.PrivacyLevel),
			string(memory.Status),
			ptrTimeToStr(memory.ObservedAt), timeToStr(memory.UpdatedAt),
			ptrTimeToStr(memory.ValidFrom), ptrTimeToStr(memory.ValidTo),
			ptrTimeToStr(memory.LastConfirmedAt),
			nullStr(memory.Supersedes), nullStr(memory.SupersededBy),
			ptrTimeToStr(memory.SupersededAt),
			marshalSliceJSON(memory.SourceEventIDs),
			marshalSliceJSON(memory.SourceSummaryIDs),
			marshalSliceJSON(memory.SourceArtifactIDs),
			marshalSliceJSON(memory.Tags),
			marshalMapJSON(memory.Metadata),
			memory.ID,
		); err != nil {
			return fmt.Errorf("update memory %s in batch: %w", memory.ID, err)
		}
	}

	return tx.Commit()
}

func (r *SQLiteRepository) ListMemories(ctx context.Context, opts core.ListMemoriesOptions) ([]core.Memory, error) {
	query := "SELECT " + memoryCols + " FROM memories WHERE 1=1"
	var args []interface{}

	if opts.Type != "" {
		query += " AND type = ?"
		args = append(args, string(opts.Type))
	}
	if opts.Scope != "" {
		query += " AND scope = ?"
		args = append(args, string(opts.Scope))
	}
	if opts.ProjectID != "" {
		query += " AND project_id = ?"
		args = append(args, opts.ProjectID)
	}
	if opts.AgentID != "" {
		query += " AND (agent_id = ? OR agent_id = '' OR agent_id IS NULL OR privacy_level IN ('shared', 'public_safe'))"
		args = append(args, opts.AgentID)
	}
	if opts.Status != "" {
		query += " AND status = ?"
		args = append(args, string(opts.Status))
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, defaultLimit(opts.Limit))

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []core.Memory
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, *m)
	}
	return memories, rows.Err()
}

func (r *SQLiteRepository) SearchMemories(ctx context.Context, query string, opts core.ListMemoriesOptions) ([]core.Memory, error) {
	sq := sanitizeFTS5Query(query)
	if sq == "" {
		return nil, nil
	}
	q := "SELECT " + prefixCols("m", memoryCols) + `
		FROM memories_fts f JOIN memories m ON f.id = m.id
		WHERE memories_fts MATCH ?`
	args := []interface{}{sq}
	if opts.Status != "" {
		q += " AND m.status = ?"
		args = append(args, string(opts.Status))
	}
	if opts.Type != "" {
		q += " AND m.type = ?"
		args = append(args, string(opts.Type))
	}
	if opts.Scope != "" {
		q += " AND m.scope = ?"
		args = append(args, string(opts.Scope))
	}
	if opts.ProjectID != "" {
		q += " AND m.project_id = ?"
		args = append(args, opts.ProjectID)
	}
	if opts.AgentID != "" {
		q += " AND (m.agent_id = ? OR m.agent_id = '' OR m.agent_id IS NULL OR m.privacy_level IN ('shared', 'public_safe'))"
		args = append(args, opts.AgentID)
	}
	q += `
		ORDER BY rank
		LIMIT ?`
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []core.Memory
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, *m)
	}
	return memories, rows.Err()
}

func (r *SQLiteRepository) SearchMemoriesFuzzy(ctx context.Context, text string, opts core.ListMemoriesOptions) ([]core.Memory, error) {
	q := sanitizeFTS5Query(text)
	if q == "" {
		return nil, nil
	}

	sqlQuery := "SELECT " + prefixCols("m", memoryCols) + `
		FROM memories_fts f JOIN memories m ON f.id = m.id
		WHERE memories_fts MATCH ?`
	args := []interface{}{q}

	if opts.Status != "" {
		sqlQuery += " AND m.status = ?"
		args = append(args, string(opts.Status))
	}
	if opts.Type != "" {
		sqlQuery += " AND m.type = ?"
		args = append(args, string(opts.Type))
	}
	if opts.Scope != "" {
		sqlQuery += " AND m.scope = ?"
		args = append(args, string(opts.Scope))
	}
	if opts.ProjectID != "" {
		sqlQuery += " AND m.project_id = ?"
		args = append(args, opts.ProjectID)
	}
	if opts.AgentID != "" {
		sqlQuery += " AND (m.agent_id = ? OR m.agent_id = '' OR m.agent_id IS NULL OR m.privacy_level IN ('shared', 'public_safe'))"
		args = append(args, opts.AgentID)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	sqlQuery += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := r.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories := make([]core.Memory, 0, limit)
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, *m)
	}

	return memories, rows.Err()
}

func (r *SQLiteRepository) ListMemoriesBySourceEventIDs(ctx context.Context, eventIDs []string) ([]core.Memory, error) {
	if len(eventIDs) == 0 {
		return []core.Memory{}, nil
	}

	query := "SELECT " + prefixCols("m", memoryCols) + `
		FROM memories m
		WHERE m.status = 'active'
		  AND EXISTS (
			SELECT 1
			FROM json_each(m.source_event_ids_json) j
			WHERE j.value IN (` + placeholders(len(eventIDs)) + `)
		  )
		ORDER BY m.created_at DESC`
	args := make([]interface{}, 0, len(eventIDs))
	for _, eventID := range eventIDs {
		args = append(args, eventID)
	}

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories := make([]core.Memory, 0)
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, *m)
	}
	return memories, rows.Err()
}

// prefixCols adds a table alias prefix to each column expression in a comma-separated list.
// It handles COALESCE(...) expressions correctly.
func prefixCols(alias, cols string) string {
	// Split carefully: we need to handle COALESCE() which contains commas.
	var result []string
	depth := 0
	start := 0
	for i := 0; i < len(cols); i++ {
		switch cols[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(cols[start:i]))
				start = i + 1
			}
		}
	}
	result = append(result, strings.TrimSpace(cols[start:]))

	var prefixed []string
	for _, col := range result {
		upper := strings.ToUpper(col)
		if strings.HasPrefix(upper, "COALESCE(") {
			// Replace the column name inside COALESCE: COALESCE(col,'') -> COALESCE(m.col,'')
			inner := col[len("COALESCE(") : len(col)-1]
			parts := strings.SplitN(inner, ",", 2)
			prefixed = append(prefixed, fmt.Sprintf("COALESCE(%s.%s,%s)", alias, strings.TrimSpace(parts[0]), parts[1]))
		} else {
			prefixed = append(prefixed, alias+"."+col)
		}
	}
	return strings.Join(prefixed, ", ")
}

// ---------- Claims ----------

func (r *SQLiteRepository) InsertClaim(ctx context.Context, claim *core.Claim) error {
	if claim.ID == "" {
		claim.ID = generateID("clm_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO claims (id, memory_id, subject_entity_id, predicate, object_value,
			object_entity_id, confidence, source_event_id, source_summary_id,
			observed_at, valid_from, valid_to, metadata_json)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		claim.ID, claim.MemoryID,
		nullStr(claim.SubjectEntityID), claim.Predicate,
		nullStr(claim.ObjectValue), nullStr(claim.ObjectEntityID),
		claim.Confidence,
		nullStr(claim.SourceEventID), nullStr(claim.SourceSummaryID),
		ptrTimeToStr(claim.ObservedAt),
		ptrTimeToStr(claim.ValidFrom), ptrTimeToStr(claim.ValidTo),
		marshalMapJSON(claim.Metadata),
	)
	return err
}

func (r *SQLiteRepository) GetClaim(ctx context.Context, id string) (*core.Claim, error) {
	row := r.QueryRowContext(ctx, `
		SELECT id, memory_id, COALESCE(subject_entity_id,''), predicate,
			COALESCE(object_value,''), COALESCE(object_entity_id,''), confidence,
			COALESCE(source_event_id,''), COALESCE(source_summary_id,''),
			observed_at, valid_from, valid_to, metadata_json
		FROM claims WHERE id = ?`, id)

	var c core.Claim
	var observedAt, validFrom, validTo sql.NullString
	var metaJSON string
	err := row.Scan(&c.ID, &c.MemoryID, &c.SubjectEntityID, &c.Predicate,
		&c.ObjectValue, &c.ObjectEntityID, &c.Confidence,
		&c.SourceEventID, &c.SourceSummaryID,
		&observedAt, &validFrom, &validTo, &metaJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("claim not found: %s", id)
		}
		return nil, err
	}
	c.ObservedAt = nullStrToPtrTime(observedAt)
	c.ValidFrom = nullStrToPtrTime(validFrom)
	c.ValidTo = nullStrToPtrTime(validTo)
	c.Metadata = unmarshalMap(metaJSON)
	return &c, nil
}

func (r *SQLiteRepository) ListClaimsByMemory(ctx context.Context, memoryID string) ([]core.Claim, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT id, memory_id, COALESCE(subject_entity_id,''), predicate,
			COALESCE(object_value,''), COALESCE(object_entity_id,''), confidence,
			COALESCE(source_event_id,''), COALESCE(source_summary_id,''),
			observed_at, valid_from, valid_to, metadata_json
		FROM claims WHERE memory_id = ?`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claims []core.Claim
	for rows.Next() {
		var c core.Claim
		var observedAt, validFrom, validTo sql.NullString
		var metaJSON string
		if err := rows.Scan(&c.ID, &c.MemoryID, &c.SubjectEntityID, &c.Predicate,
			&c.ObjectValue, &c.ObjectEntityID, &c.Confidence,
			&c.SourceEventID, &c.SourceSummaryID,
			&observedAt, &validFrom, &validTo, &metaJSON); err != nil {
			return nil, err
		}
		c.ObservedAt = nullStrToPtrTime(observedAt)
		c.ValidFrom = nullStrToPtrTime(validFrom)
		c.ValidTo = nullStrToPtrTime(validTo)
		c.Metadata = unmarshalMap(metaJSON)
		claims = append(claims, c)
	}
	return claims, rows.Err()
}

// ---------- Entities ----------

func (r *SQLiteRepository) InsertEntity(ctx context.Context, entity *core.Entity) error {
	if entity.ID == "" {
		entity.ID = generateID("ent_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO entities (id, type, canonical_name, aliases_json, description,
			metadata_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		entity.ID, entity.Type, entity.CanonicalName,
		marshalSliceJSON(entity.Aliases), nullStr(entity.Description),
		marshalMapJSON(entity.Metadata),
		timeToStr(entity.CreatedAt), timeToStr(entity.UpdatedAt),
	)
	return err
}

func (r *SQLiteRepository) UpdateEntity(ctx context.Context, entity *core.Entity) error {
	_, err := r.ExecContext(ctx, `
		UPDATE entities
		SET type = ?, canonical_name = ?, aliases_json = ?, description = ?, metadata_json = ?, updated_at = ?
		WHERE id = ?`,
		entity.Type,
		entity.CanonicalName,
		marshalSliceJSON(entity.Aliases),
		nullStr(entity.Description),
		marshalMapJSON(entity.Metadata),
		timeToStr(entity.UpdatedAt),
		entity.ID,
	)
	return err
}

func (r *SQLiteRepository) GetEntity(ctx context.Context, id string) (*core.Entity, error) {
	row := r.QueryRowContext(ctx, `
		SELECT id, type, canonical_name, aliases_json, COALESCE(description,''),
			metadata_json, created_at, updated_at
		FROM entities WHERE id = ?`, id)

	var e core.Entity
	var aliasesJSON, metaJSON, createdAt, updatedAt string
	err := row.Scan(&e.ID, &e.Type, &e.CanonicalName, &aliasesJSON,
		&e.Description, &metaJSON, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entity not found: %s", id)
		}
		return nil, err
	}
	e.Aliases = unmarshalSlice(aliasesJSON)
	e.Metadata = unmarshalMap(metaJSON)
	e.CreatedAt = strToTime(createdAt)
	e.UpdatedAt = strToTime(updatedAt)
	return &e, nil
}

func (r *SQLiteRepository) GetEntitiesByIDs(ctx context.Context, ids []string) ([]core.Entity, error) {
	if len(ids) == 0 {
		return []core.Entity{}, nil
	}
	placeholder := strings.Repeat("?,", len(ids)-1) + "?"
	query := `
		SELECT id, type, canonical_name, aliases_json, COALESCE(description,''),
			metadata_json, created_at, updated_at
		FROM entities
		WHERE id IN (` + placeholder + `)`
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entities := make([]core.Entity, 0)
	for rows.Next() {
		var e core.Entity
		var aliasesJSON, metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.Type, &e.CanonicalName, &aliasesJSON,
			&e.Description, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.Aliases = unmarshalSlice(aliasesJSON)
		e.Metadata = unmarshalMap(metaJSON)
		e.CreatedAt = strToTime(createdAt)
		e.UpdatedAt = strToTime(updatedAt)
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

func (r *SQLiteRepository) ListEntities(ctx context.Context, opts core.ListEntitiesOptions) ([]core.Entity, error) {
	query := `SELECT id, type, canonical_name, aliases_json, COALESCE(description,''),
		metadata_json, created_at, updated_at
		FROM entities WHERE 1=1`
	var args []interface{}

	if opts.Type != "" {
		query += " AND type = ?"
		args = append(args, opts.Type)
	}
	query += " ORDER BY canonical_name LIMIT ?"
	args = append(args, defaultLimit(opts.Limit))

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []core.Entity
	for rows.Next() {
		var e core.Entity
		var aliasesJSON, metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.Type, &e.CanonicalName, &aliasesJSON,
			&e.Description, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.Aliases = unmarshalSlice(aliasesJSON)
		e.Metadata = unmarshalMap(metaJSON)
		e.CreatedAt = strToTime(createdAt)
		e.UpdatedAt = strToTime(updatedAt)
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

func (r *SQLiteRepository) SearchEntities(ctx context.Context, query string, limit int) ([]core.Entity, error) {
	// Entities don't have an FTS table; search canonical_name and description with LIKE.
	likeQ := "%" + query + "%"
	rows, err := r.QueryContext(ctx, `
		SELECT id, type, canonical_name, aliases_json, COALESCE(description,''),
			metadata_json, created_at, updated_at
		FROM entities
		WHERE canonical_name LIKE ? OR aliases_json LIKE ? OR description LIKE ?
		LIMIT ?`, likeQ, likeQ, likeQ, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []core.Entity
	for rows.Next() {
		var e core.Entity
		var aliasesJSON, metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.Type, &e.CanonicalName, &aliasesJSON,
			&e.Description, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.Aliases = unmarshalSlice(aliasesJSON)
		e.Metadata = unmarshalMap(metaJSON)
		e.CreatedAt = strToTime(createdAt)
		e.UpdatedAt = strToTime(updatedAt)
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

func (r *SQLiteRepository) LinkMemoryEntity(ctx context.Context, memoryID, entityID, role string) error {
	_, err := r.ExecContext(ctx, `
		INSERT OR REPLACE INTO memory_entities (memory_id, entity_id, role)
		VALUES (?,?,?)`, memoryID, entityID, nullStr(role))
	return err
}

func (r *SQLiteRepository) LinkMemoryEntitiesBatch(ctx context.Context, links []core.MemoryEntityLink) error {
	if len(links) == 0 {
		return nil
	}

	valueParts := make([]string, 0, len(links))
	args := make([]interface{}, 0, len(links)*3)
	for _, link := range links {
		if strings.TrimSpace(link.MemoryID) == "" || strings.TrimSpace(link.EntityID) == "" {
			continue
		}
		valueParts = append(valueParts, "(?,?,?)")
		args = append(args, link.MemoryID, link.EntityID, nullStr(link.Role))
	}
	if len(valueParts) == 0 {
		return nil
	}

	query := "INSERT OR IGNORE INTO memory_entities (memory_id, entity_id, role) VALUES " + strings.Join(valueParts, ",")
	_, err := r.ExecContext(ctx, query, args...)
	return err
}

func (r *SQLiteRepository) GetMemoryEntities(ctx context.Context, memoryID string) ([]core.Entity, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT e.id, e.type, e.canonical_name, e.aliases_json, COALESCE(e.description,''),
			e.metadata_json, e.created_at, e.updated_at
		FROM entities e
		JOIN memory_entities me ON me.entity_id = e.id
		WHERE me.memory_id = ?`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []core.Entity
	for rows.Next() {
		var e core.Entity
		var aliasesJSON, metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.Type, &e.CanonicalName, &aliasesJSON,
			&e.Description, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.Aliases = unmarshalSlice(aliasesJSON)
		e.Metadata = unmarshalMap(metaJSON)
		e.CreatedAt = strToTime(createdAt)
		e.UpdatedAt = strToTime(updatedAt)
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

func (r *SQLiteRepository) GetMemoryEntitiesBatch(ctx context.Context, memoryIDs []string) (map[string][]core.Entity, error) {
	result := make(map[string][]core.Entity)
	if len(memoryIDs) == 0 {
		return result, nil
	}

	placeholder := strings.Repeat("?,", len(memoryIDs)-1) + "?"
	query := `
		SELECT me.memory_id, e.id, e.type, e.canonical_name, e.aliases_json, COALESCE(e.description,''),
			e.metadata_json, e.created_at, e.updated_at
		FROM memory_entities me
		JOIN entities e ON me.entity_id = e.id
		WHERE me.memory_id IN (` + placeholder + `)`
	args := make([]interface{}, 0, len(memoryIDs))
	for _, id := range memoryIDs {
		args = append(args, id)
	}

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var memoryID string
		var e core.Entity
		var aliasesJSON, metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&memoryID, &e.ID, &e.Type, &e.CanonicalName, &aliasesJSON,
			&e.Description, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.Aliases = unmarshalSlice(aliasesJSON)
		e.Metadata = unmarshalMap(metaJSON)
		e.CreatedAt = strToTime(createdAt)
		e.UpdatedAt = strToTime(updatedAt)
		result[memoryID] = append(result[memoryID], e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, id := range memoryIDs {
		if _, ok := result[id]; !ok {
			result[id] = nil
		}
	}

	return result, nil
}

func (r *SQLiteRepository) CountMemoryEntityLinks(ctx context.Context, entityID string) (int64, error) {
	row := r.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_entities WHERE entity_id = ?`, entityID)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SQLiteRepository) CountMemoryEntityLinksBatch(ctx context.Context, entityIDs []string) (map[string]int64, error) {
	counts := make(map[string]int64)
	if len(entityIDs) == 0 {
		return counts, nil
	}

	placeholder := strings.Repeat("?,", len(entityIDs)-1) + "?"
	query := `
		SELECT entity_id, COUNT(*) as cnt
		FROM memory_entities
		WHERE entity_id IN (` + placeholder + `)
		GROUP BY entity_id`
	args := make([]interface{}, 0, len(entityIDs))
	for _, id := range entityIDs {
		args = append(args, id)
	}

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var entityID string
		var count int64
		if err := rows.Scan(&entityID, &count); err != nil {
			return nil, err
		}
		counts[entityID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, id := range entityIDs {
		if _, ok := counts[id]; !ok {
			counts[id] = 0
		}
	}

	return counts, nil
}

func (r *SQLiteRepository) CountActiveMemories(ctx context.Context) (int64, error) {
	row := r.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE status = 'active'`)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SQLiteRepository) InsertProject(ctx context.Context, project *core.Project) error {
	if project.ID == "" {
		project.ID = generateID("prj_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO projects (id, name, path, description, metadata_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`,
		project.ID, project.Name,
		nullStr(project.Path), nullStr(project.Description),
		marshalMapJSON(project.Metadata),
		timeToStr(project.CreatedAt), timeToStr(project.UpdatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetProject(ctx context.Context, id string) (*core.Project, error) {
	row := r.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(path,''), COALESCE(description,''), metadata_json, created_at, updated_at
		FROM projects WHERE id = ?`, id)

	var p core.Project
	var metaJSON, createdAt, updatedAt string
	err := row.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &metaJSON, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("project not found: %s", id)
		}
		return nil, err
	}
	p.Metadata = unmarshalMap(metaJSON)
	p.CreatedAt = strToTime(createdAt)
	p.UpdatedAt = strToTime(updatedAt)
	return &p, nil
}

func (r *SQLiteRepository) ListProjects(ctx context.Context) ([]core.Project, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT id, name, COALESCE(path,''), COALESCE(description,''), metadata_json, created_at, updated_at
		FROM projects
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []core.Project
	for rows.Next() {
		var p core.Project
		var metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Metadata = unmarshalMap(metaJSON)
		p.CreatedAt = strToTime(createdAt)
		p.UpdatedAt = strToTime(updatedAt)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (r *SQLiteRepository) DeleteProject(ctx context.Context, id string) error {
	res, err := r.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("project not found: %s", id)
	}
	return nil
}

func (r *SQLiteRepository) InsertRelationship(ctx context.Context, rel *core.Relationship) error {
	if rel.ID == "" {
		rel.ID = generateID("rel_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO relationships (id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`,
		rel.ID, rel.FromEntityID, rel.ToEntityID, rel.RelationshipType,
		marshalMapJSON(rel.Metadata),
		timeToStr(rel.CreatedAt), timeToStr(rel.UpdatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetRelationship(ctx context.Context, id string) (*core.Relationship, error) {
	row := r.QueryRowContext(ctx, `
		SELECT id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at
		FROM relationships WHERE id = ?`, id)

	var rel core.Relationship
	var metaJSON, createdAt, updatedAt string
	err := row.Scan(&rel.ID, &rel.FromEntityID, &rel.ToEntityID, &rel.RelationshipType, &metaJSON, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("relationship not found: %s", id)
		}
		return nil, err
	}
	rel.Metadata = unmarshalMap(metaJSON)
	rel.CreatedAt = strToTime(createdAt)
	rel.UpdatedAt = strToTime(updatedAt)
	return &rel, nil
}

func (r *SQLiteRepository) ListRelationships(ctx context.Context, opts core.ListRelationshipsOptions) ([]core.Relationship, error) {
	query := `SELECT id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at
		FROM relationships WHERE 1=1`
	var args []interface{}

	if opts.EntityID != "" {
		query += " AND (from_entity_id = ? OR to_entity_id = ?)"
		args = append(args, opts.EntityID, opts.EntityID)
	}
	if opts.RelationshipType != "" {
		query += " AND relationship_type = ?"
		args = append(args, opts.RelationshipType)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, defaultLimit(opts.Limit))

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relationships []core.Relationship
	for rows.Next() {
		var rel core.Relationship
		var metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&rel.ID, &rel.FromEntityID, &rel.ToEntityID, &rel.RelationshipType, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		rel.Metadata = unmarshalMap(metaJSON)
		rel.CreatedAt = strToTime(createdAt)
		rel.UpdatedAt = strToTime(updatedAt)
		relationships = append(relationships, rel)
	}
	return relationships, rows.Err()
}

func (r *SQLiteRepository) ListRelationshipsByEntityIDs(ctx context.Context, entityIDs []string) ([]core.Relationship, error) {
	if len(entityIDs) == 0 {
		return nil, nil
	}

	cleanIDs := make([]string, 0, len(entityIDs))
	seen := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cleanIDs = append(cleanIDs, id)
	}
	if len(cleanIDs) == 0 {
		return nil, nil
	}

	inClause := placeholders(len(cleanIDs))
	query := fmt.Sprintf(`SELECT id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at
		FROM relationships
		WHERE from_entity_id IN (%s) OR to_entity_id IN (%s)`, inClause, inClause)

	args := make([]interface{}, 0, len(cleanIDs)*2)
	for _, id := range cleanIDs {
		args = append(args, id)
	}
	for _, id := range cleanIDs {
		args = append(args, id)
	}

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relationships := make([]core.Relationship, 0)
	for rows.Next() {
		var rel core.Relationship
		var metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&rel.ID, &rel.FromEntityID, &rel.ToEntityID, &rel.RelationshipType, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		rel.Metadata = unmarshalMap(metaJSON)
		rel.CreatedAt = strToTime(createdAt)
		rel.UpdatedAt = strToTime(updatedAt)
		relationships = append(relationships, rel)
	}

	return relationships, rows.Err()
}

func (r *SQLiteRepository) InsertRelationshipsBatch(ctx context.Context, rels []*core.Relationship) error {
	if len(rels) == 0 {
		return nil
	}

	valueParts := make([]string, 0, len(rels))
	args := make([]interface{}, 0, len(rels)*7)
	for _, rel := range rels {
		if rel == nil {
			continue
		}
		if rel.ID == "" {
			rel.ID = generateID("rel_")
		}
		if strings.TrimSpace(rel.FromEntityID) == "" || strings.TrimSpace(rel.ToEntityID) == "" || strings.TrimSpace(rel.RelationshipType) == "" {
			continue
		}
		valueParts = append(valueParts, "(?,?,?,?,?,?,?)")
		args = append(args,
			rel.ID,
			rel.FromEntityID,
			rel.ToEntityID,
			rel.RelationshipType,
			marshalMapJSON(rel.Metadata),
			timeToStr(rel.CreatedAt),
			timeToStr(rel.UpdatedAt),
		)
	}
	if len(valueParts) == 0 {
		return nil
	}

	query := "INSERT INTO relationships (id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at) VALUES " + strings.Join(valueParts, ",")
	_, err := r.ExecContext(ctx, query, args...)
	return err
}

func (r *SQLiteRepository) ListRelatedEntities(ctx context.Context, entityID string, depth int) ([]core.RelatedEntity, error) {
	if strings.TrimSpace(entityID) == "" {
		return nil, nil
	}
	if depth <= 0 {
		return nil, nil
	}
	if depth > 3 {
		depth = 3
	}

	rows, err := r.QueryContext(ctx, `
		WITH RECURSIVE related(entity_id, hop, rel_type, visited) AS (
			SELECT ?, 0, '', ',' || ? || ','
			UNION ALL
			SELECT
				CASE
					WHEN r.from_entity_id = related.entity_id THEN r.to_entity_id
					ELSE r.from_entity_id
				END,
				related.hop + 1,
				r.relationship_type,
				related.visited || CASE
					WHEN r.from_entity_id = related.entity_id THEN r.to_entity_id
					ELSE r.from_entity_id
				END || ','
			FROM relationships r
			JOIN related ON (r.from_entity_id = related.entity_id OR r.to_entity_id = related.entity_id)
			WHERE related.hop < ?
				AND related.visited NOT LIKE '%,' || CASE
					WHEN r.from_entity_id = related.entity_id THEN r.to_entity_id
					ELSE r.from_entity_id
				END || ',%'
		)
		SELECT DISTINCT
			e.id,
			e.type,
			e.canonical_name,
			e.aliases_json,
			COALESCE(e.description,''),
			e.metadata_json,
			e.created_at,
			e.updated_at,
			related.hop,
			related.rel_type
		FROM related
		JOIN entities e ON e.id = related.entity_id
		WHERE related.hop > 0
		ORDER BY related.hop ASC`, entityID, entityID, depth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	resultsByEntity := make(map[string]core.RelatedEntity)
	orderedIDs := make([]string, 0)
	for rows.Next() {
		var ent core.Entity
		var aliasesJSON, metaJSON, createdAt, updatedAt, relType string
		var hop int
		if err := rows.Scan(&ent.ID, &ent.Type, &ent.CanonicalName, &aliasesJSON,
			&ent.Description, &metaJSON, &createdAt, &updatedAt, &hop, &relType); err != nil {
			return nil, err
		}
		ent.Aliases = unmarshalSlice(aliasesJSON)
		ent.Metadata = unmarshalMap(metaJSON)
		ent.CreatedAt = strToTime(createdAt)
		ent.UpdatedAt = strToTime(updatedAt)

		existing, ok := resultsByEntity[ent.ID]
		if !ok || hop < existing.HopDistance {
			if !ok {
				orderedIDs = append(orderedIDs, ent.ID)
			}
			resultsByEntity[ent.ID] = core.RelatedEntity{
				Entity:       ent,
				HopDistance:  hop,
				Relationship: relType,
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	related := make([]core.RelatedEntity, 0, len(resultsByEntity))
	for _, id := range orderedIDs {
		related = append(related, resultsByEntity[id])
	}
	return related, nil
}

func (r *SQLiteRepository) RebuildEntityGraphProjection(ctx context.Context) error {
	tx, err := r.Conn().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin projection rebuild transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM entity_graph_projection`); err != nil {
		return fmt.Errorf("truncate entity_graph_projection: %w", err)
	}

	createdAt := timeToStr(time.Now().UTC())
	if _, err := tx.ExecContext(ctx, `
		WITH RECURSIVE walk(root_entity_id, entity_id, hop_distance, relationship_path, visited) AS (
			SELECT id, id, 0, '', ',' || id || ','
			FROM entities

			UNION ALL

			SELECT
				walk.root_entity_id,
				CASE
					WHEN rel.from_entity_id = walk.entity_id THEN rel.to_entity_id
					ELSE rel.from_entity_id
				END,
				walk.hop_distance + 1,
				CASE
					WHEN walk.relationship_path = '' THEN rel.relationship_type
					ELSE walk.relationship_path || '>' || rel.relationship_type
				END,
				walk.visited || CASE
					WHEN rel.from_entity_id = walk.entity_id THEN rel.to_entity_id
					ELSE rel.from_entity_id
				END || ','
			FROM walk
			JOIN relationships rel
				ON rel.from_entity_id = walk.entity_id OR rel.to_entity_id = walk.entity_id
			WHERE walk.hop_distance < 2
				AND walk.visited NOT LIKE '%,' || CASE
					WHEN rel.from_entity_id = walk.entity_id THEN rel.to_entity_id
					ELSE rel.from_entity_id
				END || ',%'
		),
		ranked AS (
			SELECT
				root_entity_id AS entity_id,
				entity_id AS related_entity_id,
				hop_distance,
				relationship_path,
				CASE
					WHEN hop_distance = 1 THEN 1.0
					WHEN hop_distance = 2 THEN 0.5
					ELSE 0.0
				END AS score,
				ROW_NUMBER() OVER (
					PARTITION BY root_entity_id, entity_id
					ORDER BY hop_distance ASC
				) AS row_num
			FROM walk
			WHERE hop_distance > 0
		)
		INSERT INTO entity_graph_projection (
			entity_id,
			related_entity_id,
			hop_distance,
			relationship_path,
			score,
			created_at
		)
		SELECT
			entity_id,
			related_entity_id,
			hop_distance,
			relationship_path,
			score,
			?
		FROM ranked
		WHERE row_num = 1`, createdAt); err != nil {
		return fmt.Errorf("rebuild entity_graph_projection rows: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit projection rebuild: %w", err)
	}

	return nil
}

func (r *SQLiteRepository) ListProjectedRelatedEntities(ctx context.Context, entityID string) ([]core.ProjectedRelation, error) {
	if strings.TrimSpace(entityID) == "" {
		return nil, nil
	}

	rows, err := r.QueryContext(ctx, `
		SELECT related_entity_id, hop_distance, COALESCE(relationship_path, ''), score
		FROM entity_graph_projection
		WHERE entity_id = ?
		ORDER BY hop_distance ASC, score DESC, related_entity_id ASC`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projected := make([]core.ProjectedRelation, 0)
	for rows.Next() {
		var rel core.ProjectedRelation
		if err := rows.Scan(&rel.RelatedEntityID, &rel.HopDistance, &rel.RelationshipPath, &rel.Score); err != nil {
			return nil, err
		}
		projected = append(projected, rel)
	}

	return projected, rows.Err()
}

func (r *SQLiteRepository) DeleteRelationship(ctx context.Context, id string) error {
	res, err := r.ExecContext(ctx, `DELETE FROM relationships WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("relationship not found: %s", id)
	}
	return nil
}

// ---------- Episodes ----------

func (r *SQLiteRepository) InsertEpisode(ctx context.Context, episode *core.Episode) error {
	if episode.ID == "" {
		episode.ID = generateID("epi_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO episodes (id, title, summary, tight_description, scope, project_id,
			session_id, importance, privacy_level, started_at, ended_at,
			source_span_json, source_summary_ids_json, participants_json,
			related_entities_json, outcomes_json, unresolved_items_json,
			metadata_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		episode.ID, episode.Title, episode.Summary, episode.TightDescription,
		string(episode.Scope), nullStr(episode.ProjectID), nullStr(episode.SessionID),
		episode.Importance, string(episode.PrivacyLevel),
		ptrTimeToStr(episode.StartedAt), ptrTimeToStr(episode.EndedAt),
		marshalJSON(episode.SourceSpan),
		marshalSliceJSON(episode.SourceSummaryIDs),
		marshalSliceJSON(episode.Participants),
		marshalSliceJSON(episode.RelatedEntities),
		marshalSliceJSON(episode.Outcomes),
		marshalSliceJSON(episode.UnresolvedItems),
		marshalMapJSON(episode.Metadata),
		timeToStr(episode.CreatedAt), timeToStr(episode.UpdatedAt),
	)
	return err
}

func (r *SQLiteRepository) scanEpisode(scanner interface {
	Scan(dest ...interface{}) error
}) (*core.Episode, error) {
	var ep core.Episode
	var startedAt, endedAt sql.NullString
	var spanJSON, sumIDsJSON, partJSON, relJSON, outJSON, unresJSON, metaJSON string
	var createdAt, updatedAt string

	err := scanner.Scan(
		&ep.ID, &ep.Title, &ep.Summary, &ep.TightDescription, &ep.Scope,
		&ep.ProjectID, &ep.SessionID, &ep.Importance, &ep.PrivacyLevel,
		&startedAt, &endedAt,
		&spanJSON, &sumIDsJSON, &partJSON, &relJSON, &outJSON, &unresJSON,
		&metaJSON, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	ep.StartedAt = nullStrToPtrTime(startedAt)
	ep.EndedAt = nullStrToPtrTime(endedAt)
	_ = json.Unmarshal([]byte(spanJSON), &ep.SourceSpan)
	ep.SourceSummaryIDs = unmarshalSlice(sumIDsJSON)
	ep.Participants = unmarshalSlice(partJSON)
	ep.RelatedEntities = unmarshalSlice(relJSON)
	ep.Outcomes = unmarshalSlice(outJSON)
	ep.UnresolvedItems = unmarshalSlice(unresJSON)
	ep.Metadata = unmarshalMap(metaJSON)
	ep.CreatedAt = strToTime(createdAt)
	ep.UpdatedAt = strToTime(updatedAt)
	return &ep, nil
}

const episodeCols = `id, title, summary, tight_description, scope,
	COALESCE(project_id,''), COALESCE(session_id,''), importance, privacy_level,
	started_at, ended_at,
	source_span_json, source_summary_ids_json, participants_json,
	related_entities_json, outcomes_json, unresolved_items_json,
	metadata_json, created_at, updated_at`

func (r *SQLiteRepository) GetEpisode(ctx context.Context, id string) (*core.Episode, error) {
	row := r.QueryRowContext(ctx,
		"SELECT "+episodeCols+" FROM episodes WHERE id = ?", id)
	ep, err := r.scanEpisode(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("episode not found: %s", id)
		}
		return nil, err
	}
	return ep, nil
}

func (r *SQLiteRepository) ListEpisodes(ctx context.Context, opts core.ListEpisodesOptions) ([]core.Episode, error) {
	query := "SELECT " + episodeCols + " FROM episodes WHERE 1=1"
	var args []interface{}

	if opts.Scope != "" {
		query += " AND scope = ?"
		args = append(args, string(opts.Scope))
	}
	if opts.ProjectID != "" {
		query += " AND project_id = ?"
		args = append(args, opts.ProjectID)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, defaultLimit(opts.Limit))

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []core.Episode
	for rows.Next() {
		ep, err := r.scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, *ep)
	}
	return episodes, rows.Err()
}

func (r *SQLiteRepository) SearchEpisodes(ctx context.Context, query string, limit int) ([]core.Episode, error) {
	sq := sanitizeFTS5Query(query)
	if sq == "" {
		return nil, nil
	}
	q := "SELECT " + prefixCols("e", episodeCols) + `
		FROM episodes_fts f JOIN episodes e ON f.id = e.id
		WHERE episodes_fts MATCH ?
		ORDER BY rank
		LIMIT ?`
	rows, err := r.QueryContext(ctx, q, sq, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []core.Episode
	for rows.Next() {
		ep, err := r.scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, *ep)
	}
	return episodes, rows.Err()
}

// ---------- Artifacts ----------

func (r *SQLiteRepository) InsertArtifact(ctx context.Context, artifact *core.Artifact) error {
	if artifact.ID == "" {
		artifact.ID = generateID("art_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO artifacts (id, kind, source_system, project_id, path, content,
			metadata_json, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		artifact.ID, artifact.Kind,
		nullStr(artifact.SourceSystem), nullStr(artifact.ProjectID),
		nullStr(artifact.Path), nullStr(artifact.Content),
		marshalMapJSON(artifact.Metadata), timeToStr(artifact.CreatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetArtifact(ctx context.Context, id string) (*core.Artifact, error) {
	row := r.QueryRowContext(ctx, `
		SELECT id, kind, COALESCE(source_system,''), COALESCE(project_id,''),
			COALESCE(path,''), COALESCE(content,''), metadata_json, created_at
		FROM artifacts WHERE id = ?`, id)

	var a core.Artifact
	var metaJSON, createdAt string
	err := row.Scan(&a.ID, &a.Kind, &a.SourceSystem, &a.ProjectID,
		&a.Path, &a.Content, &metaJSON, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("artifact not found: %s", id)
		}
		return nil, err
	}
	a.Metadata = unmarshalMap(metaJSON)
	a.CreatedAt = strToTime(createdAt)
	return &a, nil
}

// ---------- Jobs ----------

func (r *SQLiteRepository) InsertJob(ctx context.Context, job *core.Job) error {
	if job.ID == "" {
		job.ID = generateID("job_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO jobs (id, kind, status, payload_json, result_json, error_text,
			scheduled_at, started_at, finished_at, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.Kind, job.Status,
		marshalMapJSON(job.Payload), marshalMapJSON(job.Result),
		nullStr(job.ErrorText),
		ptrTimeToStr(job.ScheduledAt), ptrTimeToStr(job.StartedAt),
		ptrTimeToStr(job.FinishedAt),
		timeToStr(job.CreatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetJob(ctx context.Context, id string) (*core.Job, error) {
	row := r.QueryRowContext(ctx, `
		SELECT id, kind, status, payload_json, result_json, COALESCE(error_text,''),
			scheduled_at, started_at, finished_at, created_at
		FROM jobs WHERE id = ?`, id)

	var j core.Job
	var payJSON, resJSON string
	var scheduledAt, startedAt, finishedAt sql.NullString
	var createdAt string
	err := row.Scan(&j.ID, &j.Kind, &j.Status, &payJSON, &resJSON, &j.ErrorText,
		&scheduledAt, &startedAt, &finishedAt, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("job not found: %s", id)
		}
		return nil, err
	}
	j.Payload = unmarshalMap(payJSON)
	j.Result = unmarshalMap(resJSON)
	j.ScheduledAt = nullStrToPtrTime(scheduledAt)
	j.StartedAt = nullStrToPtrTime(startedAt)
	j.FinishedAt = nullStrToPtrTime(finishedAt)
	j.CreatedAt = strToTime(createdAt)
	return &j, nil
}

func (r *SQLiteRepository) UpdateJob(ctx context.Context, job *core.Job) error {
	_, err := r.ExecContext(ctx, `
		UPDATE jobs SET kind=?, status=?, payload_json=?, result_json=?,
			error_text=?, scheduled_at=?, started_at=?, finished_at=?
		WHERE id=?`,
		job.Kind, job.Status,
		marshalMapJSON(job.Payload), marshalMapJSON(job.Result),
		nullStr(job.ErrorText),
		ptrTimeToStr(job.ScheduledAt), ptrTimeToStr(job.StartedAt),
		ptrTimeToStr(job.FinishedAt),
		job.ID,
	)
	return err
}

func (r *SQLiteRepository) ListJobs(ctx context.Context, opts core.ListJobsOptions) ([]core.Job, error) {
	query := `SELECT id, kind, status, payload_json, result_json, COALESCE(error_text,''),
		scheduled_at, started_at, finished_at, created_at
		FROM jobs WHERE 1=1`
	var args []interface{}

	if opts.Kind != "" {
		query += " AND kind = ?"
		args = append(args, opts.Kind)
	}
	if opts.Status != "" {
		query += " AND status = ?"
		args = append(args, opts.Status)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, defaultLimit(opts.Limit))

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []core.Job
	for rows.Next() {
		var j core.Job
		var payJSON, resJSON string
		var scheduledAt, startedAt, finishedAt sql.NullString
		var createdAt string
		if err := rows.Scan(&j.ID, &j.Kind, &j.Status, &payJSON, &resJSON, &j.ErrorText,
			&scheduledAt, &startedAt, &finishedAt, &createdAt); err != nil {
			return nil, err
		}
		j.Payload = unmarshalMap(payJSON)
		j.Result = unmarshalMap(resJSON)
		j.ScheduledAt = nullStrToPtrTime(scheduledAt)
		j.StartedAt = nullStrToPtrTime(startedAt)
		j.FinishedAt = nullStrToPtrTime(finishedAt)
		j.CreatedAt = strToTime(createdAt)
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// ---------- Ingestion Policies ----------

func (r *SQLiteRepository) InsertIngestionPolicy(ctx context.Context, policy *core.IngestionPolicy) error {
	if policy.ID == "" {
		policy.ID = generateID("pol_")
	}
	_, err := r.ExecContext(ctx, `
		INSERT INTO ingestion_policies (id, pattern_type, pattern, mode,
			priority, match_mode, metadata_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		policy.ID, policy.PatternType, policy.Pattern, policy.Mode,
		policy.Priority, defaultPolicyMatchMode(policy.MatchMode),
		marshalMapJSON(policy.Metadata),
		timeToStr(policy.CreatedAt), timeToStr(policy.UpdatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetIngestionPolicy(ctx context.Context, id string) (*core.IngestionPolicy, error) {
	row := r.QueryRowContext(ctx, `
		SELECT id, pattern_type, pattern, mode, priority, match_mode, metadata_json, created_at, updated_at
		FROM ingestion_policies WHERE id = ?`, id)

	var p core.IngestionPolicy
	var metaJSON, createdAt, updatedAt string
	var priority sql.NullInt64
	var matchMode sql.NullString
	err := row.Scan(&p.ID, &p.PatternType, &p.Pattern, &p.Mode,
		&priority, &matchMode, &metaJSON, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("ingestion policy not found: %s", id)
		}
		return nil, err
	}
	p.Metadata = unmarshalMap(metaJSON)
	p.CreatedAt = strToTime(createdAt)
	p.UpdatedAt = strToTime(updatedAt)
	if priority.Valid {
		p.Priority = int(priority.Int64)
	}
	p.MatchMode = defaultPolicyMatchMode(matchMode.String)
	return &p, nil
}

func (r *SQLiteRepository) ListIngestionPolicies(ctx context.Context) ([]core.IngestionPolicy, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT id, pattern_type, pattern, mode, priority, match_mode, metadata_json, created_at, updated_at
		FROM ingestion_policies
		ORDER BY priority DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []core.IngestionPolicy
	for rows.Next() {
		var p core.IngestionPolicy
		var metaJSON, createdAt, updatedAt string
		var priority sql.NullInt64
		var matchMode sql.NullString
		if err := rows.Scan(&p.ID, &p.PatternType, &p.Pattern, &p.Mode, &priority, &matchMode, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Metadata = unmarshalMap(metaJSON)
		p.CreatedAt = strToTime(createdAt)
		p.UpdatedAt = strToTime(updatedAt)
		if priority.Valid {
			p.Priority = int(priority.Int64)
		}
		p.MatchMode = defaultPolicyMatchMode(matchMode.String)
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

func (r *SQLiteRepository) DeleteIngestionPolicy(ctx context.Context, id string) error {
	res, err := r.ExecContext(ctx, `DELETE FROM ingestion_policies WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("ingestion policy not found: %s", id)
	}
	return nil
}

func (r *SQLiteRepository) MatchIngestionPolicy(ctx context.Context, patternType, value string) (*core.IngestionPolicy, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT id, pattern_type, pattern, mode, priority, match_mode, metadata_json, created_at, updated_at
		FROM ingestion_policies
		WHERE pattern_type = ?
		ORDER BY priority DESC, created_at ASC`, patternType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p core.IngestionPolicy
		var metaJSON, createdAt, updatedAt string
		var priority sql.NullInt64
		var matchMode sql.NullString
		if err := rows.Scan(&p.ID, &p.PatternType, &p.Pattern, &p.Mode,
			&priority, &matchMode, &metaJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Metadata = unmarshalMap(metaJSON)
		p.CreatedAt = strToTime(createdAt)
		p.UpdatedAt = strToTime(updatedAt)
		if priority.Valid {
			p.Priority = int(priority.Int64)
		}
		p.MatchMode = defaultPolicyMatchMode(matchMode.String)

		if matchesPolicy(p, value) {
			return &p, nil
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return nil, nil
}

func defaultPolicyMatchMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return "glob"
	}
	return mode
}

func matchesPolicy(p core.IngestionPolicy, value string) bool {
	switch p.MatchMode {
	case "exact":
		return p.Pattern == value
	case "regex":
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			return false
		}
		return re.MatchString(value)
	default:
		matched, _ := filepath.Match(p.Pattern, value)
		return matched
	}
}

// ---------- Recall History ----------

func (r *SQLiteRepository) RecordRecall(ctx context.Context, sessionID, itemID, itemKind string) error {
	_, err := r.ExecContext(ctx, `
		INSERT INTO recall_history (session_id, item_id, item_kind, shown_at)
		VALUES (?,?,?,?)`,
		sessionID, itemID, itemKind, timeToStr(time.Now().UTC()))
	return err
}

func (r *SQLiteRepository) RecordRecallBatch(ctx context.Context, sessionID string, items []core.RecallRecord) error {
	if len(items) == 0 {
		return nil
	}

	shownAt := timeToStr(time.Now().UTC())
	valueRows := strings.Repeat("(?,?,?,?),", len(items)-1) + "(?,?,?,?)"
	query := `
		INSERT INTO recall_history (session_id, item_id, item_kind, shown_at)
		VALUES ` + valueRows
	args := make([]interface{}, 0, len(items)*4)
	for _, item := range items {
		args = append(args, sessionID, item.ItemID, item.ItemKind, shownAt)
	}

	_, err := r.ExecContext(ctx, query, args...)
	return err
}

func (r *SQLiteRepository) GetRecentRecalls(ctx context.Context, sessionID string, limit int) ([]core.RecallHistoryEntry, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT session_id, item_id, item_kind, shown_at
		FROM recall_history
		WHERE session_id = ?
		ORDER BY shown_at DESC
		LIMIT ?`, sessionID, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []core.RecallHistoryEntry
	for rows.Next() {
		var e core.RecallHistoryEntry
		if err := rows.Scan(&e.SessionID, &e.ItemID, &e.ItemKind, &e.ShownAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *SQLiteRepository) ListMemoryAccessStats(ctx context.Context, since time.Time) ([]core.MemoryAccessStat, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT item_id, COUNT(*) AS access_count, MAX(shown_at) AS last_accessed_at
		FROM recall_history
		WHERE item_kind = 'memory' AND shown_at >= ?
		GROUP BY item_id`, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]core.MemoryAccessStat, 0)
	for rows.Next() {
		var stat core.MemoryAccessStat
		if err := rows.Scan(&stat.MemoryID, &stat.AccessCount, &stat.LastAccessedAt); err != nil {
			return nil, err
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

func (r *SQLiteRepository) CleanupRecallHistory(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -olderThanDays).Format(time.RFC3339)
	result, err := r.ExecContext(ctx, `
		DELETE FROM recall_history WHERE shown_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *SQLiteRepository) PurgeOldEvents(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -olderThanDays).Format(time.RFC3339)
	result, err := r.ExecContext(ctx, `
		DELETE FROM events
		WHERE occurred_at < ? AND reflected_at IS NOT NULL`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *SQLiteRepository) PurgeOldJobs(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -olderThanDays).Format(time.RFC3339)
	result, err := r.ExecContext(ctx, `
		DELETE FROM jobs
		WHERE created_at < ? AND status IN ('completed', 'failed')`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *SQLiteRepository) ExpireRetrievalCache(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.ExecContext(ctx, `
		DELETE FROM retrieval_cache
		WHERE expires_at < ?`, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *SQLiteRepository) PurgeOldRelevanceFeedback(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -olderThanDays).Format(time.RFC3339)
	result, err := r.ExecContext(ctx, `
		DELETE FROM relevance_feedback
		WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *SQLiteRepository) VacuumAnalyze(ctx context.Context) error {
	var firstErr error
	if _, err := r.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		slog.Warn("SQLite vacuum/analyze maintenance step failed", "step", "wal_checkpoint", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	if _, err := r.ExecContext(ctx, `ANALYZE`); err != nil {
		slog.Warn("SQLite vacuum/analyze maintenance step failed", "step", "analyze", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	if _, err := r.ExecContext(ctx, `VACUUM`); err != nil {
		slog.Warn("SQLite vacuum/analyze maintenance step failed", "step", "vacuum", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *SQLiteRepository) InsertRelevanceFeedback(ctx context.Context, sessionID, itemID, itemKind, action string) error {
	_, err := r.ExecContext(ctx, `
		INSERT OR IGNORE INTO relevance_feedback (session_id, item_id, item_kind, action, created_at)
		VALUES (?,?,?,?,?)`,
		sessionID, itemID, itemKind, action, timeToStr(time.Now().UTC()))
	return err
}

func (r *SQLiteRepository) ListRelevanceFeedback(ctx context.Context, itemID string) ([]core.RelevanceFeedbackEntry, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT session_id, item_id, item_kind, action, created_at
		FROM relevance_feedback
		WHERE item_id = ?
		ORDER BY created_at DESC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]core.RelevanceFeedbackEntry, 0)
	for rows.Next() {
		var entry core.RelevanceFeedbackEntry
		var createdAt string
		if err := rows.Scan(&entry.SessionID, &entry.ItemID, &entry.ItemKind, &entry.Action, &createdAt); err != nil {
			return nil, err
		}
		entry.CreatedAt = strToTime(createdAt)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *SQLiteRepository) CountExpandedFeedbackBatch(ctx context.Context, memoryIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(memoryIDs))
	if len(memoryIDs) == 0 {
		return counts, nil
	}
	for _, id := range memoryIDs {
		counts[id] = 0
	}

	placeholder := strings.Repeat("?,", len(memoryIDs)-1) + "?"
	query := `
		SELECT item_id, COUNT(*)
		FROM relevance_feedback
		WHERE item_kind = 'memory' AND action = 'expanded' AND item_id IN (` + placeholder + `)
		GROUP BY item_id`
	args := make([]interface{}, 0, len(memoryIDs))
	for _, id := range memoryIDs {
		args = append(args, id)
	}

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var memoryID string
		var count int
		if err := rows.Scan(&memoryID, &count); err != nil {
			return nil, err
		}
		counts[memoryID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func (r *SQLiteRepository) UpsertEmbedding(ctx context.Context, embedding *core.EmbeddingRecord) error {
	_, err := r.ExecContext(ctx, `
		INSERT INTO embeddings (object_id, object_kind, embedding_json, model, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(object_id, object_kind, model)
		DO UPDATE SET embedding_json=excluded.embedding_json, created_at=excluded.created_at`,
		embedding.ObjectID,
		embedding.ObjectKind,
		marshalEmbeddingJSON(embedding.Vector),
		embedding.Model,
		timeToStr(embedding.CreatedAt),
	)
	return err
}

func (r *SQLiteRepository) GetEmbedding(ctx context.Context, objectID, objectKind, model string) (*core.EmbeddingRecord, error) {
	row := r.QueryRowContext(ctx, `
		SELECT object_id, object_kind, embedding_json, model, created_at
		FROM embeddings
		WHERE object_id = ? AND object_kind = ? AND model = ?`, objectID, objectKind, model)

	var rec core.EmbeddingRecord
	var embeddingJSON, createdAt string
	if err := row.Scan(&rec.ObjectID, &rec.ObjectKind, &embeddingJSON, &rec.Model, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("embedding not found: %s/%s/%s", objectKind, objectID, model)
		}
		return nil, err
	}
	rec.Vector = unmarshalEmbeddingJSON(embeddingJSON)
	rec.CreatedAt = strToTime(createdAt)
	return &rec, nil
}

func (r *SQLiteRepository) GetEmbeddingsBatch(ctx context.Context, objectIDs []string, objectKind, model string) (map[string]core.EmbeddingRecord, error) {
	records := make(map[string]core.EmbeddingRecord)
	if len(objectIDs) == 0 {
		return records, nil
	}

	placeholder := strings.Repeat("?,", len(objectIDs)-1) + "?"
	query := `
		SELECT object_id, object_kind, embedding_json, model, created_at
		FROM embeddings
		WHERE object_id IN (` + placeholder + `) AND object_kind = ? AND model = ?`
	args := make([]interface{}, 0, len(objectIDs)+2)
	for _, id := range objectIDs {
		args = append(args, id)
	}
	args = append(args, objectKind, model)

	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var rec core.EmbeddingRecord
		var embeddingJSON, createdAt string
		if err := rows.Scan(&rec.ObjectID, &rec.ObjectKind, &embeddingJSON, &rec.Model, &createdAt); err != nil {
			return nil, err
		}
		rec.Vector = unmarshalEmbeddingJSON(embeddingJSON)
		rec.CreatedAt = strToTime(createdAt)
		records[rec.ObjectID] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

func (r *SQLiteRepository) ListEmbeddingsByKind(ctx context.Context, objectKind, model string, limit int) ([]core.EmbeddingRecord, error) {
	rows, err := r.QueryContext(ctx, `
		SELECT object_id, object_kind, embedding_json, model, created_at
		FROM embeddings
		WHERE object_kind = ? AND model = ?
		ORDER BY created_at DESC
		LIMIT ?`, objectKind, model, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]core.EmbeddingRecord, 0)
	for rows.Next() {
		var rec core.EmbeddingRecord
		var embeddingJSON, createdAt string
		if err := rows.Scan(&rec.ObjectID, &rec.ObjectKind, &embeddingJSON, &rec.Model, &createdAt); err != nil {
			return nil, err
		}
		rec.Vector = unmarshalEmbeddingJSON(embeddingJSON)
		rec.CreatedAt = strToTime(createdAt)
		records = append(records, rec)
	}
	return records, rows.Err()
}

func (r *SQLiteRepository) DeleteEmbeddings(ctx context.Context, objectID, objectKind, model string) error {
	if model == "" {
		_, err := r.ExecContext(ctx, `DELETE FROM embeddings WHERE object_id = ? AND object_kind = ?`, objectID, objectKind)
		return err
	}
	_, err := r.ExecContext(ctx, `DELETE FROM embeddings WHERE object_id = ? AND object_kind = ? AND model = ?`, objectID, objectKind, model)
	return err
}

func (r *SQLiteRepository) ListUnembeddedMemories(ctx context.Context, model string, limit int) ([]core.Memory, error) {
	query := "SELECT " + memoryCols + ` FROM memories m
		WHERE m.status = 'active'
		AND NOT EXISTS (
			SELECT 1 FROM embeddings e
			WHERE e.object_id = m.id AND e.object_kind = 'memory' AND e.model = ?
		)
		ORDER BY m.created_at DESC LIMIT ?`
	rows, err := r.QueryContext(ctx, query, model, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []core.Memory
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, *m)
	}
	return memories, rows.Err()
}

func (r *SQLiteRepository) ListUnembeddedSummaries(ctx context.Context, model string, limit int) ([]core.Summary, error) {
	query := `SELECT id, kind, scope, COALESCE(project_id,''), COALESCE(session_id,''),
		COALESCE(agent_id,''), COALESCE(title,''), body, tight_description,
		privacy_level, source_span_json, metadata_json, depth, COALESCE(condensed_kind,''), created_at, updated_at
		FROM summaries s
		WHERE NOT EXISTS (
			SELECT 1 FROM embeddings e
			WHERE e.object_id = s.id AND e.object_kind = 'summary' AND e.model = ?
		)
		ORDER BY s.created_at DESC LIMIT ?`
	rows, err := r.QueryContext(ctx, query, model, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []core.Summary
	for rows.Next() {
		var sm core.Summary
		var spanJSON, metaJSON, createdAt, updatedAt string
		if err := rows.Scan(&sm.ID, &sm.Kind, &sm.Scope, &sm.ProjectID, &sm.SessionID,
			&sm.AgentID, &sm.Title, &sm.Body, &sm.TightDescription,
			&sm.PrivacyLevel, &spanJSON, &metaJSON, &sm.Depth, &sm.CondensedKind, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(spanJSON), &sm.SourceSpan)
		sm.Metadata = unmarshalMap(metaJSON)
		sm.CreatedAt = strToTime(createdAt)
		sm.UpdatedAt = strToTime(updatedAt)
		summaries = append(summaries, sm)
	}
	return summaries, rows.Err()
}

func (r *SQLiteRepository) ListUnembeddedEpisodes(ctx context.Context, model string, limit int) ([]core.Episode, error) {
	query := "SELECT " + episodeCols + ` FROM episodes ep
		WHERE NOT EXISTS (
			SELECT 1 FROM embeddings e
			WHERE e.object_id = ep.id AND e.object_kind = 'episode' AND e.model = ?
		)
		ORDER BY ep.created_at DESC LIMIT ?`
	rows, err := r.QueryContext(ctx, query, model, defaultLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	episodes := make([]core.Episode, 0)
	for rows.Next() {
		ep, err := r.scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, *ep)
	}
	return episodes, rows.Err()
}

func normalizeRowsAffected(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func execDeleteCount(ctx context.Context, tx *sql.Tx, table string) (int64, error) {
	result, err := tx.ExecContext(ctx, "DELETE FROM "+table)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return normalizeRowsAffected(n), nil
}

func (r *SQLiteRepository) ResetDerived(ctx context.Context) (*core.ResetDerivedResult, error) {
	if _, err := r.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return nil, fmt.Errorf("disable foreign keys: %w", err)
	}
	foreignKeysRestored := false
	defer func() {
		if foreignKeysRestored {
			return
		}
		if _, err := r.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
			slog.Warn("reset-derived failed to restore foreign_keys pragma", "error", err)
		}
	}()

	tx, err := r.Conn().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reset-derived transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	result := &core.ResetDerivedResult{}
	if result.MemoryEntitiesDeleted, err = execDeleteCount(ctx, tx, "memory_entities"); err != nil {
		return nil, fmt.Errorf("delete memory_entities: %w", err)
	}
	if result.SummaryEdgesDeleted, err = execDeleteCount(ctx, tx, "summary_edges"); err != nil {
		return nil, fmt.Errorf("delete summary_edges: %w", err)
	}
	if result.MemoriesDeleted, err = execDeleteCount(ctx, tx, "memories"); err != nil {
		return nil, fmt.Errorf("delete memories: %w", err)
	}
	if result.ClaimsDeleted, err = execDeleteCount(ctx, tx, "claims"); err != nil {
		return nil, fmt.Errorf("delete claims: %w", err)
	}
	if result.EntitiesDeleted, err = execDeleteCount(ctx, tx, "entities"); err != nil {
		return nil, fmt.Errorf("delete entities: %w", err)
	}
	if result.RelationshipsDeleted, err = execDeleteCount(ctx, tx, "relationships"); err != nil {
		return nil, fmt.Errorf("delete relationships: %w", err)
	}
	if result.SummariesDeleted, err = execDeleteCount(ctx, tx, "summaries"); err != nil {
		return nil, fmt.Errorf("delete summaries: %w", err)
	}
	if result.EpisodesDeleted, err = execDeleteCount(ctx, tx, "episodes"); err != nil {
		return nil, fmt.Errorf("delete episodes: %w", err)
	}
	if result.JobsDeleted, err = execDeleteCount(ctx, tx, "jobs"); err != nil {
		return nil, fmt.Errorf("delete jobs: %w", err)
	}
	if result.MemoriesFTSDeleted, err = execDeleteCount(ctx, tx, "memories_fts"); err != nil {
		return nil, fmt.Errorf("delete memories_fts: %w", err)
	}
	if result.SummariesFTSDeleted, err = execDeleteCount(ctx, tx, "summaries_fts"); err != nil {
		return nil, fmt.Errorf("delete summaries_fts: %w", err)
	}
	if result.EpisodesFTSDeleted, err = execDeleteCount(ctx, tx, "episodes_fts"); err != nil {
		return nil, fmt.Errorf("delete episodes_fts: %w", err)
	}
	if result.EmbeddingsDeleted, err = execDeleteCount(ctx, tx, "embeddings"); err != nil {
		return nil, fmt.Errorf("delete embeddings: %w", err)
	}
	if result.RetrievalCacheDeleted, err = execDeleteCount(ctx, tx, "retrieval_cache"); err != nil {
		return nil, fmt.Errorf("delete retrieval_cache: %w", err)
	}
	if result.RecallHistoryDeleted, err = execDeleteCount(ctx, tx, "recall_history"); err != nil {
		return nil, fmt.Errorf("delete recall_history: %w", err)
	}
	if result.RelevanceFeedbackDeleted, err = execDeleteCount(ctx, tx, "relevance_feedback"); err != nil {
		return nil, fmt.Errorf("delete relevance_feedback: %w", err)
	}
	if result.EntityGraphProjectionDeleted, err = execDeleteCount(ctx, tx, "entity_graph_projection"); err != nil {
		return nil, fmt.Errorf("delete entity_graph_projection: %w", err)
	}
	updateResult, err := tx.ExecContext(ctx, `UPDATE events SET reflected_at = NULL`)
	if err != nil {
		return nil, fmt.Errorf("reset events reflected_at: %w", err)
	}
	if result.EventsReset, err = updateResult.RowsAffected(); err != nil {
		return nil, fmt.Errorf("read events_reset rows affected: %w", err)
	}
	result.EventsReset = normalizeRowsAffected(result.EventsReset)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reset-derived transaction: %w", err)
	}

	if _, err := r.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return nil, fmt.Errorf("restore foreign keys: %w", err)
	}
	foreignKeysRestored = true

	if _, err := r.ExecContext(ctx, `VACUUM`); err != nil {
		slog.Warn("reset-derived VACUUM failed after successful cleanup", "error", err)
	}

	return result, nil
}

// ---------- Counts ----------

func (r *SQLiteRepository) countTable(ctx context.Context, table string) (int64, error) {
	row := r.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
	var n int64
	err := row.Scan(&n)
	return n, err
}

func (r *SQLiteRepository) CountEvents(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "events")
}

func (r *SQLiteRepository) CountMemories(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "memories")
}

func (r *SQLiteRepository) CountSummaries(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "summaries")
}

func (r *SQLiteRepository) CountEpisodes(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "episodes")
}

func (r *SQLiteRepository) CountEntities(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "entities")
}

// ---------- Index Management ----------

func (r *SQLiteRepository) RebuildFTSIndexes(ctx context.Context) error {
	stmts := []string{
		// Clear all FTS content
		"DELETE FROM memories_fts",
		"DELETE FROM summaries_fts",
		"DELETE FROM episodes_fts",
		"DELETE FROM events_fts",

		// Re-insert from canonical tables
		`INSERT INTO memories_fts(id, type, subject, body, tight_description, tags)
		 SELECT id, type, subject, body, tight_description, tags_json FROM memories`,

		`INSERT INTO summaries_fts(id, kind, title, body, tight_description)
		 SELECT id, kind, title, body, tight_description FROM summaries`,

		`INSERT INTO episodes_fts(id, title, summary, tight_description)
		 SELECT id, title, summary, tight_description FROM episodes`,

		`INSERT INTO events_fts(id, kind, content)
		 SELECT id, kind, content FROM events`,
	}
	for _, stmt := range stmts {
		if _, err := r.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("rebuild FTS: %w", err)
		}
	}
	return nil
}
