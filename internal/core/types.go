package core

import "time"

// Scope represents where a memory belongs.
type Scope string

const (
	// ScopeGlobal marks memory that applies across all projects.
	ScopeGlobal Scope = "global"
	// ScopeProject marks memory that applies within a project.
	ScopeProject Scope = "project"
	// ScopeSession marks memory that applies within a single session.
	ScopeSession Scope = "session"
)

// PrivacyLevel controls who can see a memory.
type PrivacyLevel string

const (
	// PrivacyPrivate marks content visible only to the local owner.
	PrivacyPrivate PrivacyLevel = "private"
	// PrivacyShared marks content visible to trusted collaborators.
	PrivacyShared PrivacyLevel = "shared"
	// PrivacyPublicSafe marks content safe for broader publication.
	PrivacyPublicSafe PrivacyLevel = "public_safe"
)

// MemoryStatus represents the lifecycle state of a memory.
type MemoryStatus string

const (
	// MemoryStatusActive marks a memory as currently active.
	MemoryStatusActive MemoryStatus = "active"
	// MemoryStatusSuperseded marks a memory as replaced by a newer one.
	MemoryStatusSuperseded MemoryStatus = "superseded"
	// MemoryStatusArchived marks a memory as retained but inactive.
	MemoryStatusArchived MemoryStatus = "archived"
	// MemoryStatusRetracted marks a memory as withdrawn.
	MemoryStatusRetracted MemoryStatus = "retracted"
)

// MemoryType is the kind of durable memory record.
type MemoryType string

const (
	// MemoryTypeIdentity represents a stable identity claim.
	MemoryTypeIdentity MemoryType = "identity"
	// MemoryTypePreference represents a preference.
	MemoryTypePreference MemoryType = "preference"
	// MemoryTypeFact represents a factual claim.
	MemoryTypeFact MemoryType = "fact"
	// MemoryTypeDecision represents a recorded decision.
	MemoryTypeDecision MemoryType = "decision"
	// MemoryTypeEpisode represents a narrative episode.
	MemoryTypeEpisode MemoryType = "episode"
	// MemoryTypeTodo represents an actionable todo item.
	MemoryTypeTodo MemoryType = "todo"
	// MemoryTypeRelationship represents a relationship between entities.
	MemoryTypeRelationship MemoryType = "relationship"
	// MemoryTypeProcedure represents a procedural instruction.
	MemoryTypeProcedure MemoryType = "procedure"
	// MemoryTypeConstraint represents a constraint or rule.
	MemoryTypeConstraint MemoryType = "constraint"
	// MemoryTypeIncident represents an important incident.
	MemoryTypeIncident MemoryType = "incident"
	// MemoryTypeArtifact represents a durable artifact reference.
	MemoryTypeArtifact MemoryType = "artifact"
	// MemoryTypeSummary represents a summary-as-memory record.
	MemoryTypeSummary MemoryType = "summary"
	// MemoryTypeActiveContext represents active context to preserve.
	MemoryTypeActiveContext MemoryType = "active_context"
	// MemoryTypeOpenLoop represents an unresolved open loop.
	MemoryTypeOpenLoop MemoryType = "open_loop"
	// MemoryTypeAssumption represents a provisional assumption.
	MemoryTypeAssumption MemoryType = "assumption"
	// MemoryTypeContradiction represents a conflicting claim.
	MemoryTypeContradiction MemoryType = "contradiction"
)

// RecallMode selects the retrieval strategy.
type RecallMode string

const (
	// RecallModeAmbient returns low-latency associative recall.
	RecallModeAmbient RecallMode = "ambient"
	// RecallModeFacts returns factual memories.
	RecallModeFacts RecallMode = "facts"
	// RecallModeEpisodes returns narrative episodes.
	RecallModeEpisodes RecallMode = "episodes"
	// RecallModeTimeline returns chronologically ordered results.
	RecallModeTimeline RecallMode = "timeline"
	// RecallModeProject returns results scoped to a project.
	RecallModeProject RecallMode = "project"
	// RecallModeEntity returns results related to entities.
	RecallModeEntity RecallMode = "entity"
	// RecallModeActive returns active context and open loops.
	RecallModeActive RecallMode = "active"
	// RecallModeHistory searches raw event history.
	RecallModeHistory RecallMode = "history"
	// RecallModeHybrid combines multiple retrieval strategies.
	RecallModeHybrid RecallMode = "hybrid"
)

// Event is an append-only raw interaction record.
type Event struct {
	// SequenceID is the monotonic insertion-order identifier for pagination.
	SequenceID   int64             `json:"-"`
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	SourceSystem string            `json:"source_system"`
	Surface      string            `json:"surface,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	AgentID      string            `json:"agent_id,omitempty"`
	ActorType    string            `json:"actor_type,omitempty"`
	ActorID      string            `json:"actor_id,omitempty"`
	PrivacyLevel PrivacyLevel      `json:"privacy_level"`
	Content      string            `json:"content"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Hash         string            `json:"hash,omitempty"`
	OccurredAt   time.Time         `json:"occurred_at"`
	IngestedAt   time.Time         `json:"ingested_at"`
	// ReflectedAt records when this event was processed by the reflect job.
	// Nil indicates the event has not yet been reflected.
	ReflectedAt *time.Time `json:"reflected_at,omitempty"`
}

// Summary is a compression layer object over history.
type Summary struct {
	ID               string            `json:"id"`
	Kind             string            `json:"kind"` // leaf, session, topic, episode, condensed
	Depth            int               `json:"depth"`
	CondensedKind    string            `json:"condensed_kind,omitempty"`
	Scope            Scope             `json:"scope"`
	ProjectID        string            `json:"project_id,omitempty"`
	SessionID        string            `json:"session_id,omitempty"`
	AgentID          string            `json:"agent_id,omitempty"`
	Title            string            `json:"title,omitempty"`
	Body             string            `json:"body"`
	TightDescription string            `json:"tight_description"`
	PrivacyLevel     PrivacyLevel      `json:"privacy_level"`
	SourceSpan       SourceSpan        `json:"source_span"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// SourceSpan identifies the source events and summaries backing a summary.
type SourceSpan struct {
	EventIDs   []string `json:"event_ids,omitempty"`
	SummaryIDs []string `json:"summary_ids,omitempty"`
}

// Memory is a canonical typed durable memory record.
type Memory struct {
	ID                string            `json:"id"`
	Type              MemoryType        `json:"type"`
	Scope             Scope             `json:"scope"`
	ProjectID         string            `json:"project_id,omitempty"`
	SessionID         string            `json:"session_id,omitempty"`
	AgentID           string            `json:"agent_id,omitempty"`
	Subject           string            `json:"subject,omitempty"`
	Body              string            `json:"body"`
	TightDescription  string            `json:"tight_description"`
	Confidence        float64           `json:"confidence"`
	Importance        float64           `json:"importance"`
	PrivacyLevel      PrivacyLevel      `json:"privacy_level"`
	Status            MemoryStatus      `json:"status"`
	ObservedAt        *time.Time        `json:"observed_at,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	ValidFrom         *time.Time        `json:"valid_from,omitempty"`
	ValidTo           *time.Time        `json:"valid_to,omitempty"`
	LastConfirmedAt   *time.Time        `json:"last_confirmed_at,omitempty"`
	Supersedes        string            `json:"supersedes,omitempty"`
	SupersededBy      string            `json:"superseded_by,omitempty"`
	SupersededAt      *time.Time        `json:"superseded_at,omitempty"`
	SourceEventIDs    []string          `json:"source_event_ids,omitempty"`
	SourceSummaryIDs  []string          `json:"source_summary_ids,omitempty"`
	SourceArtifactIDs []string          `json:"source_artifact_ids,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	Freshness         float64           `json:"freshness,omitempty"` // computed at query time
}

// Claim is a structured atomic assertion linked to a memory.
type Claim struct {
	ID              string            `json:"id"`
	MemoryID        string            `json:"memory_id"`
	SubjectEntityID string            `json:"subject_entity_id,omitempty"`
	Predicate       string            `json:"predicate"`
	ObjectValue     string            `json:"object_value,omitempty"`
	ObjectEntityID  string            `json:"object_entity_id,omitempty"`
	Confidence      float64           `json:"confidence"`
	SourceEventID   string            `json:"source_event_id,omitempty"`
	SourceSummaryID string            `json:"source_summary_id,omitempty"`
	ObservedAt      *time.Time        `json:"observed_at,omitempty"`
	ValidFrom       *time.Time        `json:"valid_from,omitempty"`
	ValidTo         *time.Time        `json:"valid_to,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Entity is a canonical entity record.
type Entity struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	CanonicalName string            `json:"canonical_name"`
	Aliases       []string          `json:"aliases,omitempty"`
	Description   string            `json:"description,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type MemoryEntityLink struct {
	MemoryID string `json:"memory_id"`
	EntityID string `json:"entity_id"`
	Role     string `json:"role,omitempty"`
}

// Episode is a narrative memory unit.
type Episode struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Summary          string            `json:"summary"`
	TightDescription string            `json:"tight_description"`
	Scope            Scope             `json:"scope"`
	ProjectID        string            `json:"project_id,omitempty"`
	SessionID        string            `json:"session_id,omitempty"`
	Importance       float64           `json:"importance"`
	PrivacyLevel     PrivacyLevel      `json:"privacy_level"`
	StartedAt        *time.Time        `json:"started_at,omitempty"`
	EndedAt          *time.Time        `json:"ended_at,omitempty"`
	SourceSpan       SourceSpan        `json:"source_span"`
	SourceSummaryIDs []string          `json:"source_summary_ids,omitempty"`
	Participants     []string          `json:"participants,omitempty"`
	RelatedEntities  []string          `json:"related_entities,omitempty"`
	Outcomes         []string          `json:"outcomes,omitempty"`
	UnresolvedItems  []string          `json:"unresolved_items,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// Artifact is an ingested non-message source material.
type Artifact struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	SourceSystem string            `json:"source_system,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	Path         string            `json:"path,omitempty"`
	Content      string            `json:"content,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

// Job represents a maintenance/worker job.
type Job struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Status      string            `json:"status"`
	Payload     map[string]string `json:"payload,omitempty"`
	Result      map[string]string `json:"result,omitempty"`
	ErrorText   string            `json:"error_text,omitempty"`
	ScheduledAt *time.Time        `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	FinishedAt  *time.Time        `json:"finished_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// IngestionPolicy defines read/write behavior for a source pattern.
type IngestionPolicy struct {
	ID          string            `json:"id"`
	PatternType string            `json:"pattern_type"` // session, source, surface, agent, project, runtime
	Pattern     string            `json:"pattern"`
	Mode        string            `json:"mode"` // full, read_only, ignore
	Priority    int               `json:"priority,omitempty"`
	MatchMode   string            `json:"match_mode,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// RecallItem is a thin retrieval result for ambient/describe responses.
type RecallItem struct {
	ID               string             `json:"id"`
	Kind             string             `json:"kind"` // memory, episode, summary, history-node
	Type             string             `json:"type,omitempty"`
	Scope            Scope              `json:"scope"`
	Score            float64            `json:"score"`
	Signals          map[string]float64 `json:"signals,omitempty"`
	TightDescription string             `json:"tight_description"`
	Confidence       *float64           `json:"confidence,omitempty"`
	ObservedAt       string             `json:"observed_at,omitempty"`
}

// RecallResult is the response from a recall operation.
type RecallResult struct {
	Items []RecallItem `json:"items"`
	Meta  RecallMeta   `json:"meta"`
}

// RecallMeta contains metadata about a recall operation.
type RecallMeta struct {
	Mode        RecallMode `json:"mode"`
	QueryTimeMs int64      `json:"query_time_ms"`
}

type RecallRecord struct {
	ItemID   string `json:"item_id"`
	ItemKind string `json:"item_kind"`
}

// ExpandResult contains the full expansion of a memory, summary, or episode.
type ExpandResult struct {
	Memory   *Memory   `json:"memory,omitempty"`
	Summary  *Summary  `json:"summary,omitempty"`
	Episode  *Episode  `json:"episode,omitempty"`
	Claims   []Claim   `json:"claims,omitempty"`
	Events   []Event   `json:"events,omitempty"`
	Children []Summary `json:"children,omitempty"`
}

type ExpandOptions struct {
	SessionID string `json:"session_id,omitempty"`
}

// DescribeResult is a thin description of one item.
type DescribeResult struct {
	ID               string       `json:"id"`
	Kind             string       `json:"kind"`
	Type             string       `json:"type,omitempty"`
	Scope            Scope        `json:"scope"`
	TightDescription string       `json:"tight_description"`
	Status           MemoryStatus `json:"status,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
}

// RepairReport contains results from an integrity check or repair.
type RepairReport struct {
	Checked int      `json:"checked"`
	Issues  int      `json:"issues"`
	Fixed   int      `json:"fixed"`
	Details []string `json:"details,omitempty"`
}

// StatusResult contains system status information.
type StatusResult struct {
	DBPath       string `json:"db_path"`
	Initialized  bool   `json:"initialized"`
	EventCount   int64  `json:"event_count"`
	MemoryCount  int64  `json:"memory_count"`
	SummaryCount int64  `json:"summary_count"`
	EpisodeCount int64  `json:"episode_count"`
	EntityCount  int64  `json:"entity_count"`
}

type ResetDerivedResult struct {
	MemoryEntitiesDeleted        int64 `json:"memory_entities_deleted"`
	SummaryEdgesDeleted          int64 `json:"summary_edges_deleted"`
	MemoriesDeleted              int64 `json:"memories_deleted"`
	ClaimsDeleted                int64 `json:"claims_deleted"`
	EntitiesDeleted              int64 `json:"entities_deleted"`
	RelationshipsDeleted         int64 `json:"relationships_deleted"`
	SummariesDeleted             int64 `json:"summaries_deleted"`
	EpisodesDeleted              int64 `json:"episodes_deleted"`
	JobsDeleted                  int64 `json:"jobs_deleted"`
	MemoriesFTSDeleted           int64 `json:"memories_fts_deleted"`
	SummariesFTSDeleted          int64 `json:"summaries_fts_deleted"`
	EpisodesFTSDeleted           int64 `json:"episodes_fts_deleted"`
	EmbeddingsDeleted            int64 `json:"embeddings_deleted"`
	RetrievalCacheDeleted        int64 `json:"retrieval_cache_deleted"`
	RecallHistoryDeleted         int64 `json:"recall_history_deleted"`
	RelevanceFeedbackDeleted     int64 `json:"relevance_feedback_deleted"`
	EntityGraphProjectionDeleted int64 `json:"entity_graph_projection_deleted"`
	EventsReset                  int64 `json:"events_reset"`
}

// Project represents a registered project with metadata.
type Project struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Path        string            `json:"path,omitempty"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Relationship represents a directed relationship between two entities.
type Relationship struct {
	ID               string            `json:"id"`
	FromEntityID     string            `json:"from_entity_id"`
	ToEntityID       string            `json:"to_entity_id"`
	RelationshipType string            `json:"relationship_type"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}
