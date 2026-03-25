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
		"rebuild_indexes":        true,
		"extract_claims":         true,
		"form_episodes":          true,
		"detect_contradictions":  true,
		"decay_stale_memory":     true,
		"merge_duplicates":       true,
		"cleanup_recall_history": true,
		"reprocess":              true,
		"reprocess_all":          true,
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
)

// ValidateIngestEvent validates an IngestEventRequest.
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

// ValidateIngestTranscript validates an IngestTranscriptRequest.
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

// ValidateRemember validates a RememberRequest.
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

// ValidateRecall validates a RecallRequest.
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

// ValidateDescribe validates a DescribeRequest.
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

// ValidateExpand validates an ExpandRequest.
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

// ValidateHistory validates a HistoryRequest.
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

// ValidateRunJob validates a RunJobRequest.
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

// ValidateRepair validates a RepairRequest.
func ValidateRepair(req *RepairRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	// check and fix are optional; no strict validation needed.
	return nil
}

// ValidateExplainRecall validates an ExplainRecallRequest.
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

// ValidateGetMemory validates a GetMemoryRequest.
func ValidateGetMemory(req *GetMemoryRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("id is required")
	}
	return nil
}

// ValidateUpdateMemory validates an UpdateMemoryRequest.
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
	return nil
}
