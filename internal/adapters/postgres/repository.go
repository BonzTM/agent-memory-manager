package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

var _ core.Repository = (*Repository)(nil)

func NewRepository() *Repository {
	return &Repository{}
}

func generateID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("generateID: crypto/rand failed: %v", err))
	}
	return prefix + hex.EncodeToString(b)
}

func defaultLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return limit
}

func (r *Repository) Open(ctx context.Context, dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return wrapErr("open postgres", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return wrapErr("ping postgres", err)
	}
	r.db = db
	return nil
}

func (r *Repository) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *Repository) Migrate(ctx context.Context) error {
	if r.db == nil {
		return fmt.Errorf("postgres not open")
	}
	return Migrate(ctx, r.db)
}

func (r *Repository) IsInitialized(ctx context.Context) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'schema_version'
		)`).Scan(&exists)
	if err != nil {
		return false, wrapErr("is initialized", err)
	}
	if !exists {
		return false, nil
	}
	var ver int
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&ver); err != nil {
		return false, wrapErr("is initialized", err)
	}
	return ver > 0, nil
}

func marshalMapJSON(m map[string]string) []byte {
	if m == nil {
		return []byte(`{}`)
	}
	b, _ := json.Marshal(m)
	return b
}

func marshalSourceSpan(span core.SourceSpan) []byte {
	b, _ := json.Marshal(span)
	return b
}

func parseMapJSON(data []byte) map[string]string {
	if len(data) == 0 {
		return map[string]string{}
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]string{}
	}
	if out == nil {
		return map[string]string{}
	}
	return out
}

func unmarshalMapJSON(data []byte) map[string]string {
	return parseMapJSON(data)
}

func parseNullTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func parseSourceSpanJSON(data []byte) core.SourceSpan {
	if len(data) == 0 {
		return core.SourceSpan{}
	}
	var out core.SourceSpan
	_ = json.Unmarshal(data, &out)
	return out
}

func encodeVector(vec []float32) []byte {
	if len(vec) == 0 {
		return nil
	}
	b := make([]byte, 4+4*len(vec))
	binary.LittleEndian.PutUint32(b[:4], uint32(len(vec)))
	off := 4
	for _, v := range vec {
		binary.LittleEndian.PutUint32(b[off:off+4], mathFloat32bits(v))
		off += 4
	}
	return b
}

func decodeVector(data []byte) []float32 {
	if len(data) < 4 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(data[:4]))
	if n <= 0 || len(data) < 4+n*4 {
		return nil
	}
	vec := make([]float32, 0, n)
	off := 4
	for i := 0; i < n; i++ {
		v := mathFloat32frombits(binary.LittleEndian.Uint32(data[off : off+4]))
		vec = append(vec, v)
		off += 4
	}
	return vec
}

func mathFloat32bits(f float32) uint32     { return math.Float32bits(f) }
func mathFloat32frombits(b uint32) float32 { return math.Float32frombits(b) }

func errNotFound(kind, id string) error {
	return fmt.Errorf("%w: %s %s", core.ErrNotFound, kind, id)
}

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

func placeholders(start, n int) string {
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, fmt.Sprintf("$%d", start+i))
	}
	return strings.Join(parts, ",")
}

func (r *Repository) InsertEvent(ctx context.Context, event *core.Event) error {
	if event.ID == "" {
		event.ID = generateID("evt_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO events (
			id, kind, source_system, surface, session_id, project_id,
			agent_id, actor_type, actor_id, privacy_level, content,
			metadata_json, hash, occurred_at, ingested_at
		)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),
			NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11,$12::jsonb,
			NULLIF($13,''),$14,$15)`,
		event.ID, event.Kind, event.SourceSystem, event.Surface, event.SessionID, event.ProjectID,
		event.AgentID, event.ActorType, event.ActorID, string(event.PrivacyLevel), event.Content,
		marshalMapJSON(event.Metadata), event.Hash, event.OccurredAt.UTC(), event.IngestedAt.UTC(),
	)
	return wrapErr("insert event", err)
}

func (r *Repository) GetEvent(ctx context.Context, id string) (*core.Event, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT sequence_id, id, kind, source_system,
			COALESCE(surface,''), COALESCE(session_id,''), COALESCE(project_id,''),
			COALESCE(agent_id,''), COALESCE(actor_type,''), COALESCE(actor_id,''),
			privacy_level, content, metadata_json, COALESCE(hash,''), occurred_at, ingested_at, reflected_at
		FROM events WHERE id = $1`, id)
	var e core.Event
	var metadata []byte
	var reflectedAt sql.NullTime
	if err := row.Scan(&e.SequenceID, &e.ID, &e.Kind, &e.SourceSystem,
		&e.Surface, &e.SessionID, &e.ProjectID, &e.AgentID, &e.ActorType, &e.ActorID,
		&e.PrivacyLevel, &e.Content, &metadata, &e.Hash, &e.OccurredAt, &e.IngestedAt, &reflectedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("event", id)
		}
		return nil, wrapErr("get event", err)
	}
	e.Metadata = parseMapJSON(metadata)
	if reflectedAt.Valid {
		t := reflectedAt.Time.UTC()
		e.ReflectedAt = &t
	}
	return &e, nil
}

func (r *Repository) UpdateEvent(ctx context.Context, event *core.Event) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE events SET
			kind = $1, source_system = $2, surface = NULLIF($3,''),
			session_id = NULLIF($4,''), project_id = NULLIF($5,''),
			agent_id = NULLIF($6,''), actor_type = NULLIF($7,''), actor_id = NULLIF($8,''),
			privacy_level = $9, content = $10, metadata_json = $11::jsonb,
			hash = NULLIF($12,''), occurred_at = $13, ingested_at = $14, reflected_at = $15
		WHERE id = $16`,
		event.Kind, event.SourceSystem, event.Surface, event.SessionID, event.ProjectID,
		event.AgentID, event.ActorType, event.ActorID, string(event.PrivacyLevel), event.Content,
		marshalMapJSON(event.Metadata), event.Hash, event.OccurredAt.UTC(), event.IngestedAt.UTC(), event.ReflectedAt, event.ID,
	)
	return wrapErr("update event", err)
}

func (r *Repository) ListEvents(ctx context.Context, opts core.ListEventsOptions) ([]core.Event, error) {
	query := `SELECT sequence_id, id, kind, source_system,
		COALESCE(surface,''), COALESCE(session_id,''), COALESCE(project_id,''),
		COALESCE(agent_id,''), COALESCE(actor_type,''), COALESCE(actor_id,''),
		privacy_level, content, metadata_json, COALESCE(hash,''), occurred_at, ingested_at, reflected_at
		FROM events WHERE 1=1`
	args := make([]any, 0)
	i := 1
	if opts.SessionID != "" {
		query += fmt.Sprintf(" AND session_id = $%d", i)
		args = append(args, opts.SessionID)
		i++
	}
	if opts.ProjectID != "" {
		query += fmt.Sprintf(" AND project_id = $%d", i)
		args = append(args, opts.ProjectID)
		i++
	}
	if opts.Kind != "" {
		query += fmt.Sprintf(" AND kind = $%d", i)
		args = append(args, opts.Kind)
		i++
	}
	if opts.Before != "" {
		query += fmt.Sprintf(" AND occurred_at < $%d", i)
		args = append(args, opts.Before)
		i++
	}
	if opts.BeforeSequenceID > 0 {
		query += fmt.Sprintf(" AND sequence_id <= $%d", i)
		args = append(args, opts.BeforeSequenceID)
		i++
	}
	if opts.After != "" {
		query += fmt.Sprintf(" AND occurred_at > $%d", i)
		args = append(args, opts.After)
		i++
	}
	if opts.AfterSequenceID > 0 {
		query += fmt.Sprintf(" AND sequence_id > $%d", i)
		args = append(args, opts.AfterSequenceID)
		i++
	}
	if opts.UnreflectedOnly {
		query += " AND reflected_at IS NULL"
	}
	if opts.AfterSequenceID > 0 || opts.BeforeSequenceID > 0 || opts.UnreflectedOnly {
		query += " ORDER BY sequence_id ASC"
	} else {
		query += " ORDER BY occurred_at DESC, id DESC"
	}
	query += fmt.Sprintf(" LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("list events", err)
	}
	defer rows.Close()

	out := make([]core.Event, 0)
	for rows.Next() {
		var e core.Event
		var metadata []byte
		var reflectedAt sql.NullTime
		if err := rows.Scan(&e.SequenceID, &e.ID, &e.Kind, &e.SourceSystem,
			&e.Surface, &e.SessionID, &e.ProjectID, &e.AgentID, &e.ActorType, &e.ActorID,
			&e.PrivacyLevel, &e.Content, &metadata, &e.Hash, &e.OccurredAt, &e.IngestedAt, &reflectedAt); err != nil {
			return nil, wrapErr("list events", err)
		}
		e.Metadata = parseMapJSON(metadata)
		if reflectedAt.Valid {
			t := reflectedAt.Time.UTC()
			e.ReflectedAt = &t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) SearchEvents(ctx context.Context, query string, limit int) ([]core.Event, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT sequence_id, id, kind, source_system,
			COALESCE(surface,''), COALESCE(session_id,''), COALESCE(project_id,''),
			COALESCE(agent_id,''), COALESCE(actor_type,''), COALESCE(actor_id,''),
			privacy_level, content, metadata_json, COALESCE(hash,''), occurred_at, ingested_at, reflected_at
		FROM events
		WHERE events_fts @@ plainto_tsquery('simple', $1)
		ORDER BY ts_rank(events_fts, plainto_tsquery('simple', $1)) DESC, occurred_at DESC
		LIMIT $2`, query, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("search events", err)
	}
	defer rows.Close()
	out := make([]core.Event, 0)
	for rows.Next() {
		var e core.Event
		var metadata []byte
		var reflectedAt sql.NullTime
		if err := rows.Scan(&e.SequenceID, &e.ID, &e.Kind, &e.SourceSystem,
			&e.Surface, &e.SessionID, &e.ProjectID, &e.AgentID, &e.ActorType, &e.ActorID,
			&e.PrivacyLevel, &e.Content, &metadata, &e.Hash, &e.OccurredAt, &e.IngestedAt, &reflectedAt); err != nil {
			return nil, wrapErr("search events", err)
		}
		e.Metadata = parseMapJSON(metadata)
		if reflectedAt.Valid {
			t := reflectedAt.Time.UTC()
			e.ReflectedAt = &t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) CountUnreflectedEvents(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE reflected_at IS NULL`).Scan(&n)
	return n, wrapErr("count unreflected events", err)
}

func (r *Repository) ClaimUnreflectedEvents(ctx context.Context, limit int) ([]core.Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, wrapErr("claim unreflected events", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
		WITH claimed AS (
			SELECT id
			FROM events
			WHERE reflected_at IS NULL
			ORDER BY sequence_id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE events e
		SET reflected_at = NOW()
		FROM claimed
		WHERE e.id = claimed.id
		RETURNING e.sequence_id, e.id, e.kind, e.source_system,
			COALESCE(e.surface,''), COALESCE(e.session_id,''), COALESCE(e.project_id,''),
			COALESCE(e.agent_id,''), COALESCE(e.actor_type,''), COALESCE(e.actor_id,''),
			e.privacy_level, e.content, e.metadata_json, COALESCE(e.hash,''), e.occurred_at, e.ingested_at`, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("claim unreflected events", err)
	}
	defer rows.Close()
	out := make([]core.Event, 0)
	for rows.Next() {
		var e core.Event
		var metadata []byte
		if err := rows.Scan(&e.SequenceID, &e.ID, &e.Kind, &e.SourceSystem,
			&e.Surface, &e.SessionID, &e.ProjectID, &e.AgentID, &e.ActorType, &e.ActorID,
			&e.PrivacyLevel, &e.Content, &metadata, &e.Hash, &e.OccurredAt, &e.IngestedAt); err != nil {
			return nil, wrapErr("claim unreflected events", err)
		}
		e.Metadata = parseMapJSON(metadata)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("claim unreflected events", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapErr("claim unreflected events", err)
	}
	return out, nil
}

func (r *Repository) InsertSummary(ctx context.Context, summary *core.Summary) error {
	if summary.ID == "" {
		summary.ID = generateID("sum_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO summaries (id, kind, scope, project_id, session_id, agent_id,
			title, body, tight_description, privacy_level, source_span_json,
			metadata_json, depth, condensed_kind, created_at, updated_at)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11::jsonb,$12::jsonb,$13,$14,$15,$16)`,
		summary.ID, summary.Kind, string(summary.Scope), summary.ProjectID, summary.SessionID, summary.AgentID,
		summary.Title, summary.Body, summary.TightDescription, string(summary.PrivacyLevel),
		marshalSourceSpan(summary.SourceSpan), marshalMapJSON(summary.Metadata), summary.Depth, summary.CondensedKind, summary.CreatedAt.UTC(), summary.UpdatedAt.UTC(),
	)
	return wrapErr("insert summary", err)
}

func (r *Repository) GetSummary(ctx context.Context, id string) (*core.Summary, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, kind, scope, COALESCE(project_id,''), COALESCE(session_id,''), COALESCE(agent_id,''),
			COALESCE(title,''), body, tight_description, privacy_level,
			source_span_json, metadata_json, depth, COALESCE(condensed_kind,''), created_at, updated_at
		FROM summaries WHERE id = $1`, id)
	var s core.Summary
	var span, meta []byte
	if err := row.Scan(&s.ID, &s.Kind, &s.Scope, &s.ProjectID, &s.SessionID, &s.AgentID,
		&s.Title, &s.Body, &s.TightDescription, &s.PrivacyLevel, &span, &meta, &s.Depth, &s.CondensedKind, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("summary", id)
		}
		return nil, wrapErr("get summary", err)
	}
	s.SourceSpan = parseSourceSpanJSON(span)
	s.Metadata = parseMapJSON(meta)
	return &s, nil
}

func (r *Repository) ListSummaries(ctx context.Context, opts core.ListSummariesOptions) ([]core.Summary, error) {
	query := `SELECT id, kind, scope, COALESCE(project_id,''), COALESCE(session_id,''), COALESCE(agent_id,''),
		COALESCE(title,''), body, tight_description, privacy_level,
		source_span_json, metadata_json, depth, COALESCE(condensed_kind,''), created_at, updated_at
		FROM summaries WHERE 1=1`
	args := make([]any, 0)
	i := 1
	if opts.Kind != "" {
		query += fmt.Sprintf(" AND kind = $%d", i)
		args = append(args, opts.Kind)
		i++
	}
	if opts.Scope != "" {
		query += fmt.Sprintf(" AND scope = $%d", i)
		args = append(args, string(opts.Scope))
		i++
	}
	if opts.ProjectID != "" {
		query += fmt.Sprintf(" AND project_id = $%d", i)
		args = append(args, opts.ProjectID)
		i++
	}
	if opts.SessionID != "" {
		query += fmt.Sprintf(" AND session_id = $%d", i)
		args = append(args, opts.SessionID)
		i++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("list summaries", err)
	}
	defer rows.Close()
	out := make([]core.Summary, 0)
	for rows.Next() {
		var s core.Summary
		var span, meta []byte
		if err := rows.Scan(&s.ID, &s.Kind, &s.Scope, &s.ProjectID, &s.SessionID, &s.AgentID,
			&s.Title, &s.Body, &s.TightDescription, &s.PrivacyLevel, &span, &meta, &s.Depth, &s.CondensedKind, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, wrapErr("list summaries", err)
		}
		s.SourceSpan = parseSourceSpanJSON(span)
		s.Metadata = parseMapJSON(meta)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) SearchSummaries(ctx context.Context, query string, limit int) ([]core.Summary, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, kind, scope, COALESCE(project_id,''), COALESCE(session_id,''), COALESCE(agent_id,''),
			COALESCE(title,''), body, tight_description, privacy_level,
			source_span_json, metadata_json, depth, COALESCE(condensed_kind,''), created_at, updated_at
		FROM summaries
		WHERE summaries_fts @@ plainto_tsquery('simple', $1)
		ORDER BY ts_rank(summaries_fts, plainto_tsquery('simple', $1)) DESC, created_at DESC
		LIMIT $2`, query, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("search summaries", err)
	}
	defer rows.Close()
	out := make([]core.Summary, 0)
	for rows.Next() {
		var s core.Summary
		var span, meta []byte
		if err := rows.Scan(&s.ID, &s.Kind, &s.Scope, &s.ProjectID, &s.SessionID, &s.AgentID,
			&s.Title, &s.Body, &s.TightDescription, &s.PrivacyLevel, &span, &meta, &s.Depth, &s.CondensedKind, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, wrapErr("search summaries", err)
		}
		s.SourceSpan = parseSourceSpanJSON(span)
		s.Metadata = parseMapJSON(meta)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) GetSummaryChildren(ctx context.Context, parentID string) ([]core.SummaryEdge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT parent_summary_id, child_kind, child_id, COALESCE(edge_order, 0)
		FROM summary_edges WHERE parent_summary_id = $1 ORDER BY edge_order`, parentID)
	if err != nil {
		return nil, wrapErr("get summary children", err)
	}
	defer rows.Close()
	out := make([]core.SummaryEdge, 0)
	for rows.Next() {
		var e core.SummaryEdge
		if err := rows.Scan(&e.ParentSummaryID, &e.ChildKind, &e.ChildID, &e.EdgeOrder); err != nil {
			return nil, wrapErr("get summary children", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) ListParentedSummaryIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT child_id FROM summary_edges WHERE child_kind = 'summary'`)
	if err != nil {
		return nil, wrapErr("list parented summary ids", err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, wrapErr("list parented summary ids", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (r *Repository) InsertSummaryEdge(ctx context.Context, edge *core.SummaryEdge) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO summary_edges (parent_summary_id, child_kind, child_id, edge_order)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (parent_summary_id, child_kind, child_id)
		DO UPDATE SET edge_order = EXCLUDED.edge_order`,
		edge.ParentSummaryID, edge.ChildKind, edge.ChildID, edge.EdgeOrder)
	return wrapErr("insert summary edge", err)
}

func (r *Repository) InsertMemory(ctx context.Context, memory *core.Memory) error {
	if memory.ID == "" {
		memory.ID = generateID("mem_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO memories (
			id, type, scope, project_id, session_id, agent_id, subject, body, tight_description,
			confidence, importance, privacy_level, status,
			observed_at, created_at, updated_at, valid_from, valid_to, last_confirmed_at,
			supersedes, superseded_by, superseded_at,
			source_event_ids, source_summary_ids, source_artifact_ids, tags, metadata_json
		)
		VALUES (
			$1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,
			$10,$11,$12,$13,
			$14,$15,$16,$17,$18,$19,
			NULLIF($20,''),NULLIF($21,''),$22,
			$23,$24,$25,$26,$27::jsonb
		)`,
		memory.ID, string(memory.Type), string(memory.Scope), memory.ProjectID, memory.SessionID, memory.AgentID,
		memory.Subject, memory.Body, memory.TightDescription,
		memory.Confidence, memory.Importance, string(memory.PrivacyLevel), string(memory.Status),
		memory.ObservedAt, memory.CreatedAt.UTC(), memory.UpdatedAt.UTC(), memory.ValidFrom, memory.ValidTo, memory.LastConfirmedAt,
		memory.Supersedes, memory.SupersededBy, memory.SupersededAt,
		pq.Array(memory.SourceEventIDs), pq.Array(memory.SourceSummaryIDs), pq.Array(memory.SourceArtifactIDs), pq.Array(memory.Tags), marshalMapJSON(memory.Metadata),
	)
	return wrapErr("insert memory", err)
}

const memoryCols = `id, type, scope, COALESCE(project_id,''), COALESCE(session_id,''), COALESCE(agent_id,''),
	COALESCE(subject,''), body, tight_description, confidence, importance, privacy_level, status,
	observed_at, created_at, updated_at, valid_from, valid_to, last_confirmed_at,
	COALESCE(supersedes,''), COALESCE(superseded_by,''), superseded_at,
	source_event_ids, source_summary_ids, source_artifact_ids, tags, metadata_json`

func (r *Repository) scanMemory(scanner interface{ Scan(dest ...any) error }) (*core.Memory, error) {
	var m core.Memory
	var observedAt, validFrom, validTo, lastConfirmedAt, supersededAt sql.NullTime
	var sourceEventIDs, sourceSummaryIDs, sourceArtifactIDs, tags pq.StringArray
	var meta []byte
	if err := scanner.Scan(&m.ID, &m.Type, &m.Scope, &m.ProjectID, &m.SessionID, &m.AgentID,
		&m.Subject, &m.Body, &m.TightDescription, &m.Confidence, &m.Importance, &m.PrivacyLevel, &m.Status,
		&observedAt, &m.CreatedAt, &m.UpdatedAt, &validFrom, &validTo, &lastConfirmedAt,
		&m.Supersedes, &m.SupersededBy, &supersededAt,
		pq.Array(&sourceEventIDs), pq.Array(&sourceSummaryIDs), pq.Array(&sourceArtifactIDs), pq.Array(&tags), &meta); err != nil {
		return nil, wrapErr("scan memory", err)
	}
	if observedAt.Valid {
		t := observedAt.Time.UTC()
		m.ObservedAt = &t
	}
	if validFrom.Valid {
		t := validFrom.Time.UTC()
		m.ValidFrom = &t
	}
	if validTo.Valid {
		t := validTo.Time.UTC()
		m.ValidTo = &t
	}
	if lastConfirmedAt.Valid {
		t := lastConfirmedAt.Time.UTC()
		m.LastConfirmedAt = &t
	}
	if supersededAt.Valid {
		t := supersededAt.Time.UTC()
		m.SupersededAt = &t
	}
	m.SourceEventIDs = []string(sourceEventIDs)
	m.SourceSummaryIDs = []string(sourceSummaryIDs)
	m.SourceArtifactIDs = []string(sourceArtifactIDs)
	m.Tags = []string(tags)
	m.Metadata = parseMapJSON(meta)
	return &m, nil
}

func (r *Repository) GetMemory(ctx context.Context, id string) (*core.Memory, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE id = $1`, id)
	m, err := r.scanMemory(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("memory", id)
		}
		return nil, wrapErr("get memory", err)
	}
	return m, nil
}

func (r *Repository) GetMemoriesByIDs(ctx context.Context, ids []string) (map[string]*core.Memory, error) {
	out := make(map[string]*core.Memory, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+memoryCols+` FROM memories WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, wrapErr("get memories by ids", err)
	}
	defer rows.Close()
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, wrapErr("get memories by ids", err)
		}
		out[m.ID] = m
	}
	return out, rows.Err()
}

func (r *Repository) UpdateMemory(ctx context.Context, memory *core.Memory) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE memories SET
			type = $1, scope = $2, project_id = NULLIF($3,''), session_id = NULLIF($4,''), agent_id = NULLIF($5,''),
			subject = NULLIF($6,''), body = $7, tight_description = $8, confidence = $9, importance = $10,
			privacy_level = $11, status = $12, observed_at = $13, updated_at = $14,
			valid_from = $15, valid_to = $16, last_confirmed_at = $17,
			supersedes = NULLIF($18,''), superseded_by = NULLIF($19,''), superseded_at = $20,
			source_event_ids = $21, source_summary_ids = $22, source_artifact_ids = $23,
			tags = $24, metadata_json = $25::jsonb
		WHERE id = $26`,
		string(memory.Type), string(memory.Scope), memory.ProjectID, memory.SessionID, memory.AgentID,
		memory.Subject, memory.Body, memory.TightDescription, memory.Confidence, memory.Importance,
		string(memory.PrivacyLevel), string(memory.Status), memory.ObservedAt, memory.UpdatedAt.UTC(),
		memory.ValidFrom, memory.ValidTo, memory.LastConfirmedAt,
		memory.Supersedes, memory.SupersededBy, memory.SupersededAt,
		pq.Array(memory.SourceEventIDs), pq.Array(memory.SourceSummaryIDs), pq.Array(memory.SourceArtifactIDs),
		pq.Array(memory.Tags), marshalMapJSON(memory.Metadata), memory.ID,
	)
	return wrapErr("update memory", err)
}

func (r *Repository) UpdateMemoriesBatch(ctx context.Context, memories []*core.Memory) error {
	if len(memories) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapErr("begin update memories batch", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE memories SET
			type = $1, scope = $2, project_id = NULLIF($3,''), session_id = NULLIF($4,''), agent_id = NULLIF($5,''),
			subject = NULLIF($6,''), body = $7, tight_description = $8, confidence = $9, importance = $10,
			privacy_level = $11, status = $12, observed_at = $13, updated_at = $14,
			valid_from = $15, valid_to = $16, last_confirmed_at = $17,
			supersedes = NULLIF($18,''), superseded_by = NULLIF($19,''), superseded_at = $20,
			source_event_ids = $21, source_summary_ids = $22, source_artifact_ids = $23,
			tags = $24, metadata_json = $25::jsonb
		WHERE id = $26`)
	if err != nil {
		return wrapErr("prepare update memories batch", err)
	}
	defer stmt.Close()

	for _, memory := range memories {
		if _, err := stmt.ExecContext(ctx,
			string(memory.Type), string(memory.Scope), memory.ProjectID, memory.SessionID, memory.AgentID,
			memory.Subject, memory.Body, memory.TightDescription, memory.Confidence, memory.Importance,
			string(memory.PrivacyLevel), string(memory.Status), memory.ObservedAt, memory.UpdatedAt.UTC(),
			memory.ValidFrom, memory.ValidTo, memory.LastConfirmedAt,
			memory.Supersedes, memory.SupersededBy, memory.SupersededAt,
			pq.Array(memory.SourceEventIDs), pq.Array(memory.SourceSummaryIDs), pq.Array(memory.SourceArtifactIDs),
			pq.Array(memory.Tags), marshalMapJSON(memory.Metadata), memory.ID,
		); err != nil {
			return wrapErr(fmt.Sprintf("update memory %s in batch", memory.ID), err)
		}
	}

	return tx.Commit()
}

func (r *Repository) ListMemories(ctx context.Context, opts core.ListMemoriesOptions) ([]core.Memory, error) {
	query := `SELECT ` + memoryCols + ` FROM memories WHERE 1=1`
	args := make([]any, 0)
	i := 1
	if opts.Type != "" {
		query += fmt.Sprintf(" AND type = $%d", i)
		args = append(args, string(opts.Type))
		i++
	}
	if opts.Scope != "" {
		query += fmt.Sprintf(" AND scope = $%d", i)
		args = append(args, string(opts.Scope))
		i++
	}
	if opts.ProjectID != "" {
		query += fmt.Sprintf(" AND project_id = $%d", i)
		args = append(args, opts.ProjectID)
		i++
	}
	if opts.AgentID != "" {
		query += fmt.Sprintf(" AND (agent_id = $%d OR agent_id IS NULL OR privacy_level IN ('shared','public_safe'))", i)
		args = append(args, opts.AgentID)
		i++
	}
	if opts.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", i)
		args = append(args, string(opts.Status))
		i++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("list memories", err)
	}
	defer rows.Close()
	out := make([]core.Memory, 0)
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, wrapErr("list memories", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *Repository) SearchMemories(ctx context.Context, query string, opts core.ListMemoriesOptions) ([]core.Memory, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	q := `SELECT ` + memoryCols + ` FROM memories WHERE memories_fts @@ plainto_tsquery('simple', $1)`
	args := []any{query}
	i := 2
	if opts.Status != "" {
		q += fmt.Sprintf(" AND status = $%d", i)
		args = append(args, string(opts.Status))
		i++
	}
	if opts.Type != "" {
		q += fmt.Sprintf(" AND type = $%d", i)
		args = append(args, string(opts.Type))
		i++
	}
	if opts.Scope != "" {
		q += fmt.Sprintf(" AND scope = $%d", i)
		args = append(args, string(opts.Scope))
		i++
	}
	if opts.ProjectID != "" {
		q += fmt.Sprintf(" AND project_id = $%d", i)
		args = append(args, opts.ProjectID)
		i++
	}
	if opts.AgentID != "" {
		q += fmt.Sprintf(" AND (agent_id = $%d OR agent_id IS NULL OR privacy_level IN ('shared','public_safe'))", i)
		args = append(args, opts.AgentID)
		i++
	}
	q += fmt.Sprintf(" ORDER BY ts_rank(memories_fts, plainto_tsquery('simple', $1)) DESC, created_at DESC LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("search memories", err)
	}
	defer rows.Close()
	out := make([]core.Memory, 0)
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, wrapErr("search memories", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *Repository) SearchMemoriesFuzzy(ctx context.Context, query string, opts core.ListMemoriesOptions) ([]core.Memory, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	like := "%" + query + "%"
	sqlQuery := `SELECT ` + memoryCols + ` FROM memories WHERE (body ILIKE $1 OR tight_description ILIKE $1 OR COALESCE(subject,'') ILIKE $1)`
	args := []any{like}
	i := 2
	if opts.Status != "" {
		sqlQuery += fmt.Sprintf(" AND status = $%d", i)
		args = append(args, string(opts.Status))
		i++
	}
	if opts.Type != "" {
		sqlQuery += fmt.Sprintf(" AND type = $%d", i)
		args = append(args, string(opts.Type))
		i++
	}
	if opts.Scope != "" {
		sqlQuery += fmt.Sprintf(" AND scope = $%d", i)
		args = append(args, string(opts.Scope))
		i++
	}
	if opts.ProjectID != "" {
		sqlQuery += fmt.Sprintf(" AND project_id = $%d", i)
		args = append(args, opts.ProjectID)
		i++
	}
	if opts.AgentID != "" {
		sqlQuery += fmt.Sprintf(" AND (agent_id = $%d OR agent_id IS NULL OR privacy_level IN ('shared','public_safe'))", i)
		args = append(args, opts.AgentID)
		i++
	}
	sqlQuery += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, wrapErr("search memories fuzzy", err)
	}
	defer rows.Close()
	out := make([]core.Memory, 0)
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, wrapErr("search memories fuzzy", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *Repository) ListMemoriesBySourceEventIDs(ctx context.Context, eventIDs []string) ([]core.Memory, error) {
	if len(eventIDs) == 0 {
		return []core.Memory{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+memoryCols+`
		FROM memories
		WHERE status = 'active' AND source_event_ids && $1
		ORDER BY created_at DESC`, pq.Array(eventIDs))
	if err != nil {
		return nil, wrapErr("list memories by source event ids", err)
	}
	defer rows.Close()
	out := make([]core.Memory, 0)
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, wrapErr("list memories by source event ids", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *Repository) CountActiveMemories(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memories WHERE status = 'active'`).Scan(&n)
	return n, wrapErr("count active memories", err)
}

func (r *Repository) InsertClaim(ctx context.Context, claim *core.Claim) error {
	if claim.ID == "" {
		claim.ID = generateID("clm_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO claims (id, memory_id, subject_entity_id, predicate, object_value,
			object_entity_id, confidence, source_event_id, source_summary_id,
			observed_at, valid_from, valid_to, metadata_json)
		VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),NULLIF($6,''),$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13::jsonb)`,
		claim.ID, claim.MemoryID, claim.SubjectEntityID, claim.Predicate, claim.ObjectValue,
		claim.ObjectEntityID, claim.Confidence, claim.SourceEventID, claim.SourceSummaryID,
		claim.ObservedAt, claim.ValidFrom, claim.ValidTo, marshalMapJSON(claim.Metadata))
	return wrapErr("insert claim", err)
}

func (r *Repository) GetClaim(ctx context.Context, id string) (*core.Claim, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, memory_id, COALESCE(subject_entity_id,''), predicate,
			COALESCE(object_value,''), COALESCE(object_entity_id,''), confidence,
			COALESCE(source_event_id,''), COALESCE(source_summary_id,''),
			observed_at, valid_from, valid_to, metadata_json
		FROM claims WHERE id = $1`, id)

	var c core.Claim
	var observedAt, validFrom, validTo sql.NullTime
	var meta []byte
	if err := row.Scan(&c.ID, &c.MemoryID, &c.SubjectEntityID, &c.Predicate,
		&c.ObjectValue, &c.ObjectEntityID, &c.Confidence,
		&c.SourceEventID, &c.SourceSummaryID,
		&observedAt, &validFrom, &validTo, &meta); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("claim", id)
		}
		return nil, wrapErr("get claim", err)
	}
	if observedAt.Valid {
		t := observedAt.Time.UTC()
		c.ObservedAt = &t
	}
	if validFrom.Valid {
		t := validFrom.Time.UTC()
		c.ValidFrom = &t
	}
	if validTo.Valid {
		t := validTo.Time.UTC()
		c.ValidTo = &t
	}
	c.Metadata = parseMapJSON(meta)
	return &c, nil
}

func (r *Repository) ListClaimsByMemory(ctx context.Context, memoryID string) ([]core.Claim, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, memory_id, COALESCE(subject_entity_id,''), predicate,
			COALESCE(object_value,''), COALESCE(object_entity_id,''), confidence,
			COALESCE(source_event_id,''), COALESCE(source_summary_id,''),
			observed_at, valid_from, valid_to, metadata_json
		FROM claims
		WHERE memory_id = $1`, memoryID)
	if err != nil {
		return nil, wrapErr("list claims by memory", err)
	}
	defer rows.Close()

	out := make([]core.Claim, 0)
	for rows.Next() {
		var c core.Claim
		var observedAt, validFrom, validTo sql.NullTime
		var meta []byte
		if err := rows.Scan(&c.ID, &c.MemoryID, &c.SubjectEntityID, &c.Predicate,
			&c.ObjectValue, &c.ObjectEntityID, &c.Confidence,
			&c.SourceEventID, &c.SourceSummaryID,
			&observedAt, &validFrom, &validTo, &meta); err != nil {
			return nil, wrapErr("list claims by memory", err)
		}
		if observedAt.Valid {
			t := observedAt.Time.UTC()
			c.ObservedAt = &t
		}
		if validFrom.Valid {
			t := validFrom.Time.UTC()
			c.ValidFrom = &t
		}
		if validTo.Valid {
			t := validTo.Time.UTC()
			c.ValidTo = &t
		}
		c.Metadata = parseMapJSON(meta)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) InsertEntity(ctx context.Context, entity *core.Entity) error {
	if entity.ID == "" {
		entity.ID = generateID("ent_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO entities (id, type, canonical_name, aliases, description, metadata_json, created_at, updated_at)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6::jsonb,$7,$8)`,
		entity.ID, entity.Type, entity.CanonicalName, pq.Array(entity.Aliases), entity.Description, marshalMapJSON(entity.Metadata), entity.CreatedAt.UTC(), entity.UpdatedAt.UTC())
	return wrapErr("insert entity", err)
}

func (r *Repository) UpdateEntity(ctx context.Context, entity *core.Entity) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE entities SET type = $1, canonical_name = $2, aliases = $3, description = NULLIF($4,''), metadata_json = $5::jsonb, updated_at = $6
		WHERE id = $7`, entity.Type, entity.CanonicalName, pq.Array(entity.Aliases), entity.Description, marshalMapJSON(entity.Metadata), entity.UpdatedAt.UTC(), entity.ID)
	return wrapErr("update entity", err)
}

func (r *Repository) GetEntity(ctx context.Context, id string) (*core.Entity, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, type, canonical_name, aliases, COALESCE(description,''), metadata_json, created_at, updated_at FROM entities WHERE id = $1`, id)
	var e core.Entity
	var aliases pq.StringArray
	var meta []byte
	if err := row.Scan(&e.ID, &e.Type, &e.CanonicalName, pq.Array(&aliases), &e.Description, &meta, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("entity", id)
		}
		return nil, wrapErr("get entity", err)
	}
	e.Aliases = []string(aliases)
	e.Metadata = parseMapJSON(meta)
	return &e, nil
}

func (r *Repository) GetEntitiesByIDs(ctx context.Context, ids []string) ([]core.Entity, error) {
	if len(ids) == 0 {
		return []core.Entity{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, type, canonical_name, aliases, COALESCE(description,''), metadata_json, created_at, updated_at FROM entities WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, wrapErr("get entities by ids", err)
	}
	defer rows.Close()
	out := make([]core.Entity, 0)
	for rows.Next() {
		var e core.Entity
		var aliases pq.StringArray
		var meta []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.CanonicalName, pq.Array(&aliases), &e.Description, &meta, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, wrapErr("get entities by ids", err)
		}
		e.Aliases = []string(aliases)
		e.Metadata = parseMapJSON(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) ListEntities(ctx context.Context, opts core.ListEntitiesOptions) ([]core.Entity, error) {
	query := `SELECT id, type, canonical_name, aliases, COALESCE(description,''), metadata_json, created_at, updated_at FROM entities WHERE 1=1`
	args := make([]any, 0)
	i := 1
	if opts.Type != "" {
		query += fmt.Sprintf(" AND type = $%d", i)
		args = append(args, opts.Type)
		i++
	}
	query += fmt.Sprintf(" ORDER BY canonical_name LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("list entities", err)
	}
	defer rows.Close()
	out := make([]core.Entity, 0)
	for rows.Next() {
		var e core.Entity
		var aliases pq.StringArray
		var meta []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.CanonicalName, pq.Array(&aliases), &e.Description, &meta, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, wrapErr("list entities", err)
		}
		e.Aliases = []string(aliases)
		e.Metadata = parseMapJSON(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) SearchEntities(ctx context.Context, query string, limit int) ([]core.Entity, error) {
	like := "%" + query + "%"
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, type, canonical_name, aliases, COALESCE(description,''), metadata_json, created_at, updated_at
		FROM entities
		WHERE canonical_name ILIKE $1
			OR description ILIKE $1
			OR EXISTS (SELECT 1 FROM unnest(aliases) a WHERE a ILIKE $1)
		LIMIT $2`, like, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("search entities", err)
	}
	defer rows.Close()
	out := make([]core.Entity, 0)
	for rows.Next() {
		var e core.Entity
		var aliases pq.StringArray
		var meta []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.CanonicalName, pq.Array(&aliases), &e.Description, &meta, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, wrapErr("search entities", err)
		}
		e.Aliases = []string(aliases)
		e.Metadata = parseMapJSON(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) LinkMemoryEntity(ctx context.Context, memoryID, entityID, role string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO memory_entities (memory_id, entity_id, role)
		VALUES ($1,$2,NULLIF($3,''))
		ON CONFLICT (memory_id, entity_id)
		DO UPDATE SET role = EXCLUDED.role`, memoryID, entityID, role)
	return wrapErr("link memory entity", err)
}

func (r *Repository) LinkMemoryEntitiesBatch(ctx context.Context, links []core.MemoryEntityLink) error {
	if len(links) == 0 {
		return nil
	}

	valueParts := make([]string, 0, len(links))
	args := make([]any, 0, len(links)*3)
	i := 1
	for _, link := range links {
		if strings.TrimSpace(link.MemoryID) == "" || strings.TrimSpace(link.EntityID) == "" {
			continue
		}
		valueParts = append(valueParts, fmt.Sprintf("($%d,$%d,NULLIF($%d,''))", i, i+1, i+2))
		args = append(args, link.MemoryID, link.EntityID, link.Role)
		i += 3
	}
	if len(valueParts) == 0 {
		return nil
	}

	query := `INSERT INTO memory_entities (memory_id, entity_id, role) VALUES ` + strings.Join(valueParts, ",") + ` ON CONFLICT (memory_id, entity_id) DO NOTHING`
	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return wrapErr("link memory entities batch", err)
	}
	return nil
}

func (r *Repository) GetMemoryEntities(ctx context.Context, memoryID string) ([]core.Entity, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, e.type, e.canonical_name, e.aliases, COALESCE(e.description,''), e.metadata_json, e.created_at, e.updated_at
		FROM entities e
		JOIN memory_entities me ON me.entity_id = e.id
		WHERE me.memory_id = $1`, memoryID)
	if err != nil {
		return nil, wrapErr("get memory entities", err)
	}
	defer rows.Close()
	out := make([]core.Entity, 0)
	for rows.Next() {
		var e core.Entity
		var aliases pq.StringArray
		var meta []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.CanonicalName, pq.Array(&aliases), &e.Description, &meta, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, wrapErr("get memory entities", err)
		}
		e.Aliases = []string(aliases)
		e.Metadata = parseMapJSON(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) GetMemoryEntitiesBatch(ctx context.Context, memoryIDs []string) (map[string][]core.Entity, error) {
	out := make(map[string][]core.Entity)
	if len(memoryIDs) == 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT me.memory_id, e.id, e.type, e.canonical_name, e.aliases, COALESCE(e.description,''), e.metadata_json, e.created_at, e.updated_at
		FROM memory_entities me
		JOIN entities e ON me.entity_id = e.id
		WHERE me.memory_id = ANY($1)`, pq.Array(memoryIDs))
	if err != nil {
		return nil, wrapErr("get memory entities batch", err)
	}
	defer rows.Close()

	for rows.Next() {
		var memoryID string
		var e core.Entity
		var aliases pq.StringArray
		var meta []byte
		if err := rows.Scan(&memoryID, &e.ID, &e.Type, &e.CanonicalName, pq.Array(&aliases), &e.Description, &meta, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, wrapErr("get memory entities batch", err)
		}
		e.Aliases = []string(aliases)
		e.Metadata = parseMapJSON(meta)
		out[memoryID] = append(out[memoryID], e)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("get memory entities batch", err)
	}

	for _, id := range memoryIDs {
		if _, ok := out[id]; !ok {
			out[id] = nil
		}
	}
	return out, nil
}

func (r *Repository) CountMemoryEntityLinks(ctx context.Context, entityID string) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_entities WHERE entity_id = $1`, entityID).Scan(&n)
	return n, wrapErr("count memory entity links", err)
}

func (r *Repository) CountMemoryEntityLinksBatch(ctx context.Context, entityIDs []string) (map[string]int64, error) {
	out := make(map[string]int64)
	if len(entityIDs) == 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT entity_id, COUNT(*) as cnt
		FROM memory_entities
		WHERE entity_id = ANY($1)
		GROUP BY entity_id`, pq.Array(entityIDs))
	if err != nil {
		return nil, wrapErr("count memory entity links batch", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entityID string
		var count int64
		if err := rows.Scan(&entityID, &count); err != nil {
			return nil, wrapErr("count memory entity links batch", err)
		}
		out[entityID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("count memory entity links batch", err)
	}

	for _, id := range entityIDs {
		if _, ok := out[id]; !ok {
			out[id] = 0
		}
	}

	return out, nil
}

func (r *Repository) InsertProject(ctx context.Context, project *core.Project) error {
	if project.ID == "" {
		project.ID = generateID("prj_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO projects (id, name, path, description, metadata_json, created_at, updated_at)
		VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),$5::jsonb,$6,$7)`,
		project.ID, project.Name, project.Path, project.Description, marshalMapJSON(project.Metadata), project.CreatedAt.UTC(), project.UpdatedAt.UTC())
	return wrapErr("insert project", err)
}

func (r *Repository) GetProject(ctx context.Context, id string) (*core.Project, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, name, COALESCE(path,''), COALESCE(description,''), metadata_json, created_at, updated_at FROM projects WHERE id = $1`, id)
	var p core.Project
	var meta []byte
	if err := row.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &meta, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("project", id)
		}
		return nil, wrapErr("get project", err)
	}
	p.Metadata = parseMapJSON(meta)
	return &p, nil
}

func (r *Repository) ListProjects(ctx context.Context) ([]core.Project, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, COALESCE(path,''), COALESCE(description,''), metadata_json, created_at, updated_at FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, wrapErr("list projects", err)
	}
	defer rows.Close()
	out := make([]core.Project, 0)
	for rows.Next() {
		var p core.Project
		var meta []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &meta, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, wrapErr("list projects", err)
		}
		p.Metadata = parseMapJSON(meta)
		out = append(out, p)
	}
	return out, wrapErr("list projects", rows.Err())
}

func (r *Repository) DeleteProject(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return wrapErr("delete project", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return wrapErr("delete project", err)
	}
	if n == 0 {
		return errNotFound("project", id)
	}
	return nil
}

func (r *Repository) InsertRelationship(ctx context.Context, rel *core.Relationship) error {
	if rel.ID == "" {
		rel.ID = generateID("rel_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO relationships (id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7)`, rel.ID, rel.FromEntityID, rel.ToEntityID, rel.RelationshipType, marshalMapJSON(rel.Metadata), rel.CreatedAt.UTC(), rel.UpdatedAt.UTC())
	return wrapErr("insert relationship", err)
}

func (r *Repository) GetRelationship(ctx context.Context, id string) (*core.Relationship, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at FROM relationships WHERE id = $1`, id)
	var rel core.Relationship
	var meta []byte
	if err := row.Scan(&rel.ID, &rel.FromEntityID, &rel.ToEntityID, &rel.RelationshipType, &meta, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("relationship", id)
		}
		return nil, wrapErr("get relationship", err)
	}
	rel.Metadata = parseMapJSON(meta)
	return &rel, nil
}

func (r *Repository) ListRelationships(ctx context.Context, opts core.ListRelationshipsOptions) ([]core.Relationship, error) {
	query := `SELECT id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at FROM relationships WHERE 1=1`
	args := make([]any, 0)
	i := 1
	if opts.EntityID != "" {
		query += fmt.Sprintf(" AND (from_entity_id = $%d OR to_entity_id = $%d)", i, i)
		args = append(args, opts.EntityID)
		i++
	}
	if opts.RelationshipType != "" {
		query += fmt.Sprintf(" AND relationship_type = $%d", i)
		args = append(args, opts.RelationshipType)
		i++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("list relationships", err)
	}
	defer rows.Close()
	out := make([]core.Relationship, 0)
	for rows.Next() {
		var rel core.Relationship
		var meta []byte
		if err := rows.Scan(&rel.ID, &rel.FromEntityID, &rel.ToEntityID, &rel.RelationshipType, &meta, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
			return nil, wrapErr("list relationships", err)
		}
		rel.Metadata = parseMapJSON(meta)
		out = append(out, rel)
	}
	return out, wrapErr("list relationships", rows.Err())
}

func (r *Repository) ListRelationshipsByEntityIDs(ctx context.Context, entityIDs []string) ([]core.Relationship, error) {
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

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at
		FROM relationships
		WHERE from_entity_id = ANY($1) OR to_entity_id = ANY($1)`, pq.Array(cleanIDs))
	if err != nil {
		return nil, wrapErr("list relationships by entity ids", err)
	}
	defer rows.Close()
	out := make([]core.Relationship, 0)
	for rows.Next() {
		var rel core.Relationship
		var meta []byte
		if err := rows.Scan(&rel.ID, &rel.FromEntityID, &rel.ToEntityID, &rel.RelationshipType, &meta, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
			return nil, wrapErr("list relationships by entity ids", err)
		}
		rel.Metadata = parseMapJSON(meta)
		out = append(out, rel)
	}
	return out, wrapErr("list relationships by entity ids", rows.Err())
}

func (r *Repository) InsertRelationshipsBatch(ctx context.Context, rels []*core.Relationship) error {
	if len(rels) == 0 {
		return nil
	}

	valueParts := make([]string, 0, len(rels))
	args := make([]any, 0, len(rels)*7)
	argPos := 1

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

		valueParts = append(valueParts, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d::jsonb,$%d,$%d)", argPos, argPos+1, argPos+2, argPos+3, argPos+4, argPos+5, argPos+6))
		args = append(args,
			rel.ID,
			rel.FromEntityID,
			rel.ToEntityID,
			rel.RelationshipType,
			marshalMapJSON(rel.Metadata),
			rel.CreatedAt.UTC(),
			rel.UpdatedAt.UTC(),
		)
		argPos += 7
	}

	if len(valueParts) == 0 {
		return nil
	}

	query := `
		INSERT INTO relationships (id, from_entity_id, to_entity_id, relationship_type, metadata_json, created_at, updated_at)
		VALUES ` + strings.Join(valueParts, ",")
	_, err := r.db.ExecContext(ctx, query, args...)
	return wrapErr("insert relationships batch", err)
}

func (r *Repository) ListRelatedEntities(ctx context.Context, entityID string, depth int) ([]core.RelatedEntity, error) {
	if strings.TrimSpace(entityID) == "" {
		return nil, nil
	}
	if depth <= 0 {
		return nil, nil
	}
	if depth > 3 {
		depth = 3
	}

	rows, err := r.db.QueryContext(ctx, `
		WITH RECURSIVE related(entity_id, hop, rel_type, visited) AS (
			SELECT $1::text, 0, ''::text, ',' || $1::text || ','
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
			WHERE related.hop < $2
				AND related.visited NOT LIKE '%,' || CASE
					WHEN r.from_entity_id = related.entity_id THEN r.to_entity_id
					ELSE r.from_entity_id
				END || ',%'
		)
		SELECT DISTINCT
			e.id,
			e.type,
			e.canonical_name,
			e.aliases,
			COALESCE(e.description,''),
			e.metadata_json,
			e.created_at,
			e.updated_at,
			related.hop,
			related.rel_type
		FROM related
		JOIN entities e ON e.id = related.entity_id
		WHERE related.hop > 0
		ORDER BY related.hop ASC`, entityID, depth)
	if err != nil {
		return nil, wrapErr("list related entities", err)
	}
	defer rows.Close()

	resultsByEntity := make(map[string]core.RelatedEntity)
	orderedIDs := make([]string, 0)
	for rows.Next() {
		var ent core.Entity
		var aliases pq.StringArray
		var meta []byte
		var hop int
		var relType string
		if err := rows.Scan(&ent.ID, &ent.Type, &ent.CanonicalName, pq.Array(&aliases),
			&ent.Description, &meta, &ent.CreatedAt, &ent.UpdatedAt, &hop, &relType); err != nil {
			return nil, wrapErr("list related entities", err)
		}
		ent.Aliases = []string(aliases)
		ent.Metadata = parseMapJSON(meta)

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
		return nil, wrapErr("list related entities", err)
	}

	related := make([]core.RelatedEntity, 0, len(resultsByEntity))
	for _, id := range orderedIDs {
		related = append(related, resultsByEntity[id])
	}
	return related, nil
}

func (r *Repository) RebuildEntityGraphProjection(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapErr("begin projection rebuild transaction", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `TRUNCATE TABLE entity_graph_projection`); err != nil {
		return wrapErr("truncate entity_graph_projection", err)
	}

	createdAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		WITH RECURSIVE walk(root_entity_id, entity_id, hop_distance, relationship_path, visited) AS (
			SELECT id, id, 0, ''::text, ',' || id || ','
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
				(1.0 / (hop_distance + 1))::double precision AS score,
				ROW_NUMBER() OVER (
					PARTITION BY root_entity_id, entity_id
					ORDER BY hop_distance ASC, relationship_path ASC
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
			$1
		FROM ranked
		WHERE row_num = 1
		ON CONFLICT (entity_id, related_entity_id)
		DO UPDATE SET
			hop_distance = LEAST(entity_graph_projection.hop_distance, EXCLUDED.hop_distance),
			relationship_path = CASE
				WHEN EXCLUDED.hop_distance < entity_graph_projection.hop_distance THEN EXCLUDED.relationship_path
				ELSE entity_graph_projection.relationship_path
			END,
			score = GREATEST(entity_graph_projection.score, EXCLUDED.score),
			created_at = EXCLUDED.created_at`, createdAt); err != nil {
		return wrapErr("rebuild entity_graph_projection rows", err)
	}

	if err := tx.Commit(); err != nil {
		return wrapErr("commit projection rebuild", err)
	}

	return nil
}

func (r *Repository) ListProjectedRelatedEntities(ctx context.Context, entityID string) ([]core.ProjectedRelation, error) {
	if strings.TrimSpace(entityID) == "" {
		return nil, nil
	}

	rows, err := r.db.QueryContext(ctx, `SELECT related_entity_id, hop_distance, COALESCE(relationship_path, ''), score FROM entity_graph_projection WHERE entity_id = $1 ORDER BY hop_distance ASC, score DESC, related_entity_id ASC`, entityID)
	if err != nil {
		return nil, wrapErr("list projected related entities", err)
	}
	defer rows.Close()
	out := make([]core.ProjectedRelation, 0)
	for rows.Next() {
		var rel core.ProjectedRelation
		if err := rows.Scan(&rel.RelatedEntityID, &rel.HopDistance, &rel.RelationshipPath, &rel.Score); err != nil {
			return nil, wrapErr("list projected related entities", err)
		}
		out = append(out, rel)
	}
	return out, wrapErr("list projected related entities", rows.Err())
}

func (r *Repository) DeleteRelationship(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM relationships WHERE id = $1`, id)
	if err != nil {
		return wrapErr("delete relationship", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return wrapErr("delete relationship", err)
	}
	if n == 0 {
		return errNotFound("relationship", id)
	}
	return nil
}

func (r *Repository) InsertEpisode(ctx context.Context, episode *core.Episode) error {
	if episode.ID == "" {
		episode.ID = generateID("epi_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO episodes (
			id, title, summary, tight_description, scope, project_id, session_id,
			importance, privacy_level, started_at, ended_at,
			source_span_json, source_summary_ids, participants, related_entities,
			outcomes, unresolved_items, metadata_json, created_at, updated_at
		)
		VALUES (
			$1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10,$11,
			$12::jsonb,$13,$14,$15,$16,$17,$18::jsonb,$19,$20
		)`,
		episode.ID, episode.Title, episode.Summary, episode.TightDescription, string(episode.Scope),
		episode.ProjectID, episode.SessionID, episode.Importance, string(episode.PrivacyLevel),
		episode.StartedAt, episode.EndedAt, marshalSourceSpan(episode.SourceSpan),
		pq.Array(episode.SourceSummaryIDs), pq.Array(episode.Participants), pq.Array(episode.RelatedEntities),
		pq.Array(episode.Outcomes), pq.Array(episode.UnresolvedItems), marshalMapJSON(episode.Metadata),
		episode.CreatedAt.UTC(), episode.UpdatedAt.UTC())
	return wrapErr("insert episode", err)
}

const episodeCols = `id, title, summary, tight_description, scope, COALESCE(project_id,''), COALESCE(session_id,''),
	importance, privacy_level, started_at, ended_at,
	source_span_json, source_summary_ids, participants, related_entities, outcomes, unresolved_items,
	metadata_json, created_at, updated_at`

func (r *Repository) scanEpisode(scanner interface{ Scan(dest ...any) error }) (*core.Episode, error) {
	var ep core.Episode
	var startedAt, endedAt sql.NullTime
	var sourceSummaryIDs, participants, relatedEntities, outcomes, unresolvedItems pq.StringArray
	var span, meta []byte
	if err := scanner.Scan(&ep.ID, &ep.Title, &ep.Summary, &ep.TightDescription, &ep.Scope,
		&ep.ProjectID, &ep.SessionID, &ep.Importance, &ep.PrivacyLevel, &startedAt, &endedAt,
		&span, pq.Array(&sourceSummaryIDs), pq.Array(&participants), pq.Array(&relatedEntities), pq.Array(&outcomes), pq.Array(&unresolvedItems),
		&meta, &ep.CreatedAt, &ep.UpdatedAt); err != nil {
		return nil, wrapErr("scan episode", err)
	}
	if startedAt.Valid {
		t := startedAt.Time.UTC()
		ep.StartedAt = &t
	}
	if endedAt.Valid {
		t := endedAt.Time.UTC()
		ep.EndedAt = &t
	}
	ep.SourceSpan = parseSourceSpanJSON(span)
	ep.SourceSummaryIDs = []string(sourceSummaryIDs)
	ep.Participants = []string(participants)
	ep.RelatedEntities = []string(relatedEntities)
	ep.Outcomes = []string(outcomes)
	ep.UnresolvedItems = []string(unresolvedItems)
	ep.Metadata = parseMapJSON(meta)
	return &ep, nil
}

func (r *Repository) GetEpisode(ctx context.Context, id string) (*core.Episode, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+episodeCols+` FROM episodes WHERE id = $1`, id)
	ep, err := r.scanEpisode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("episode", id)
		}
		return nil, wrapErr("get episode", err)
	}
	return ep, nil
}

func (r *Repository) ListEpisodes(ctx context.Context, opts core.ListEpisodesOptions) ([]core.Episode, error) {
	query := `SELECT ` + episodeCols + ` FROM episodes WHERE 1=1`
	args := make([]any, 0)
	i := 1
	if opts.Scope != "" {
		query += fmt.Sprintf(" AND scope = $%d", i)
		args = append(args, string(opts.Scope))
		i++
	}
	if opts.ProjectID != "" {
		query += fmt.Sprintf(" AND project_id = $%d", i)
		args = append(args, opts.ProjectID)
		i++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("list episodes", err)
	}
	defer rows.Close()
	out := make([]core.Episode, 0)
	for rows.Next() {
		ep, err := r.scanEpisode(rows)
		if err != nil {
			return nil, wrapErr("list episodes", err)
		}
		out = append(out, *ep)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("list episodes", err)
	}
	return out, nil
}

func (r *Repository) SearchEpisodes(ctx context.Context, query string, limit int) ([]core.Episode, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+episodeCols+`
		FROM episodes, plainto_tsquery('simple', $1) AS q
		WHERE episodes_fts @@ q
		ORDER BY ts_rank(episodes_fts, q) DESC, created_at DESC
		LIMIT $2`, query, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("search episodes", err)
	}
	defer rows.Close()
	out := make([]core.Episode, 0)
	for rows.Next() {
		ep, err := r.scanEpisode(rows)
		if err != nil {
			return nil, wrapErr("search episodes", err)
		}
		out = append(out, *ep)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("search episodes", err)
	}
	return out, nil
}

func (r *Repository) InsertArtifact(ctx context.Context, artifact *core.Artifact) error {
	if artifact.ID == "" {
		artifact.ID = generateID("art_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO artifacts (id, kind, source_system, project_id, path, content, metadata_json, created_at)
		VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7::jsonb,$8)`,
		artifact.ID, artifact.Kind, artifact.SourceSystem, artifact.ProjectID, artifact.Path, artifact.Content, marshalMapJSON(artifact.Metadata), artifact.CreatedAt.UTC())
	return wrapErr("insert artifact", err)
}

func (r *Repository) GetArtifact(ctx context.Context, id string) (*core.Artifact, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, kind, COALESCE(source_system,''), COALESCE(project_id,''), COALESCE(path,''), COALESCE(content,''), metadata_json, created_at FROM artifacts WHERE id = $1`, id)
	var a core.Artifact
	var meta []byte
	if err := row.Scan(&a.ID, &a.Kind, &a.SourceSystem, &a.ProjectID, &a.Path, &a.Content, &meta, &a.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("artifact", id)
		}
		return nil, wrapErr("get artifact", err)
	}
	a.Metadata = unmarshalMapJSON(meta)
	return &a, nil
}

func (r *Repository) InsertJob(ctx context.Context, job *core.Job) error {
	if job.ID == "" {
		job.ID = generateID("job_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO jobs (id, kind, status, payload_json, result_json, error_text, scheduled_at, started_at, finished_at, created_at)
		VALUES ($1,$2,$3,$4::jsonb,$5::jsonb,NULLIF($6,''),$7,$8,$9,$10)`,
		job.ID, job.Kind, job.Status, marshalMapJSON(job.Payload), marshalMapJSON(job.Result), job.ErrorText,
		job.ScheduledAt, job.StartedAt, job.FinishedAt, job.CreatedAt.UTC())
	return wrapErr("insert job", err)
}

func (r *Repository) GetJob(ctx context.Context, id string) (*core.Job, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, kind, status, payload_json, result_json, COALESCE(error_text,''), scheduled_at, started_at, finished_at, created_at
		FROM jobs WHERE id = $1`, id)
	var j core.Job
	var payload, result []byte
	var scheduledAt, startedAt, finishedAt sql.NullTime
	if err := row.Scan(&j.ID, &j.Kind, &j.Status, &payload, &result, &j.ErrorText, &scheduledAt, &startedAt, &finishedAt, &j.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("job", id)
		}
		return nil, wrapErr("get job", err)
	}
	j.Payload = unmarshalMapJSON(payload)
	j.Result = unmarshalMapJSON(result)
	j.ScheduledAt = parseNullTime(scheduledAt)
	j.StartedAt = parseNullTime(startedAt)
	j.FinishedAt = parseNullTime(finishedAt)
	return &j, nil
}

func (r *Repository) UpdateJob(ctx context.Context, job *core.Job) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET kind = $1, status = $2, payload_json = $3::jsonb, result_json = $4::jsonb,
			error_text = NULLIF($5,''), scheduled_at = $6, started_at = $7, finished_at = $8
		WHERE id = $9`, job.Kind, job.Status, marshalMapJSON(job.Payload), marshalMapJSON(job.Result), job.ErrorText,
		job.ScheduledAt, job.StartedAt, job.FinishedAt, job.ID)
	return wrapErr("update job", err)
}

func (r *Repository) ListJobs(ctx context.Context, opts core.ListJobsOptions) ([]core.Job, error) {
	query := `SELECT id, kind, status, payload_json, result_json, COALESCE(error_text,''), scheduled_at, started_at, finished_at, created_at FROM jobs WHERE 1=1`
	args := make([]any, 0)
	i := 1
	if opts.Kind != "" {
		query += fmt.Sprintf(" AND kind = $%d", i)
		args = append(args, opts.Kind)
		i++
	}
	if opts.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", i)
		args = append(args, opts.Status)
		i++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", i)
	args = append(args, defaultLimit(opts.Limit))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("list jobs", err)
	}
	defer rows.Close()
	out := make([]core.Job, 0)
	for rows.Next() {
		var j core.Job
		var payload, result []byte
		var scheduledAt, startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&j.ID, &j.Kind, &j.Status, &payload, &result, &j.ErrorText, &scheduledAt, &startedAt, &finishedAt, &j.CreatedAt); err != nil {
			return nil, wrapErr("list jobs", err)
		}
		j.Payload = unmarshalMapJSON(payload)
		j.Result = unmarshalMapJSON(result)
		j.ScheduledAt = parseNullTime(scheduledAt)
		j.StartedAt = parseNullTime(startedAt)
		j.FinishedAt = parseNullTime(finishedAt)
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("list jobs", err)
	}
	return out, nil
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

func (r *Repository) InsertIngestionPolicy(ctx context.Context, policy *core.IngestionPolicy) error {
	if policy.ID == "" {
		policy.ID = generateID("pol_")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ingestion_policies (id, pattern_type, pattern, mode, priority, match_mode, metadata_json, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)`,
		policy.ID, policy.PatternType, policy.Pattern, policy.Mode, policy.Priority, defaultPolicyMatchMode(policy.MatchMode),
		marshalMapJSON(policy.Metadata), policy.CreatedAt.UTC(), policy.UpdatedAt.UTC())
	return wrapErr("insert ingestion policy", err)
}

func (r *Repository) GetIngestionPolicy(ctx context.Context, id string) (*core.IngestionPolicy, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, pattern_type, pattern, mode, priority, match_mode, metadata_json, created_at, updated_at
		FROM ingestion_policies WHERE id = $1`, id)
	var p core.IngestionPolicy
	var meta []byte
	if err := row.Scan(&p.ID, &p.PatternType, &p.Pattern, &p.Mode, &p.Priority, &p.MatchMode, &meta, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("ingestion policy", id)
		}
		return nil, wrapErr("get ingestion policy", err)
	}
	p.Metadata = unmarshalMapJSON(meta)
	p.MatchMode = defaultPolicyMatchMode(p.MatchMode)
	return &p, nil
}

func (r *Repository) ListIngestionPolicies(ctx context.Context) ([]core.IngestionPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, pattern_type, pattern, mode, priority, match_mode, metadata_json, created_at, updated_at
		FROM ingestion_policies
		ORDER BY priority DESC, created_at DESC`)
	if err != nil {
		return nil, wrapErr("list ingestion policies", err)
	}
	defer rows.Close()
	out := make([]core.IngestionPolicy, 0)
	for rows.Next() {
		var p core.IngestionPolicy
		var meta []byte
		if err := rows.Scan(&p.ID, &p.PatternType, &p.Pattern, &p.Mode, &p.Priority, &p.MatchMode, &meta, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, wrapErr("list ingestion policies", err)
		}
		p.Metadata = unmarshalMapJSON(meta)
		p.MatchMode = defaultPolicyMatchMode(p.MatchMode)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr("list ingestion policies", err)
	}
	return out, nil
}

func (r *Repository) DeleteIngestionPolicy(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM ingestion_policies WHERE id = $1`, id)
	if err != nil {
		return wrapErr("delete ingestion policy", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return wrapErr("delete ingestion policy", err)
	}
	if n == 0 {
		return errNotFound("ingestion policy", id)
	}
	return nil
}

func (r *Repository) MatchIngestionPolicy(ctx context.Context, patternType, value string) (*core.IngestionPolicy, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, pattern_type, pattern, mode, priority, match_mode, metadata_json, created_at, updated_at
		FROM ingestion_policies
		WHERE pattern_type = $1
		  AND CASE COALESCE(NULLIF(match_mode, ''), 'glob')
				WHEN 'exact' THEN pattern = $2
				WHEN 'regex' THEN $2 ~ pattern
				ELSE $2 LIKE REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(pattern, E'\\', E'\\\\'), '%', E'\\%'), '_', E'\\_'), '*', '%'), '?', '_') ESCAPE E'\\'
			  END
		ORDER BY priority DESC, created_at ASC
		LIMIT 1`, patternType, value)

	var p core.IngestionPolicy
	var meta []byte
	if err := row.Scan(&p.ID, &p.PatternType, &p.Pattern, &p.Mode, &p.Priority, &p.MatchMode, &meta, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, wrapErr("match ingestion policy", err)
	}
	p.Metadata = unmarshalMapJSON(meta)
	p.MatchMode = defaultPolicyMatchMode(p.MatchMode)
	return &p, nil
}

func (r *Repository) RecordRecall(ctx context.Context, sessionID, itemID, itemKind string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO recall_history (session_id, item_id, item_kind, shown_at)
		VALUES ($1,$2,$3,NOW())`, sessionID, itemID, itemKind)
	return wrapErr("record recall", err)
}

func (r *Repository) RecordRecallBatch(ctx context.Context, sessionID string, items []core.RecallRecord) error {
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		if err := r.RecordRecall(ctx, sessionID, item.ItemID, item.ItemKind); err != nil {
			return wrapErr("record recall batch", err)
		}
	}
	return nil
}

func (r *Repository) GetRecentRecalls(ctx context.Context, sessionID string, limit int) ([]core.RecallHistoryEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT session_id, item_id, item_kind, shown_at
		FROM recall_history
		WHERE session_id = $1
		ORDER BY shown_at DESC
		LIMIT $2`, sessionID, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("get recent recalls", err)
	}
	defer rows.Close()
	out := make([]core.RecallHistoryEntry, 0)
	for rows.Next() {
		var entry core.RecallHistoryEntry
		var shownAt time.Time
		if err := rows.Scan(&entry.SessionID, &entry.ItemID, &entry.ItemKind, &shownAt); err != nil {
			return nil, wrapErr("get recent recalls", err)
		}
		entry.ShownAt = shownAt.UTC().Format(time.RFC3339)
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *Repository) ListMemoryAccessStats(ctx context.Context, since time.Time) ([]core.MemoryAccessStat, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT item_id, COUNT(*) AS access_count, MAX(shown_at) AS last_accessed_at
		FROM recall_history
		WHERE item_kind = 'memory' AND shown_at >= $1
		GROUP BY item_id`, since.UTC())
	if err != nil {
		return nil, wrapErr("list memory access stats", err)
	}
	defer rows.Close()
	out := make([]core.MemoryAccessStat, 0)
	for rows.Next() {
		var stat core.MemoryAccessStat
		var lastAccessed time.Time
		if err := rows.Scan(&stat.MemoryID, &stat.AccessCount, &lastAccessed); err != nil {
			return nil, wrapErr("list memory access stats", err)
		}
		stat.LastAccessedAt = lastAccessed.UTC().Format(time.RFC3339)
		out = append(out, stat)
	}
	return out, rows.Err()
}

func (r *Repository) CleanupRecallHistory(ctx context.Context, olderThanDays int) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM recall_history
		WHERE shown_at < NOW() - ($1::text || ' days')::interval`, olderThanDays)
	if err != nil {
		return 0, wrapErr("cleanup recall history", err)
	}
	return res.RowsAffected()
}

func (r *Repository) PurgeOldEvents(ctx context.Context, olderThanDays int) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM events
		WHERE occurred_at < NOW() - ($1::text || ' days')::interval
		  AND reflected_at IS NOT NULL`, olderThanDays)
	if err != nil {
		return 0, wrapErr("purge old events", err)
	}
	return res.RowsAffected()
}

func (r *Repository) PurgeOldJobs(ctx context.Context, olderThanDays int) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM jobs
		WHERE created_at < NOW() - ($1::text || ' days')::interval
		  AND status IN ('completed', 'failed')`, olderThanDays)
	if err != nil {
		return 0, wrapErr("purge old jobs", err)
	}
	return res.RowsAffected()
}

func (r *Repository) ExpireRetrievalCache(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM retrieval_cache
		WHERE expires_at < NOW()`)
	if err != nil {
		return 0, wrapErr("expire retrieval cache", err)
	}
	return res.RowsAffected()
}

func (r *Repository) PurgeOldRelevanceFeedback(ctx context.Context, olderThanDays int) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM relevance_feedback
		WHERE created_at < NOW() - ($1::text || ' days')::interval`, olderThanDays)
	if err != nil {
		return 0, wrapErr("purge old relevance feedback", err)
	}
	return res.RowsAffected()
}

func (r *Repository) VacuumAnalyze(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `ANALYZE`); err != nil {
		slog.Warn("Postgres vacuum/analyze maintenance step failed", "step", "analyze", "error", err)
		return wrapErr("vacuum analyze", err)
	}
	return nil
}

func (r *Repository) InsertRelevanceFeedback(ctx context.Context, sessionID, itemID, itemKind, action string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO relevance_feedback (session_id, item_id, item_kind, action, created_at)
		VALUES ($1,$2,$3,$4,NOW())
		ON CONFLICT (session_id, item_id, action) DO NOTHING`,
		sessionID, itemID, itemKind, action)
	return wrapErr("insert relevance feedback", err)
}

func (r *Repository) ListRelevanceFeedback(ctx context.Context, itemID string) ([]core.RelevanceFeedbackEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT session_id, item_id, item_kind, action, created_at
		FROM relevance_feedback
		WHERE item_id = $1
		ORDER BY created_at DESC`, itemID)
	if err != nil {
		return nil, wrapErr("list relevance feedback", err)
	}
	defer rows.Close()
	out := make([]core.RelevanceFeedbackEntry, 0)
	for rows.Next() {
		var entry core.RelevanceFeedbackEntry
		if err := rows.Scan(&entry.SessionID, &entry.ItemID, &entry.ItemKind, &entry.Action, &entry.CreatedAt); err != nil {
			return nil, wrapErr("list relevance feedback", err)
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *Repository) CountExpandedFeedbackBatch(ctx context.Context, memoryIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(memoryIDs))
	for _, id := range memoryIDs {
		out[id] = 0
	}
	if len(memoryIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT item_id, COUNT(*)
		FROM relevance_feedback
		WHERE item_kind = 'memory' AND action = 'expanded' AND item_id = ANY($1)
		GROUP BY item_id`, pq.Array(memoryIDs))
	if err != nil {
		return nil, wrapErr("count expanded feedback batch", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, wrapErr("count expanded feedback batch", err)
		}
		out[id] = count
	}
	return out, rows.Err()
}

func (r *Repository) UpsertEmbedding(ctx context.Context, embedding *core.EmbeddingRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO embeddings (object_id, object_kind, embedding, model, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (object_id, object_kind, model)
		DO UPDATE SET embedding = EXCLUDED.embedding, created_at = EXCLUDED.created_at`,
		embedding.ObjectID, embedding.ObjectKind, encodeVector(embedding.Vector), embedding.Model, embedding.CreatedAt.UTC())
	return wrapErr("upsert embedding", err)
}

func (r *Repository) GetEmbedding(ctx context.Context, objectID, objectKind, model string) (*core.EmbeddingRecord, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT object_id, object_kind, embedding, model, created_at
		FROM embeddings
		WHERE object_id = $1 AND object_kind = $2 AND model = $3`, objectID, objectKind, model)
	var rec core.EmbeddingRecord
	var embedding []byte
	if err := row.Scan(&rec.ObjectID, &rec.ObjectKind, &embedding, &rec.Model, &rec.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("embedding", objectID)
		}
		return nil, wrapErr("get embedding", err)
	}
	rec.Vector = decodeVector(embedding)
	return &rec, nil
}

func (r *Repository) GetEmbeddingsBatch(ctx context.Context, objectIDs []string, objectKind, model string) (map[string]core.EmbeddingRecord, error) {
	out := make(map[string]core.EmbeddingRecord)
	if len(objectIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT object_id, object_kind, embedding, model, created_at
		FROM embeddings
		WHERE object_id = ANY($1) AND object_kind = $2 AND model = $3`, pq.Array(objectIDs), objectKind, model)
	if err != nil {
		return nil, wrapErr("get embeddings batch", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rec core.EmbeddingRecord
		var embedding []byte
		if err := rows.Scan(&rec.ObjectID, &rec.ObjectKind, &embedding, &rec.Model, &rec.CreatedAt); err != nil {
			return nil, wrapErr("get embeddings batch", err)
		}
		rec.Vector = decodeVector(embedding)
		out[rec.ObjectID] = rec
	}
	return out, rows.Err()
}

func (r *Repository) ListEmbeddingsByKind(ctx context.Context, objectKind, model string, limit int) ([]core.EmbeddingRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT object_id, object_kind, embedding, model, created_at
		FROM embeddings
		WHERE object_kind = $1 AND model = $2
		ORDER BY created_at DESC
		LIMIT $3`, objectKind, model, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("list embeddings by kind", err)
	}
	defer rows.Close()
	out := make([]core.EmbeddingRecord, 0)
	for rows.Next() {
		var rec core.EmbeddingRecord
		var embedding []byte
		if err := rows.Scan(&rec.ObjectID, &rec.ObjectKind, &embedding, &rec.Model, &rec.CreatedAt); err != nil {
			return nil, wrapErr("list embeddings by kind", err)
		}
		rec.Vector = decodeVector(embedding)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *Repository) DeleteEmbeddings(ctx context.Context, objectID, objectKind, model string) error {
	if model == "" {
		_, err := r.db.ExecContext(ctx, `DELETE FROM embeddings WHERE object_id = $1 AND object_kind = $2`, objectID, objectKind)
		return wrapErr("delete embeddings", err)
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM embeddings WHERE object_id = $1 AND object_kind = $2 AND model = $3`, objectID, objectKind, model)
	return wrapErr("delete embeddings", err)
}

func (r *Repository) ListUnembeddedMemories(ctx context.Context, model string, limit int) ([]core.Memory, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+memoryCols+`
		FROM memories m
		WHERE m.status = 'active'
		AND NOT EXISTS (
			SELECT 1 FROM embeddings e
			WHERE e.object_id = m.id AND e.object_kind = 'memory' AND e.model = $1
		)
		ORDER BY m.created_at DESC
		LIMIT $2`, model, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("list unembedded memories", err)
	}
	defer rows.Close()
	out := make([]core.Memory, 0)
	for rows.Next() {
		m, err := r.scanMemory(rows)
		if err != nil {
			return nil, wrapErr("list unembedded memories", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *Repository) ListUnembeddedSummaries(ctx context.Context, model string, limit int) ([]core.Summary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, kind, scope, COALESCE(project_id,''), COALESCE(session_id,''), COALESCE(agent_id,''),
			COALESCE(title,''), body, tight_description, privacy_level,
			source_span_json, metadata_json, depth, COALESCE(condensed_kind,''), created_at, updated_at
		FROM summaries s
		WHERE NOT EXISTS (
			SELECT 1 FROM embeddings e
			WHERE e.object_id = s.id AND e.object_kind = 'summary' AND e.model = $1
		)
		ORDER BY s.created_at DESC
		LIMIT $2`, model, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("list unembedded summaries", err)
	}
	defer rows.Close()
	out := make([]core.Summary, 0)
	for rows.Next() {
		var s core.Summary
		var span, meta []byte
		if err := rows.Scan(&s.ID, &s.Kind, &s.Scope, &s.ProjectID, &s.SessionID, &s.AgentID,
			&s.Title, &s.Body, &s.TightDescription, &s.PrivacyLevel, &span, &meta, &s.Depth, &s.CondensedKind, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, wrapErr("list unembedded summaries", err)
		}
		s.SourceSpan = parseSourceSpanJSON(span)
		s.Metadata = parseMapJSON(meta)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) ListUnembeddedEpisodes(ctx context.Context, model string, limit int) ([]core.Episode, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+episodeCols+`
		FROM episodes ep
		WHERE NOT EXISTS (
			SELECT 1 FROM embeddings e
			WHERE e.object_id = ep.id AND e.object_kind = 'episode' AND e.model = $1
		)
		ORDER BY ep.created_at DESC
		LIMIT $2`, model, defaultLimit(limit))
	if err != nil {
		return nil, wrapErr("list unembedded episodes", err)
	}
	defer rows.Close()
	out := make([]core.Episode, 0)
	for rows.Next() {
		ep, err := r.scanEpisode(rows)
		if err != nil {
			return nil, wrapErr("list unembedded episodes", err)
		}
		out = append(out, *ep)
	}
	return out, rows.Err()
}

func (r *Repository) countTable(ctx context.Context, table string) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n)
	return n, wrapErr("count table", err)
}

func (r *Repository) CountEvents(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "events")
}
func (r *Repository) CountMemories(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "memories")
}
func (r *Repository) CountSummaries(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "summaries")
}
func (r *Repository) CountEpisodes(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "episodes")
}
func (r *Repository) CountEntities(ctx context.Context) (int64, error) {
	return r.countTable(ctx, "entities")
}

func (r *Repository) RebuildFTSIndexes(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE events SET content = content;
		UPDATE memories SET body = body;
		UPDATE summaries SET body = body;
		UPDATE episodes SET summary = summary;
		UPDATE entities SET canonical_name = canonical_name;`)
	return wrapErr("rebuild ftsindexes", err)
}

func normalizeRowsAffected(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func execDeleteCount(ctx context.Context, tx *sql.Tx, table string) (int64, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM `+table)
	if err != nil {
		return 0, wrapErr("exec delete count", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, wrapErr("exec delete count", err)
	}
	return normalizeRowsAffected(n), nil
}

func (r *Repository) ResetDerived(ctx context.Context) (*core.ResetDerivedResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, wrapErr("reset derived", err)
	}
	defer func() { _ = tx.Rollback() }()
	out := &core.ResetDerivedResult{}
	if out.MemoryEntitiesDeleted, err = execDeleteCount(ctx, tx, "memory_entities"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.SummaryEdgesDeleted, err = execDeleteCount(ctx, tx, "summary_edges"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.MemoriesDeleted, err = execDeleteCount(ctx, tx, "memories"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.ClaimsDeleted, err = execDeleteCount(ctx, tx, "claims"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.EntitiesDeleted, err = execDeleteCount(ctx, tx, "entities"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.RelationshipsDeleted, err = execDeleteCount(ctx, tx, "relationships"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.SummariesDeleted, err = execDeleteCount(ctx, tx, "summaries"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.EpisodesDeleted, err = execDeleteCount(ctx, tx, "episodes"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.JobsDeleted, err = execDeleteCount(ctx, tx, "jobs"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.EmbeddingsDeleted, err = execDeleteCount(ctx, tx, "embeddings"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.RetrievalCacheDeleted, err = execDeleteCount(ctx, tx, "retrieval_cache"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.RecallHistoryDeleted, err = execDeleteCount(ctx, tx, "recall_history"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.RelevanceFeedbackDeleted, err = execDeleteCount(ctx, tx, "relevance_feedback"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.EntityGraphProjectionDeleted, err = execDeleteCount(ctx, tx, "entity_graph_projection"); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	res, err := tx.ExecContext(ctx, `UPDATE events SET reflected_at = NULL`)
	if err != nil {
		return nil, wrapErr("reset derived", err)
	}
	if out.EventsReset, err = res.RowsAffected(); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	out.EventsReset = normalizeRowsAffected(out.EventsReset)
	if err := tx.Commit(); err != nil {
		return nil, wrapErr("reset derived", err)
	}
	return out, nil
}
