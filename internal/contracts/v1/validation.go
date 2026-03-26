package v1

import (
	"fmt"
	"strings"
)

// Known valid values for validated fields.
var (
	validScopes = map[string]bool{
		"global":  true,
		"project": true,
		"session": true,
	}

	validMemoryTypes = map[string]bool{
		"identity":       true,
		"preference":     true,
		"fact":           true,
		"decision":       true,
		"episode":        true,
		"todo":           true,
		"relationship":   true,
		"procedure":      true,
		"constraint":     true,
		"incident":       true,
		"artifact":       true,
		"summary":        true,
		"active_context": true,
		"open_loop":      true,
		"assumption":     true,
		"contradiction":  true,
	}

	validRecallModes = map[string]bool{
		"ambient":  true,
		"facts":    true,
		"episodes": true,
		"timeline": true,
		"project":  true,
		"entity":   true,
		"active":   true,
		"history":  true,
		"hybrid":   true,
	}

	validPrivacyLevels = map[string]bool{
		"private":     true,
		"shared":      true,
		"public_safe": true,
	}

	validMemoryStatuses = map[string]bool{
		"active":     true,
		"superseded": true,
		"archived":   true,
		"retracted":  true,
	}

	validJobKinds = map[string]bool{
		"reflect":                true,
		"compress_history":       true,
		"consolidate_sessions":   true,
		"build_topic_summaries":  true,
		"rebuild_indexes":        true,
		"rebuild_indexes_full":   true,
		"extract_claims":         true,
		"enrich_memories":        true,
		"form_episodes":          true,
		"detect_contradictions":  true,
		"decay_stale_memory":     true,
		"merge_duplicates":       true,
		"cleanup_recall_history": true,
		"reprocess":              true,
		"reprocess_all":          true,
		"promote_high_value":     true,
		"lifecycle_review":       true,
		"cross_project_transfer": true,
		"rebuild_entity_graph":   true,
		"archive_session_traces": true,
		"update_ranking_weights": true,
	}

	validPolicyPatternTypes = map[string]bool{
		"session": true,
		"source":  true,
		"surface": true,
		"agent":   true,
		"project": true,
		"runtime": true,
	}

	validPolicyModes = map[string]bool{
		"full":      true,
		"read_only": true,
		"ignore":    true,
	}

	validPolicyMatchModes = map[string]bool{
		"exact": true,
		"glob":  true,
		"regex": true,
	}
)

// ValidateIngestEvent validates an IngestEventRequest before the ingest_event
// command is executed.
func ValidateIngestEvent(req *IngestEventRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.Kind) == "" {
		return fmt.Errorf("kind is required")
	}
	if strings.TrimSpace(req.SourceSystem) == "" {
		return fmt.Errorf("source_system is required")
	}
	if strings.TrimSpace(req.Content) == "" {
		return fmt.Errorf("content is required")
	}
	if req.PrivacyLevel != "" && !validPrivacyLevels[req.PrivacyLevel] {
		return fmt.Errorf("invalid privacy_level %q: must be one of private, shared, public_safe", req.PrivacyLevel)
	}
	return nil
}

// ValidateIngestTranscript validates an IngestTranscriptRequest and all nested
// events before bulk ingestion.
func ValidateIngestTranscript(req *IngestTranscriptRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if len(req.Events) == 0 {
		return fmt.Errorf("events list is empty")
	}
	for i, evt := range req.Events {
		if err := ValidateIngestEvent(&evt); err != nil {
			return fmt.Errorf("event[%d]: %w", i, err)
		}
	}
	return nil
}

// ValidateRemember validates a RememberRequest before creating a durable
// memory.
func ValidateRemember(req *RememberRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.Type) == "" {
		return fmt.Errorf("type is required")
	}
	if !validMemoryTypes[req.Type] {
		return fmt.Errorf("invalid type %q", req.Type)
	}
	if strings.TrimSpace(req.Body) == "" {
		return fmt.Errorf("body is required")
	}
	if strings.TrimSpace(req.TightDescription) == "" {
		return fmt.Errorf("tight_description is required")
	}
	if req.Scope != "" && !validScopes[req.Scope] {
		return fmt.Errorf("invalid scope %q: must be one of global, project, session", req.Scope)
	}
	if req.PrivacyLevel != "" && !validPrivacyLevels[req.PrivacyLevel] {
		return fmt.Errorf("invalid privacy_level %q: must be one of private, shared, public_safe", req.PrivacyLevel)
	}
	return nil
}

// ValidateRecall validates a RecallRequest before a recall query is run.
func ValidateRecall(req *RecallRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if req.Mode != "" && !validRecallModes[req.Mode] {
		return fmt.Errorf("invalid mode %q", req.Mode)
	}
	if req.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	return nil
}

// ValidateDescribe validates a DescribeRequest before thin item descriptions
// are fetched.
func ValidateDescribe(req *DescribeRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if len(req.IDs) == 0 {
		return fmt.Errorf("ids list is empty")
	}
	for i, id := range req.IDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("ids[%d] is empty", i)
		}
	}
	return nil
}

// ValidateExpand validates an ExpandRequest before an item is expanded.
func ValidateExpand(req *ExpandRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(req.Kind) == "" {
		return fmt.Errorf("kind is required")
	}
	validKinds := map[string]bool{
		"memory":  true,
		"summary": true,
		"episode": true,
	}
	if !validKinds[req.Kind] {
		return fmt.Errorf("invalid kind %q: must be one of memory, summary, episode", req.Kind)
	}
	return nil
}

// ValidateHistory validates a HistoryRequest before raw event history is
// queried.
func ValidateHistory(req *HistoryRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	// At least one of query or session_id should be provided for a meaningful search,
	// but we allow empty requests to return recent history.
	if req.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	return nil
}

// ValidateRunJob validates a RunJobRequest before a maintenance job is run.
func ValidateRunJob(req *RunJobRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.Kind) == "" {
		return fmt.Errorf("kind is required")
	}
	if !validJobKinds[req.Kind] {
		return fmt.Errorf("invalid job kind %q", req.Kind)
	}
	return nil
}

// ValidateRepair validates a RepairRequest before integrity checks or repairs
// are started.
func ValidateRepair(req *RepairRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	// check and fix are optional; no strict validation needed.
	return nil
}

// ValidateExplainRecall validates an ExplainRecallRequest before recall
// explanation is generated.
func ValidateExplainRecall(req *ExplainRecallRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if strings.TrimSpace(req.ItemID) == "" {
		return fmt.Errorf("item_id is required")
	}
	return nil
}

// ValidateGetMemory validates a GetMemoryRequest before a memory is fetched by
// ID.
func ValidateGetMemory(req *GetMemoryRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("id is required")
	}
	return nil
}

func ValidateShare(req ShareRequest) error {
	if strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if !validPrivacyLevels[req.Privacy] {
		return fmt.Errorf("invalid privacy %q: must be one of private, shared, public_safe", req.Privacy)
	}
	return nil
}

// ValidateUpdateMemory validates an UpdateMemoryRequest before mutable memory
// fields are updated.
func ValidateUpdateMemory(req *UpdateMemoryRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if req.Type != "" && !validMemoryTypes[req.Type] {
		return fmt.Errorf("invalid type %q: must be one of identity, preference, fact, decision, episode, todo, relationship, procedure, constraint, incident, artifact, summary, active_context, open_loop, assumption, contradiction", req.Type)
	}
	if req.Scope != "" && !validScopes[req.Scope] {
		return fmt.Errorf("invalid scope %q: must be one of global, project, session", req.Scope)
	}
	if req.Status != "" && !validMemoryStatuses[req.Status] {
		return fmt.Errorf("invalid status %q: must be one of active, superseded, archived, retracted", req.Status)
	}
	return nil
}

// ValidatePolicyAdd validates a PolicyAddRequest before a new ingestion policy
// is created.
func ValidatePolicyAdd(req *PolicyAddRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.PatternType) == "" {
		return fmt.Errorf("pattern_type is required")
	}
	if !validPolicyPatternTypes[req.PatternType] {
		return fmt.Errorf("invalid pattern_type %q: must be one of session, source, surface, agent, project, runtime", req.PatternType)
	}
	if strings.TrimSpace(req.Pattern) == "" {
		return fmt.Errorf("pattern is required")
	}
	if strings.TrimSpace(req.Mode) == "" {
		return fmt.Errorf("mode is required")
	}
	if !validPolicyModes[req.Mode] {
		return fmt.Errorf("invalid mode %q: must be one of full, read_only, ignore", req.Mode)
	}
	if req.MatchMode != "" && !validPolicyMatchModes[req.MatchMode] {
		return fmt.Errorf("invalid match_mode %q: must be one of exact, glob, regex", req.MatchMode)
	}
	return nil
}
