package core

import (
	"context"
	"time"
)

// Repository abstracts all persistent storage operations.
// Implementations must handle their own connection management.
type Repository interface {
	// Open opens the repository at dbPath.
	Open(ctx context.Context, dbPath string) error
	// Close releases repository resources.
	Close() error
	// Migrate applies any required schema migrations.
	Migrate(ctx context.Context) error
	// IsInitialized reports whether the repository has been initialized.
	IsInitialized(ctx context.Context) (bool, error)

	// InsertEvent stores an event.
	InsertEvent(ctx context.Context, event *Event) error
	// GetEvent retrieves an event by ID.
	GetEvent(ctx context.Context, id string) (*Event, error)
	// UpdateEvent persists changes to an existing event.
	UpdateEvent(ctx context.Context, event *Event) error
	// ListEvents returns events matching the supplied options.
	ListEvents(ctx context.Context, opts ListEventsOptions) ([]Event, error)
	// SearchEvents searches events by text query.
	SearchEvents(ctx context.Context, query string, limit int) ([]Event, error)
	// CountUnreflectedEvents returns the number of events not yet reflected.
	CountUnreflectedEvents(ctx context.Context) (int64, error)
	// ClaimUnreflectedEvents atomically claims and returns unreflected events for processing.
	// Sets reflected_at to now for claimed events to prevent concurrent processing.
	ClaimUnreflectedEvents(ctx context.Context, limit int) ([]Event, error)

	// InsertSummary stores a summary.
	InsertSummary(ctx context.Context, summary *Summary) error
	// GetSummary retrieves a summary by ID.
	GetSummary(ctx context.Context, id string) (*Summary, error)
	// ListSummaries returns summaries matching the supplied options.
	ListSummaries(ctx context.Context, opts ListSummariesOptions) ([]Summary, error)
	// SearchSummaries searches summaries by text query.
	SearchSummaries(ctx context.Context, query string, limit int) ([]Summary, error)
	// GetSummaryChildren returns the children of a summary node.
	GetSummaryChildren(ctx context.Context, parentID string) ([]SummaryEdge, error)
	ListParentedSummaryIDs(ctx context.Context) (map[string]bool, error)
	// InsertSummaryEdge stores a summary hierarchy edge.
	InsertSummaryEdge(ctx context.Context, edge *SummaryEdge) error

	// InsertMemory stores a memory.
	InsertMemory(ctx context.Context, memory *Memory) error
	// GetMemory retrieves a memory by ID.
	GetMemory(ctx context.Context, id string) (*Memory, error)
	GetMemoriesByIDs(ctx context.Context, ids []string) (map[string]*Memory, error)
	// UpdateMemory persists changes to an existing memory.
	UpdateMemory(ctx context.Context, memory *Memory) error
	// ListMemories returns memories matching the supplied options.
	ListMemories(ctx context.Context, opts ListMemoriesOptions) ([]Memory, error)
	// SearchMemories searches memories by text query.
	SearchMemories(ctx context.Context, query string, opts ListMemoriesOptions) ([]Memory, error)
	SearchMemoriesFuzzy(ctx context.Context, query string, opts ListMemoriesOptions) ([]Memory, error)
	ListMemoriesBySourceEventIDs(ctx context.Context, eventIDs []string) ([]Memory, error)

	// InsertClaim stores a claim.
	InsertClaim(ctx context.Context, claim *Claim) error
	// GetClaim retrieves a claim by ID.
	GetClaim(ctx context.Context, id string) (*Claim, error)
	// ListClaimsByMemory returns all claims attached to a memory.
	ListClaimsByMemory(ctx context.Context, memoryID string) ([]Claim, error)

	// InsertEntity stores an entity.
	InsertEntity(ctx context.Context, entity *Entity) error
	UpdateEntity(ctx context.Context, entity *Entity) error
	// GetEntity retrieves an entity by ID.
	GetEntity(ctx context.Context, id string) (*Entity, error)
	GetEntitiesByIDs(ctx context.Context, ids []string) ([]Entity, error)
	// ListEntities returns entities matching the supplied options.
	ListEntities(ctx context.Context, opts ListEntitiesOptions) ([]Entity, error)
	// SearchEntities searches entities by text query.
	SearchEntities(ctx context.Context, query string, limit int) ([]Entity, error)
	// LinkMemoryEntity links a memory to an entity with a role.
	LinkMemoryEntity(ctx context.Context, memoryID, entityID, role string) error
	LinkMemoryEntitiesBatch(ctx context.Context, links []MemoryEntityLink) error
	// GetMemoryEntities returns entities linked to a memory.
	GetMemoryEntities(ctx context.Context, memoryID string) ([]Entity, error)
	GetMemoryEntitiesBatch(ctx context.Context, memoryIDs []string) (map[string][]Entity, error)
	// CountMemoryEntityLinks returns how many memory links an entity has.
	CountMemoryEntityLinks(ctx context.Context, entityID string) (int64, error)
	CountMemoryEntityLinksBatch(ctx context.Context, entityIDs []string) (map[string]int64, error)
	// CountActiveMemories returns the number of active memories.
	CountActiveMemories(ctx context.Context) (int64, error)

	// InsertProject stores a project.
	InsertProject(ctx context.Context, project *Project) error
	// GetProject retrieves a project by ID.
	GetProject(ctx context.Context, id string) (*Project, error)
	// ListProjects returns all projects.
	ListProjects(ctx context.Context) ([]Project, error)
	// DeleteProject deletes a project by ID.
	DeleteProject(ctx context.Context, id string) error

	// InsertRelationship stores a relationship.
	InsertRelationship(ctx context.Context, rel *Relationship) error
	// GetRelationship retrieves a relationship by ID.
	GetRelationship(ctx context.Context, id string) (*Relationship, error)
	// ListRelationships returns relationships, optionally filtered by entity.
	ListRelationships(ctx context.Context, opts ListRelationshipsOptions) ([]Relationship, error)
	ListRelationshipsByEntityIDs(ctx context.Context, entityIDs []string) ([]Relationship, error)
	InsertRelationshipsBatch(ctx context.Context, rels []*Relationship) error
	ListRelatedEntities(ctx context.Context, entityID string, depth int) ([]RelatedEntity, error)
	RebuildEntityGraphProjection(ctx context.Context) error
	ListProjectedRelatedEntities(ctx context.Context, entityID string) ([]ProjectedRelation, error)
	// DeleteRelationship deletes a relationship by ID.
	DeleteRelationship(ctx context.Context, id string) error

	// InsertEpisode stores an episode.
	InsertEpisode(ctx context.Context, episode *Episode) error
	// GetEpisode retrieves an episode by ID.
	GetEpisode(ctx context.Context, id string) (*Episode, error)
	// ListEpisodes returns episodes matching the supplied options.
	ListEpisodes(ctx context.Context, opts ListEpisodesOptions) ([]Episode, error)
	// SearchEpisodes searches episodes by text query.
	SearchEpisodes(ctx context.Context, query string, limit int) ([]Episode, error)

	// InsertArtifact stores an artifact.
	InsertArtifact(ctx context.Context, artifact *Artifact) error
	// GetArtifact retrieves an artifact by ID.
	GetArtifact(ctx context.Context, id string) (*Artifact, error)

	// InsertJob stores a job.
	InsertJob(ctx context.Context, job *Job) error
	// GetJob retrieves a job by ID.
	GetJob(ctx context.Context, id string) (*Job, error)
	// UpdateJob persists changes to an existing job.
	UpdateJob(ctx context.Context, job *Job) error
	// ListJobs returns jobs matching the supplied options.
	ListJobs(ctx context.Context, opts ListJobsOptions) ([]Job, error)

	// InsertIngestionPolicy stores an ingestion policy.
	InsertIngestionPolicy(ctx context.Context, policy *IngestionPolicy) error
	// GetIngestionPolicy retrieves an ingestion policy by ID.
	GetIngestionPolicy(ctx context.Context, id string) (*IngestionPolicy, error)
	// ListIngestionPolicies returns all ingestion policies.
	ListIngestionPolicies(ctx context.Context) ([]IngestionPolicy, error)
	// DeleteIngestionPolicy deletes an ingestion policy by ID.
	DeleteIngestionPolicy(ctx context.Context, id string) error
	// MatchIngestionPolicy finds the best matching ingestion policy.
	MatchIngestionPolicy(ctx context.Context, patternType, value string) (*IngestionPolicy, error)

	// RecordRecall records that an item was shown during recall.
	RecordRecall(ctx context.Context, sessionID, itemID, itemKind string) error
	RecordRecallBatch(ctx context.Context, sessionID string, items []RecallRecord) error
	// GetRecentRecalls returns the most recent recall history entries.
	GetRecentRecalls(ctx context.Context, sessionID string, limit int) ([]RecallHistoryEntry, error)
	ListMemoryAccessStats(ctx context.Context, since time.Time) ([]MemoryAccessStat, error)
	// CleanupRecallHistory removes old recall history entries.
	CleanupRecallHistory(ctx context.Context, olderThanDays int) (int64, error)
	InsertRelevanceFeedback(ctx context.Context, sessionID, itemID, itemKind, action string) error
	ListRelevanceFeedback(ctx context.Context, itemID string) ([]RelevanceFeedbackEntry, error)
	CountExpandedFeedbackBatch(ctx context.Context, memoryIDs []string) (map[string]int, error)

	UpsertEmbedding(ctx context.Context, embedding *EmbeddingRecord) error
	GetEmbedding(ctx context.Context, objectID, objectKind, model string) (*EmbeddingRecord, error)
	GetEmbeddingsBatch(ctx context.Context, objectIDs []string, objectKind, model string) (map[string]EmbeddingRecord, error)
	ListEmbeddingsByKind(ctx context.Context, objectKind, model string, limit int) ([]EmbeddingRecord, error)
	DeleteEmbeddings(ctx context.Context, objectID, objectKind, model string) error
	ListUnembeddedMemories(ctx context.Context, model string, limit int) ([]Memory, error)
	ListUnembeddedSummaries(ctx context.Context, model string, limit int) ([]Summary, error)
	ListUnembeddedEpisodes(ctx context.Context, model string, limit int) ([]Episode, error)

	// CountEvents returns the total number of events.
	CountEvents(ctx context.Context) (int64, error)
	// CountMemories returns the total number of memories.
	CountMemories(ctx context.Context) (int64, error)
	// CountSummaries returns the total number of summaries.
	CountSummaries(ctx context.Context) (int64, error)
	// CountEpisodes returns the total number of episodes.
	CountEpisodes(ctx context.Context) (int64, error)
	// CountEntities returns the total number of entities.
	CountEntities(ctx context.Context) (int64, error)

	// RebuildFTSIndexes rebuilds any full-text search indexes.
	RebuildFTSIndexes(ctx context.Context) error
	ResetDerived(ctx context.Context) (*ResetDerivedResult, error)
}

// SummaryEdge represents a parent-child relationship in the summary hierarchy.
type SummaryEdge struct {
	ParentSummaryID string `json:"parent_summary_id"`
	ChildKind       string `json:"child_kind"` // summary or event
	ChildID         string `json:"child_id"`
	EdgeOrder       int    `json:"edge_order,omitempty"`
}

type RelatedEntity struct {
	Entity       Entity `json:"entity"`
	HopDistance  int    `json:"hop_distance"`
	Relationship string `json:"relationship"`
}

type ProjectedRelation struct {
	RelatedEntityID  string  `json:"related_entity_id"`
	HopDistance      int     `json:"hop_distance"`
	RelationshipPath string  `json:"relationship_path,omitempty"`
	Score            float64 `json:"score"`
}

// RecallHistoryEntry tracks a displayed recall item for repetition suppression.
type RecallHistoryEntry struct {
	SessionID string `json:"session_id"`
	ItemID    string `json:"item_id"`
	ItemKind  string `json:"item_kind"`
	ShownAt   string `json:"shown_at"`
}

type MemoryAccessStat struct {
	MemoryID       string `json:"memory_id"`
	AccessCount    int    `json:"access_count"`
	LastAccessedAt string `json:"last_accessed_at"`
}

type RelevanceFeedbackEntry struct {
	SessionID string    `json:"session_id"`
	ItemID    string    `json:"item_id"`
	ItemKind  string    `json:"item_kind"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
}

type EmbeddingRecord struct {
	ObjectID   string    `json:"object_id"`
	ObjectKind string    `json:"object_kind"`
	Model      string    `json:"model"`
	Vector     []float32 `json:"vector"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListEventsOptions filters event list queries.
type ListEventsOptions struct {
	SessionID        string
	ProjectID        string
	Kind             string
	Limit            int
	BeforeSequenceID int64
	Before           string
	AfterSequenceID  int64
	After            string
	UnreflectedOnly  bool
}

// ListSummariesOptions filters summary list queries.
type ListSummariesOptions struct {
	Kind      string
	Scope     Scope
	ProjectID string
	SessionID string
	Limit     int
}

// ListMemoriesOptions filters memory list queries.
type ListMemoriesOptions struct {
	Type      MemoryType
	Scope     Scope
	ProjectID string
	AgentID   string
	Status    MemoryStatus
	Limit     int
}

// ListEntitiesOptions filters entity list queries.
type ListEntitiesOptions struct {
	Type  string
	Limit int
}

// ListRelationshipsOptions filters relationship queries.
type ListRelationshipsOptions struct {
	EntityID         string // matches from_entity_id OR to_entity_id
	RelationshipType string
	Limit            int
}

// ListEpisodesOptions filters episode list queries.
type ListEpisodesOptions struct {
	Scope     Scope
	ProjectID string
	Limit     int
}

// ListJobsOptions filters job list queries.
type ListJobsOptions struct {
	Kind   string
	Status string
	Limit  int
}
