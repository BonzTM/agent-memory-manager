package v1

// Command name constants for all v0 AMM commands.
const (
	CmdIngestEvent      = "ingest_event"
	CmdIngestTranscript = "ingest_transcript"
	CmdRemember         = "remember"
	CmdRecall           = "recall"
	CmdDescribe         = "describe"
	CmdExpand           = "expand"
	CmdHistory          = "history"
	CmdGetMemory        = "get_memory"
	CmdUpdateMemory     = "update_memory"
	CmdRunJob           = "run_job"
	CmdRepair           = "repair"
	CmdExplainRecall    = "explain_recall"
	CmdStatus           = "status"
)

// CommandInfo describes a single AMM command.
type CommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CommandRegistry maps command names to their descriptions.
var CommandRegistry = map[string]CommandInfo{
	CmdIngestEvent: {
		Name:        CmdIngestEvent,
		Description: "Append a raw event to history.",
	},
	CmdIngestTranscript: {
		Name:        CmdIngestTranscript,
		Description: "Bulk-ingest a sequence of events from a transcript.",
	},
	CmdRemember: {
		Name:        CmdRemember,
		Description: "Commit an explicit durable memory.",
	},
	CmdRecall: {
		Name:        CmdRecall,
		Description: "Retrieve memories using the specified recall mode.",
	},
	CmdDescribe: {
		Name:        CmdDescribe,
		Description: "Return thin descriptions for one or more items by ID.",
	},
	CmdExpand: {
		Name:        CmdExpand,
		Description: "Return the full expansion of a single item.",
	},
	CmdHistory: {
		Name:        CmdHistory,
		Description: "Retrieve raw history by query or session.",
	},
	CmdGetMemory: {
		Name:        CmdGetMemory,
		Description: "Retrieve a single memory by ID.",
	},
	CmdUpdateMemory: {
		Name:        CmdUpdateMemory,
		Description: "Update an existing memory.",
	},
	CmdRunJob: {
		Name:        CmdRunJob,
		Description: "Execute a maintenance job by kind.",
	},
	CmdRepair: {
		Name:        CmdRepair,
		Description: "Run integrity checks and optionally fix issues.",
	},
	CmdExplainRecall: {
		Name:        CmdExplainRecall,
		Description: "Explain why an item surfaced for a given query.",
	},
	CmdStatus: {
		Name:        CmdStatus,
		Description: "Return system status information.",
	},
}
