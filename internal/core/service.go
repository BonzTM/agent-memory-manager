package core

import "context"

// Service is the single entry point for all AMM business logic.
// CLI, MCP, and HTTP are adapters that call through this interface.
type Service interface {
	// Init initializes the database and runs migrations.
	Init(ctx context.Context, dbPath string) error

	// IngestEvent appends a raw event to history.
	IngestEvent(ctx context.Context, event *Event) (*Event, error)

	// IngestTranscript bulk-ingests a sequence of events.
	IngestTranscript(ctx context.Context, events []*Event) (int, error)

	// Remember commits an explicit durable memory.
	Remember(ctx context.Context, memory *Memory) (*Memory, error)

	// Recall performs retrieval using the specified mode.
	Recall(ctx context.Context, query string, opts RecallOptions) (*RecallResult, error)

	// Describe returns thin descriptions for one or more items.
	Describe(ctx context.Context, ids []string) ([]DescribeResult, error)

	// Expand returns the full expansion of a single item.
	Expand(ctx context.Context, id string, kind string) (*ExpandResult, error)

	// History retrieves raw history by query or session.
	History(ctx context.Context, query string, opts HistoryOptions) ([]Event, error)

	// GetMemory retrieves a single memory by ID.
	GetMemory(ctx context.Context, id string) (*Memory, error)

	// UpdateMemory updates an existing memory.
	UpdateMemory(ctx context.Context, memory *Memory) (*Memory, error)

	// GetSummary retrieves a single summary by ID.
	GetSummary(ctx context.Context, id string) (*Summary, error)

	// GetEpisode retrieves a single episode by ID.
	GetEpisode(ctx context.Context, id string) (*Episode, error)

	// GetEntity retrieves a single entity by ID.
	GetEntity(ctx context.Context, id string) (*Entity, error)

	// RunJob executes a maintenance job by kind.
	RunJob(ctx context.Context, kind string) (*Job, error)

	// Repair runs integrity checks and optionally fixes issues.
	Repair(ctx context.Context, check bool, fix string) (*RepairReport, error)

	// ExplainRecall explains why an item surfaced for a query.
	ExplainRecall(ctx context.Context, query string, itemID string) (map[string]interface{}, error)

	// Status returns system status information.
	Status(ctx context.Context) (*StatusResult, error)
}

// RecallOptions configures a recall operation.
type RecallOptions struct {
	Mode      RecallMode `json:"mode"`
	ProjectID string     `json:"project_id,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
	EntityIDs []string   `json:"entity_ids,omitempty"`
	Limit     int        `json:"limit,omitempty"`
	Explain   bool       `json:"explain,omitempty"`
}

// HistoryOptions configures a history retrieval.
type HistoryOptions struct {
	SessionID string `json:"session_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
}
