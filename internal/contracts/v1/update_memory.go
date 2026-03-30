package v1

import "github.com/bonztm/agent-memory-manager/internal/core"

func ApplyMemoryUpdate(mem *core.Memory, req UpdateMemoryRequest) {
	if mem == nil {
		return
	}

	if req.Body != "" {
		mem.Body = req.Body
	}
	if req.TightDescription != "" {
		mem.TightDescription = req.TightDescription
	}
	if req.Subject != "" {
		mem.Subject = req.Subject
	}
	if req.Type != "" {
		mem.Type = core.MemoryType(req.Type)
	}
	if req.Scope != "" {
		mem.Scope = core.Scope(req.Scope)
	}
	if req.Status != "" {
		mem.Status = core.MemoryStatus(req.Status)
	}
	if req.Metadata != nil {
		if mem.Metadata == nil {
			mem.Metadata = make(map[string]string)
		}
		for k, v := range req.Metadata {
			mem.Metadata[k] = v
		}
	}
}
