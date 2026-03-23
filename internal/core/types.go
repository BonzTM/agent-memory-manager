package core

import "time"

// Scope represents where a memory belongs.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
	ScopeSession Scope = "session"
)

// PrivacyLevel controls who can see a memory.
type PrivacyLevel string

const (
	PrivacyPrivate    PrivacyLevel = "private"
	PrivacyShared     PrivacyLevel = "shared"
	PrivacyPublicSafe PrivacyLevel = "public_safe"
)

// MemoryStatus represents the lifecycle state of a memory.
type MemoryStatus string

const (
	MemoryStatusActive     MemoryStatus = "active"
	MemoryStatusSuperseded MemoryStatus = "superseded"
	MemoryStatusArchived   MemoryStatus = "archived"
	MemoryStatusRetracted  MemoryStatus = "retracted"
)

// MemoryType is the kind of durable memory record.
type MemoryType string

const (
	MemoryTypeIdentity      MemoryType = "identity"
	MemoryTypePreference    MemoryType = "preference"
	MemoryTypeFact          MemoryType = "fact"
	MemoryTypeDecision      MemoryType = "decision"
	MemoryTypeEpisode       MemoryType = "episode"
	MemoryTypeTodo          MemoryType = "todo"
	MemoryTypeRelationship  MemoryType = "relationship"
	MemoryTypeProcedure     MemoryType = "procedure"
	MemoryTypeConstraint    MemoryType = "constraint"
	MemoryTypeIncident      MemoryType = "incident"
	MemoryTypeArtifact      MemoryType = "artifact"
	MemoryTypeSummary       MemoryType = "summary"
	MemoryTypeActiveContext MemoryType = "active_context"
	MemoryTypeOpenLoop      MemoryType = "open_loop"
	MemoryTypeAssumption    MemoryType = "assumption"
	MemoryTypeContradiction MemoryType = "contradiction"
)

// RecallMode selects the retrieval strategy.
type RecallMode string

const (
	RecallModeAmbient  RecallMode = "ambient"
	RecallModeFacts    RecallMode = "facts"
	RecallModeEpisodes RecallMode = "episodes"
	RecallModeTimeline RecallMode = "timeline"
	RecallModeProject  RecallMode = "project"
	RecallModeEntity   RecallMode = "entity"
	RecallModeActive   RecallMode = "active"
	RecallModeHistory  RecallMode = "history"
	RecallModeHybrid   RecallMode = "hybrid"
)

// Event is an append-only raw interaction record.
type Event struct {
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
}

// Summary is a compression layer object over history.
type Summary struct {
	ID               string            `json:"id"`
	Kind             string            `json:"kind"` // leaf, session, topic, episode, condensed
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
	ID               string            `json:"id"`
	Type             MemoryType        `json:"type"`
	Scope            Scope             `json:"scope"`
	ProjectID        string            `json:"project_id,omitempty"`
	SessionID        string            `json:"session_id,omitempty"`
	AgentID          string            `json:"agent_id,omitempty"`
	Subject          string            `json:"subject,omitempty"`
	Body             string            `json:"body"`
	TightDescription string            `json:"tight_description"`
	Confidence       float64           `json:"confidence"`
	Importance       float64           `json:"importance"`
	PrivacyLevel     PrivacyLevel      `json:"privacy_level"`
	Status           MemoryStatus      `json:"status"`
	ObservedAt       *time.Time        `json:"observed_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	ValidFrom        *time.Time        `json:"valid_from,omitempty"`
	ValidTo          *time.Time        `json:"valid_to,omitempty"`
	LastConfirmedAt  *time.Time        `json:"last_confirmed_at,omitempty"`
	Supersedes       string            `json:"supersedes,omitempty"`
	SupersededBy     string            `json:"superseded_by,omitempty"`
	SupersededAt     *time.Time        `json:"superseded_at,omitempty"`
	SourceEventIDs   []string          `json:"source_event_ids,omitempty"`
	SourceSummaryIDs []string          `json:"source_summary_ids,omitempty"`
	SourceArtifactIDs []string         `json:"source_artifact_ids,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Freshness        float64           `json:"freshness,omitempty"` // computed at query time
}

// Claim is a structured atomic assertion linked to a memory.
type Claim struct {
	ID              string     `json:"id"`
	MemoryID        string     `json:"memory_id"`
	SubjectEntityID string     `json:"subject_entity_id,omitempty"`
	Predicate       string     `json:"predicate"`
	ObjectValue     string     `json:"object_value,omitempty"`
	ObjectEntityID  string     `json:"object_entity_id,omitempty"`
	Confidence      float64    `json:"confidence"`
	SourceEventID   string     `json:"source_event_id,omitempty"`
	SourceSummaryID string     `json:"source_summary_id,omitempty"`
	ObservedAt      *time.Time `json:"observed_at,omitempty"`
	ValidFrom       *time.Time `json:"valid_from,omitempty"`
	ValidTo         *time.Time `json:"valid_to,omitempty"`
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
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// RecallItem is a thin retrieval result for ambient/describe responses.
type RecallItem struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"` // memory, episode, summary, history-node
	Type             string   `json:"type,omitempty"`
	Scope            Scope    `json:"scope"`
	Score            float64  `json:"score"`
	TightDescription string   `json:"tight_description"`
	Confidence       *float64 `json:"confidence,omitempty"`
	ObservedAt       string   `json:"observed_at,omitempty"`
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

// ExpandResult contains the full expansion of a memory, summary, or episode.
type ExpandResult struct {
	Memory   *Memory   `json:"memory,omitempty"`
	Summary  *Summary  `json:"summary,omitempty"`
	Episode  *Episode  `json:"episode,omitempty"`
	Claims   []Claim   `json:"claims,omitempty"`
	Events   []Event   `json:"events,omitempty"`
	Children []Summary `json:"children,omitempty"`
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
	Checked   int      `json:"checked"`
	Issues    int      `json:"issues"`
	Fixed     int      `json:"fixed"`
	Details   []string `json:"details,omitempty"`
}

// StatusResult contains system status information.
type StatusResult struct {
	DBPath      string `json:"db_path"`
	Initialized bool   `json:"initialized"`
	EventCount  int64  `json:"event_count"`
	MemoryCount int64  `json:"memory_count"`
	SummaryCount int64 `json:"summary_count"`
	EpisodeCount int64 `json:"episode_count"`
	EntityCount  int64 `json:"entity_count"`
}
