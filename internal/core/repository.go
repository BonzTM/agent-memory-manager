package core

import "context"

// Repository abstracts all persistent storage operations.
// Implementations must handle their own connection management.
type Repository interface {
	// Lifecycle
	Open(ctx context.Context, dbPath string) error
	Close() error
	Migrate(ctx context.Context) error
	IsInitialized(ctx context.Context) (bool, error)

	// Events
	InsertEvent(ctx context.Context, event *Event) error
	GetEvent(ctx context.Context, id string) (*Event, error)
	ListEvents(ctx context.Context, opts ListEventsOptions) ([]Event, error)
	SearchEvents(ctx context.Context, query string, limit int) ([]Event, error)
	MaxEventRowID(ctx context.Context) (int64, error)

	// Summaries
	InsertSummary(ctx context.Context, summary *Summary) error
	GetSummary(ctx context.Context, id string) (*Summary, error)
	ListSummaries(ctx context.Context, opts ListSummariesOptions) ([]Summary, error)
	SearchSummaries(ctx context.Context, query string, limit int) ([]Summary, error)
	GetSummaryChildren(ctx context.Context, parentID string) ([]SummaryEdge, error)
	InsertSummaryEdge(ctx context.Context, edge *SummaryEdge) error

	// Memories
	InsertMemory(ctx context.Context, memory *Memory) error
	GetMemory(ctx context.Context, id string) (*Memory, error)
	UpdateMemory(ctx context.Context, memory *Memory) error
	ListMemories(ctx context.Context, opts ListMemoriesOptions) ([]Memory, error)
	SearchMemories(ctx context.Context, query string, limit int) ([]Memory, error)

	// Claims
	InsertClaim(ctx context.Context, claim *Claim) error
	GetClaim(ctx context.Context, id string) (*Claim, error)
	ListClaimsByMemory(ctx context.Context, memoryID string) ([]Claim, error)

	// Entities
	InsertEntity(ctx context.Context, entity *Entity) error
	GetEntity(ctx context.Context, id string) (*Entity, error)
	ListEntities(ctx context.Context, opts ListEntitiesOptions) ([]Entity, error)
	SearchEntities(ctx context.Context, query string, limit int) ([]Entity, error)
	LinkMemoryEntity(ctx context.Context, memoryID, entityID, role string) error
	GetMemoryEntities(ctx context.Context, memoryID string) ([]Entity, error)

	// Episodes
	InsertEpisode(ctx context.Context, episode *Episode) error
	GetEpisode(ctx context.Context, id string) (*Episode, error)
	ListEpisodes(ctx context.Context, opts ListEpisodesOptions) ([]Episode, error)
	SearchEpisodes(ctx context.Context, query string, limit int) ([]Episode, error)

	// Artifacts
	InsertArtifact(ctx context.Context, artifact *Artifact) error
	GetArtifact(ctx context.Context, id string) (*Artifact, error)

	// Jobs
	InsertJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	UpdateJob(ctx context.Context, job *Job) error
	ListJobs(ctx context.Context, opts ListJobsOptions) ([]Job, error)

	// Ingestion Policies
	InsertIngestionPolicy(ctx context.Context, policy *IngestionPolicy) error
	GetIngestionPolicy(ctx context.Context, id string) (*IngestionPolicy, error)
	ListIngestionPolicies(ctx context.Context) ([]IngestionPolicy, error)
	DeleteIngestionPolicy(ctx context.Context, id string) error
	MatchIngestionPolicy(ctx context.Context, patternType, value string) (*IngestionPolicy, error)

	// Recall History (for repetition suppression)
	RecordRecall(ctx context.Context, sessionID, itemID, itemKind string) error
	GetRecentRecalls(ctx context.Context, sessionID string, limit int) ([]RecallHistoryEntry, error)
	CleanupRecallHistory(ctx context.Context, olderThanDays int) (int64, error)

	// Counts for status
	CountEvents(ctx context.Context) (int64, error)
	CountMemories(ctx context.Context) (int64, error)
	CountSummaries(ctx context.Context) (int64, error)
	CountEpisodes(ctx context.Context) (int64, error)
	CountEntities(ctx context.Context) (int64, error)

	// Index management
	RebuildFTSIndexes(ctx context.Context) error
}

// SummaryEdge represents a parent-child relationship in the summary hierarchy.
type SummaryEdge struct {
	ParentSummaryID string `json:"parent_summary_id"`
	ChildKind       string `json:"child_kind"` // summary or event
	ChildID         string `json:"child_id"`
	EdgeOrder       int    `json:"edge_order,omitempty"`
}

// RecallHistoryEntry tracks what was shown to suppress repetition.
type RecallHistoryEntry struct {
	SessionID string `json:"session_id"`
	ItemID    string `json:"item_id"`
	ItemKind  string `json:"item_kind"`
	ShownAt   string `json:"shown_at"`
}

// List option types for filtered queries.

type ListEventsOptions struct {
	SessionID   string
	ProjectID   string
	Kind        string
	Limit       int
	BeforeRowID int64
	Before      string
	AfterRowID  int64
	After       string
}

type ListSummariesOptions struct {
	Kind      string
	Scope     Scope
	ProjectID string
	SessionID string
	Limit     int
}

type ListMemoriesOptions struct {
	Type      MemoryType
	Scope     Scope
	ProjectID string
	Status    MemoryStatus
	Limit     int
}

type ListEntitiesOptions struct {
	Type  string
	Limit int
}

type ListEpisodesOptions struct {
	Scope     Scope
	ProjectID string
	Limit     int
}

type ListJobsOptions struct {
	Kind   string
	Status string
	Limit  int
}
