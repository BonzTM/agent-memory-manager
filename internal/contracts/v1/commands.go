package v1

// Command name constants for all v1 amm commands.
const (
	// CmdIngestEvent appends a single raw event to history.
	CmdIngestEvent = "ingest_event"
	// CmdIngestTranscript appends a batch of raw events to history.
	CmdIngestTranscript = "ingest_transcript"
	// CmdRemember commits a durable memory record.
	CmdRemember = "remember"
	// CmdRecall retrieves memories for a query.
	CmdRecall = "recall"
	// CmdDescribe returns thin descriptions for one or more items.
	CmdDescribe = "describe"
	// CmdExpand expands a single item to full detail.
	CmdExpand = "expand"
	// CmdHistory queries raw interaction history.
	CmdHistory = "history"
	// CmdGetMemory retrieves a single memory by ID.
	CmdGetMemory = "get_memory"
	// CmdUpdateMemory updates an existing memory.
	CmdUpdateMemory = "update_memory"
	// CmdPolicyList lists ingestion policies.
	CmdPolicyList = "policy_list"
	// CmdPolicyAdd adds an ingestion policy.
	CmdPolicyAdd = "policy_add"
	// CmdPolicyRemove removes an ingestion policy by ID.
	CmdPolicyRemove = "policy_remove"
	// CmdRunJob executes a maintenance job.
	CmdRunJob = "run_job"
	// CmdRepair runs integrity checks and optional fixes.
	CmdRepair = "repair"
	// CmdExplainRecall explains why an item surfaced for a query.
	CmdExplainRecall = "explain_recall"
	// CmdStatus returns runtime and storage status information.
	CmdStatus = "status"
	// CmdRun executes a full v1 command envelope.
	CmdRun = "run"
	// CmdValidate validates a v1 command envelope without executing it.
	CmdValidate = "validate"
)

// CommandInfo describes a single amm command.
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
	CmdPolicyList: {
		Name:        CmdPolicyList,
		Description: "List ingestion policies.",
	},
	CmdPolicyAdd: {
		Name:        CmdPolicyAdd,
		Description: "Add an ingestion policy.",
	},
	CmdPolicyRemove: {
		Name:        CmdPolicyRemove,
		Description: "Remove an ingestion policy by ID.",
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
	CmdRun: {
		Name:        CmdRun,
		Description: "Execute a full v1 command envelope from a file or stdin.",
	},
	CmdValidate: {
		Name:        CmdValidate,
		Description: "Validate a v1 command envelope without executing.",
	},
}
