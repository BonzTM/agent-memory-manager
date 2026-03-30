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
	// CmdFormatContextWindow assembles context from summaries plus fresh events.
	CmdFormatContextWindow = "format_context_window"
	// CmdHistory queries raw interaction history.
	CmdHistory = "history"
	CmdGrep    = "grep"
	// CmdGetMemory retrieves a single memory by ID.
	CmdGetMemory = "get_memory"
	// CmdUpdateMemory updates an existing memory.
	CmdUpdateMemory = "update_memory"
	CmdShare        = "share"
	CmdForget       = "forget"
	// CmdPolicyList lists ingestion policies.
	CmdPolicyList = "policy_list"
	// CmdPolicyAdd adds an ingestion policy.
	CmdPolicyAdd = "policy_add"
	// CmdPolicyRemove removes an ingestion policy by ID.
	CmdPolicyRemove = "policy_remove"
	// CmdRegisterProject stores a new project.
	CmdRegisterProject = "register_project"
	// CmdGetProject retrieves a project by ID.
	CmdGetProject = "get_project"
	// CmdListProjects lists registered projects.
	CmdListProjects = "list_projects"
	// CmdRemoveProject removes a project by ID.
	CmdRemoveProject = "remove_project"
	// CmdAddRelationship stores a relationship.
	CmdAddRelationship = "add_relationship"
	// CmdGetRelationship retrieves a relationship by ID.
	CmdGetRelationship = "get_relationship"
	// CmdListRelationships lists relationships.
	CmdListRelationships = "list_relationships"
	// CmdRemoveRelationship removes a relationship by ID.
	CmdRemoveRelationship = "remove_relationship"
	// CmdGetSummary retrieves a summary by ID.
	CmdGetSummary = "get_summary"
	// CmdGetEpisode retrieves an episode by ID.
	CmdGetEpisode = "get_episode"
	// CmdGetEntity retrieves an entity by ID.
	CmdGetEntity = "get_entity"
	// CmdRunJob executes a maintenance job.
	CmdRunJob = "run_job"
	// CmdRepair runs integrity checks and optional fixes.
	CmdRepair = "repair"
	// CmdExplainRecall explains why an item surfaced for a query.
	CmdExplainRecall = "explain_recall"
	// CmdStatus returns runtime and storage status information.
	CmdStatus       = "status"
	CmdResetDerived = "reset_derived"
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
	CmdFormatContextWindow: {
		Name:        CmdFormatContextWindow,
		Description: "Assemble a deterministic context window from summaries and fresh events.",
	},
	CmdHistory: {
		Name:        CmdHistory,
		Description: "Retrieve raw history by query or session.",
	},
	CmdGrep: {
		Name:        CmdGrep,
		Description: "Search events and group matches by covering summary.",
	},
	CmdGetMemory: {
		Name:        CmdGetMemory,
		Description: "Retrieve a single memory by ID.",
	},
	CmdUpdateMemory: {
		Name:        CmdUpdateMemory,
		Description: "Update an existing memory.",
	},
	CmdShare: {
		Name:        CmdShare,
		Description: "Update the privacy level of an existing memory.",
	},
	CmdForget: {
		Name:        CmdForget,
		Description: "Forget (retract) a memory by ID.",
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
	CmdRegisterProject: {
		Name:        CmdRegisterProject,
		Description: "Register a project.",
	},
	CmdGetProject: {
		Name:        CmdGetProject,
		Description: "Retrieve a project by ID.",
	},
	CmdListProjects: {
		Name:        CmdListProjects,
		Description: "List all registered projects.",
	},
	CmdRemoveProject: {
		Name:        CmdRemoveProject,
		Description: "Remove a project by ID.",
	},
	CmdAddRelationship: {
		Name:        CmdAddRelationship,
		Description: "Add a relationship between entities.",
	},
	CmdGetRelationship: {
		Name:        CmdGetRelationship,
		Description: "Retrieve a relationship by ID.",
	},
	CmdListRelationships: {
		Name:        CmdListRelationships,
		Description: "List relationships.",
	},
	CmdRemoveRelationship: {
		Name:        CmdRemoveRelationship,
		Description: "Remove a relationship by ID.",
	},
	CmdGetSummary: {
		Name:        CmdGetSummary,
		Description: "Retrieve a summary by ID.",
	},
	CmdGetEpisode: {
		Name:        CmdGetEpisode,
		Description: "Retrieve an episode by ID.",
	},
	CmdGetEntity: {
		Name:        CmdGetEntity,
		Description: "Retrieve an entity by ID.",
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
	CmdResetDerived: {
		Name:        CmdResetDerived,
		Description: "Purge derived data while preserving events.",
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
