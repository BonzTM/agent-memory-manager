package v1

// IngestEventRequest is the request payload for the ingest_event command.
//
// It captures one raw history event exactly as it should be appended to the
// canonical event log.
type IngestEventRequest struct {
	Kind         string            `json:"kind"`
	SourceSystem string            `json:"source_system"`
	Surface      string            `json:"surface,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	AgentID      string            `json:"agent_id,omitempty"`
	ActorType    string            `json:"actor_type,omitempty"`
	ActorID      string            `json:"actor_id,omitempty"`
	PrivacyLevel string            `json:"privacy_level,omitempty"`
	Content      string            `json:"content"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	OccurredAt   string            `json:"occurred_at,omitempty"`
}

// IngestEventResponse is the response payload returned by ingest_event.
//
// It identifies the stored event and records when ingestion completed.
type IngestEventResponse struct {
	ID         string `json:"id"`
	IngestedAt string `json:"ingested_at"`
}

// IngestTranscriptRequest is the request payload for the ingest_transcript
// command.
//
// It wraps a batch of raw events that should be appended in a single call.
type IngestTranscriptRequest struct {
	Events []IngestEventRequest `json:"events"`
}

// IngestTranscriptResponse is the response payload returned by
// ingest_transcript.
//
// It reports how many events were accepted from the submitted batch.
type IngestTranscriptResponse struct {
	Ingested int `json:"ingested"`
}

// RememberRequest is the request payload for the remember command.
//
// It describes a durable memory record to create, including its type, scope,
// body, and optional ranking or provenance metadata.
type RememberRequest struct {
	Type             string            `json:"type"`
	Scope            string            `json:"scope,omitempty"`
	ProjectID        string            `json:"project_id,omitempty"`
	SessionID        string            `json:"session_id,omitempty"`
	AgentID          string            `json:"agent_id,omitempty"`
	Subject          string            `json:"subject,omitempty"`
	Body             string            `json:"body"`
	TightDescription string            `json:"tight_description"`
	Confidence       *float64          `json:"confidence,omitempty"`
	Importance       *float64          `json:"importance,omitempty"`
	PrivacyLevel     string            `json:"privacy_level,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	SourceEventIDs   []string          `json:"source_event_ids,omitempty"`
}

// RememberResponse is the response payload returned by remember.
//
// It identifies the newly created memory and records when it was written.
type RememberResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

// RecallRequest is the request payload for the recall command.
//
// It defines the search phrase and optional recall controls such as mode,
// scope filters, entity filters, and result limits.
type RecallRequest struct {
	Query     string   `json:"query"`
	Mode      string   `json:"mode,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	AgentID   string   `json:"agent_id,omitempty"`
	EntityIDs []string `json:"entity_ids,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Explain   bool     `json:"explain,omitempty"`
}

// RecallResponse is the response payload returned by recall.
//
// It contains the ranked result set together with metadata about how recall
// was performed.
type RecallResponse struct {
	Items []RecallItemResponse `json:"items"`
	Meta  RecallMetaResponse   `json:"meta"`
}

// RecallItemResponse is a single ranked hit returned in a RecallResponse.
//
// It exposes the thin fields needed to display or decide whether to expand a
// recalled item.
type RecallItemResponse struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Type             string   `json:"type,omitempty"`
	Scope            string   `json:"scope"`
	Score            float64  `json:"score"`
	TightDescription string   `json:"tight_description"`
	Confidence       *float64 `json:"confidence,omitempty"`
	ObservedAt       string   `json:"observed_at,omitempty"`
}

// RecallMetaResponse contains metadata about a recall operation.
//
// It reports the effective recall mode and basic timing data for the query.
type RecallMetaResponse struct {
	Mode        string `json:"mode"`
	QueryTimeMs int64  `json:"query_time_ms"`
}

// DescribeRequest is the request payload for the describe command.
//
// It lists the item IDs that should be resolved to thin descriptions.
type DescribeRequest struct {
	IDs []string `json:"ids"`
}

// DescribeResponse is the response payload returned by describe.
//
// It contains one thin description per requested item.
type DescribeResponse struct {
	Items []DescribeItemResponse `json:"items"`
}

// DescribeItemResponse is a single thin item description returned by describe.
//
// It includes identity, classification, and lifecycle metadata without a full
// expansion payload.
type DescribeItemResponse struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Type             string `json:"type,omitempty"`
	Scope            string `json:"scope"`
	TightDescription string `json:"tight_description"`
	Status           string `json:"status,omitempty"`
	CreatedAt        string `json:"created_at"`
}

// ExpandRequest is the request payload for the expand command.
//
// It identifies which item should be expanded and, when known, the item kind
// used to resolve the correct expansion path.
type ExpandRequest struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	SessionID string `json:"session_id,omitempty"`
}

// ExpandResponse is the response payload returned by expand.
//
// It carries the fully expanded representation of the requested item plus any
// linked claims, events, or child records.
type ExpandResponse struct {
	Memory   interface{}   `json:"memory,omitempty"`
	Summary  interface{}   `json:"summary,omitempty"`
	Episode  interface{}   `json:"episode,omitempty"`
	Claims   []interface{} `json:"claims,omitempty"`
	Events   []interface{} `json:"events,omitempty"`
	Children []interface{} `json:"children,omitempty"`
}

// HistoryRequest is the request payload for the history command.
//
// It defines optional full-text, session, project, limit, and time-bound
// filters over raw event history.
type HistoryRequest struct {
	Query     string `json:"query,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
}

// HistoryResponse is the response payload returned by history.
//
// It returns matching raw events in the order chosen by the service.
type HistoryResponse struct {
	Events []HistoryEventResponse `json:"events"`
}

// HistoryEventResponse is a single raw event returned in a HistoryResponse.
//
// It mirrors the stored event fields needed to inspect historical interaction
// records.
type HistoryEventResponse struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	SourceSystem string            `json:"source_system"`
	Surface      string            `json:"surface,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	ActorType    string            `json:"actor_type,omitempty"`
	Content      string            `json:"content"`
	PrivacyLevel string            `json:"privacy_level"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	OccurredAt   string            `json:"occurred_at"`
	IngestedAt   string            `json:"ingested_at"`
}

// RunJobRequest is the request payload for the run_job command.
//
// It names the maintenance job that should be executed.
type RunJobRequest struct {
	Kind string `json:"kind"`
}

// RunJobResponse is the response payload returned by run_job.
//
// It describes the executed maintenance job, including status, timing, and any
// machine-readable result details.
type RunJobResponse struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Status     string            `json:"status"`
	Result     map[string]string `json:"result,omitempty"`
	ErrorText  string            `json:"error_text,omitempty"`
	StartedAt  string            `json:"started_at,omitempty"`
	FinishedAt string            `json:"finished_at,omitempty"`
}

// RepairRequest is the request payload for the repair command.
//
// It controls whether integrity checks should be read-only and which repair
// target, if any, should be fixed.
type RepairRequest struct {
	Check bool   `json:"check"`
	Fix   string `json:"fix,omitempty"`
}

// RepairResponse is the response payload returned by repair.
//
// It summarizes how many checks ran, how many issues were found, and what was
// repaired.
type RepairResponse struct {
	Checked int      `json:"checked"`
	Issues  int      `json:"issues"`
	Fixed   int      `json:"fixed"`
	Details []string `json:"details,omitempty"`
}

// StatusResponse is the response payload returned by status.
//
// It reports database initialization state and high-level object counts for the
// current store.
type StatusResponse struct {
	DBPath       string `json:"db_path"`
	Initialized  bool   `json:"initialized"`
	EventCount   int64  `json:"event_count"`
	MemoryCount  int64  `json:"memory_count"`
	SummaryCount int64  `json:"summary_count"`
	EpisodeCount int64  `json:"episode_count"`
	EntityCount  int64  `json:"entity_count"`
}

// ExplainRecallRequest is the request payload for the explain_recall command.
//
// It pairs a query with a surfaced item so the service can explain that recall
// result.
type ExplainRecallRequest struct {
	Query  string `json:"query"`
	ItemID string `json:"item_id"`
}

// ExplainRecallResponse is the response payload returned by explain_recall.
//
// It contains a structured explanation of the signals that caused an item to
// surface.
type ExplainRecallResponse struct {
	Explanation map[string]interface{} `json:"explanation"`
}

// GetMemoryRequest is the request payload for the get_memory command.
//
// It identifies the durable memory record to fetch.
type GetMemoryRequest struct {
	ID string `json:"id"`
}

type ShareRequest struct {
	ID      string `json:"id"`
	Privacy string `json:"privacy"`
}

// UpdateMemoryRequest is the request payload for the update_memory command.
//
// It identifies an existing memory and supplies any mutable fields that should
// be updated in place.
type UpdateMemoryRequest struct {
	ID               string            `json:"id"`
	Body             string            `json:"body,omitempty"`
	TightDescription string            `json:"tight_description,omitempty"`
	Type             string            `json:"type,omitempty"`
	Scope            string            `json:"scope,omitempty"`
	Status           string            `json:"status,omitempty"`
	Confidence       *float64          `json:"confidence,omitempty"`
	Importance       *float64          `json:"importance,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// PolicyListRequest is the request payload for the policy_list command.
//
// It is empty because listing ingestion policies does not require any input.
type PolicyListRequest struct{}

// PolicyListResponse is the response payload returned by policy_list.
//
// It contains the full set of configured ingestion policies.
type PolicyListResponse struct {
	Policies []PolicyResponse `json:"policies"`
}

// PolicyAddRequest is the request payload for the policy_add command.
//
// It defines the policy pattern, pattern type, and ingestion mode to create.
type PolicyAddRequest struct {
	PatternType string            `json:"pattern_type"`
	Pattern     string            `json:"pattern"`
	Mode        string            `json:"mode"`
	Priority    int               `json:"priority,omitempty"`
	MatchMode   string            `json:"match_mode,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// PolicyAddResponse is the response payload returned by policy_add.
//
// It identifies the newly created ingestion policy.
type PolicyAddResponse struct {
	ID string `json:"id"`
}

// PolicyRemoveRequest is the request payload for the policy_remove command.
//
// It identifies the ingestion policy to delete.
type PolicyRemoveRequest struct {
	ID string `json:"id"`
}

// PolicyResponse is a single ingestion policy returned by policy APIs.
//
// It includes the stored policy definition together with lifecycle timestamps.
type PolicyResponse struct {
	ID          string            `json:"id"`
	PatternType string            `json:"pattern_type"`
	Pattern     string            `json:"pattern"`
	Mode        string            `json:"mode"`
	Priority    int               `json:"priority,omitempty"`
	MatchMode   string            `json:"match_mode,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}
