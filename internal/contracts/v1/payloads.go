package v1

// IngestEventRequest is the payload for the ingest_event command.
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

// IngestEventResponse is the response from the ingest_event command.
type IngestEventResponse struct {
	ID         string `json:"id"`
	IngestedAt string `json:"ingested_at"`
}

// IngestTranscriptRequest is the payload for the ingest_transcript command.
type IngestTranscriptRequest struct {
	Events []IngestEventRequest `json:"events"`
}

// IngestTranscriptResponse is the response from the ingest_transcript command.
type IngestTranscriptResponse struct {
	Ingested int `json:"ingested"`
}

// RememberRequest is the payload for the remember command.
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

// RememberResponse is the response from the remember command.
type RememberResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

// RecallRequest is the payload for the recall command.
type RecallRequest struct {
	Query     string   `json:"query"`
	Mode      string   `json:"mode,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	EntityIDs []string `json:"entity_ids,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Explain   bool     `json:"explain,omitempty"`
}

// RecallResponse is the response from the recall command.
type RecallResponse struct {
	Items []RecallItemResponse `json:"items"`
	Meta  RecallMetaResponse   `json:"meta"`
}

// RecallItemResponse is a single item in a recall response.
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
type RecallMetaResponse struct {
	Mode        string `json:"mode"`
	QueryTimeMs int64  `json:"query_time_ms"`
}

// DescribeRequest is the payload for the describe command.
type DescribeRequest struct {
	IDs []string `json:"ids"`
}

// DescribeResponse is the response from the describe command.
type DescribeResponse struct {
	Items []DescribeItemResponse `json:"items"`
}

// DescribeItemResponse is a single item in a describe response.
type DescribeItemResponse struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Type             string `json:"type,omitempty"`
	Scope            string `json:"scope"`
	TightDescription string `json:"tight_description"`
	Status           string `json:"status,omitempty"`
	CreatedAt        string `json:"created_at"`
}

// ExpandRequest is the payload for the expand command.
type ExpandRequest struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// ExpandResponse is the response from the expand command.
type ExpandResponse struct {
	Memory   interface{}   `json:"memory,omitempty"`
	Summary  interface{}   `json:"summary,omitempty"`
	Episode  interface{}   `json:"episode,omitempty"`
	Claims   []interface{} `json:"claims,omitempty"`
	Events   []interface{} `json:"events,omitempty"`
	Children []interface{} `json:"children,omitempty"`
}

// HistoryRequest is the payload for the history command.
type HistoryRequest struct {
	Query     string `json:"query,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
}

// HistoryResponse is the response from the history command.
type HistoryResponse struct {
	Events []HistoryEventResponse `json:"events"`
}

// HistoryEventResponse is a single event in a history response.
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

// RunJobRequest is the payload for the run_job command.
type RunJobRequest struct {
	Kind string `json:"kind"`
}

// RunJobResponse is the response from the run_job command.
type RunJobResponse struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Status     string            `json:"status"`
	Result     map[string]string `json:"result,omitempty"`
	ErrorText  string            `json:"error_text,omitempty"`
	StartedAt  string            `json:"started_at,omitempty"`
	FinishedAt string            `json:"finished_at,omitempty"`
}

// RepairRequest is the payload for the repair command.
type RepairRequest struct {
	Check bool   `json:"check"`
	Fix   string `json:"fix,omitempty"`
}

// RepairResponse is the response from the repair command.
type RepairResponse struct {
	Checked int      `json:"checked"`
	Issues  int      `json:"issues"`
	Fixed   int      `json:"fixed"`
	Details []string `json:"details,omitempty"`
}

// StatusResponse is the response from the status command.
type StatusResponse struct {
	DBPath       string `json:"db_path"`
	Initialized  bool   `json:"initialized"`
	EventCount   int64  `json:"event_count"`
	MemoryCount  int64  `json:"memory_count"`
	SummaryCount int64  `json:"summary_count"`
	EpisodeCount int64  `json:"episode_count"`
	EntityCount  int64  `json:"entity_count"`
}

// ExplainRecallRequest is the payload for the explain_recall command.
type ExplainRecallRequest struct {
	Query  string `json:"query"`
	ItemID string `json:"item_id"`
}

// ExplainRecallResponse is the response from the explain_recall command.
type ExplainRecallResponse struct {
	Explanation map[string]interface{} `json:"explanation"`
}

// GetMemoryRequest is the payload for the get_memory command.
type GetMemoryRequest struct {
	ID string `json:"id"`
}

// UpdateMemoryRequest is the payload for the update_memory command.
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

type PolicyListRequest struct{}

type PolicyListResponse struct {
	Policies []PolicyResponse `json:"policies"`
}

type PolicyAddRequest struct {
	PatternType string            `json:"pattern_type"`
	Pattern     string            `json:"pattern"`
	Mode        string            `json:"mode"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type PolicyAddResponse struct {
	ID string `json:"id"`
}

type PolicyRemoveRequest struct {
	ID string `json:"id"`
}

type PolicyResponse struct {
	ID          string            `json:"id"`
	PatternType string            `json:"pattern_type"`
	Pattern     string            `json:"pattern"`
	Mode        string            `json:"mode"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}
