import { spawn } from "node:child_process";

function nowRfc3339(value = Date.now()) {
  const date = new Date(value > 1_000_000_000_000 ? value : value * 1000);
  return Number.isNaN(date.getTime()) ? new Date().toISOString() : date.toISOString();
}

function stringifyValue(value) {
  if (value === undefined || value === null) return undefined;
  if (typeof value === "string") return value;
  return JSON.stringify(value);
}

function stringifyRecord(record) {
  return Object.fromEntries(
    Object.entries(record)
      .map(([key, value]) => [key, stringifyValue(value)])
      .filter(([, value]) => value !== undefined),
  );
}

function normalizeText(value) {
  if (typeof value !== "string") return "";
  return value.trim();
}

function extractTextParts(parts) {
  if (!Array.isArray(parts)) return "";
  const text = parts
    .map((part) => {
      if (typeof part === "string") return part;
      if (!part || typeof part !== "object") return "";
      if (typeof part.text === "string") return part.text;
      if (typeof part.content === "string") return part.content;
      if (Array.isArray(part.content)) return extractTextParts(part.content);
      if (Array.isArray(part.parts)) return extractTextParts(part.parts);
      return "";
    })
    .filter(Boolean)
    .join("\n");
  return normalizeText(text);
}

function extractMessageText(message) {
  if (!message || typeof message !== "object") return "";

  if (typeof message.content === "string") {
    return normalizeText(message.content);
  }

  if (Array.isArray(message.content)) {
    return extractTextParts(message.content);
  }

  if (typeof message.text === "string") {
    return normalizeText(message.text);
  }

  if (Array.isArray(message.parts)) {
    return extractTextParts(message.parts);
  }

  return "";
}

function extractMessageRole(message) {
  if (!message || typeof message !== "object") return undefined;
  if (typeof message.role === "string") return message.role;
  if (typeof message.type === "string") return message.type;
  if (message.author && typeof message.author.role === "string") {
    return message.author.role;
  }
  return undefined;
}

function extractMessageID(message) {
  if (!message || typeof message !== "object") return undefined;
  if (typeof message.id === "string") return message.id;
  if (typeof message.messageID === "string") return message.messageID;
  if (typeof message.messageId === "string") return message.messageId;
  return undefined;
}

function extractMessageTimestamp(message) {
  if (!message || typeof message !== "object") return undefined;
  if (message.time && typeof message.time.created !== "undefined") {
    return message.time.created;
  }
  if (typeof message.createdAt !== "undefined") return message.createdAt;
  if (typeof message.created_at !== "undefined") return message.created_at;
  if (typeof message.timestamp !== "undefined") return message.timestamp;
  return undefined;
}

function isMessageFinal(message, eventType) {
  if (eventType === "message.created") {
    const status = typeof message?.status === "string" ? message.status.toLowerCase() : "";
    if (status && ["streaming", "in_progress", "pending", "partial"].includes(status)) {
      return false;
    }
    return true;
  }

  if (eventType !== "message.updated") {
    return false;
  }

  if (message?.final === true || message?.done === true || message?.complete === true) {
    return true;
  }

  const status = typeof message?.status === "string" ? message.status.toLowerCase() : "";
  return ["completed", "complete", "done", "final"].includes(status);
}

function extractMessagePayload(event) {
  const properties = event?.properties;
  if (!properties || typeof properties !== "object") return undefined;
  if (properties.message && typeof properties.message === "object") return properties.message;
  if (properties.data && typeof properties.data.message === "object") return properties.data.message;
  if (properties.payload && typeof properties.payload.message === "object") return properties.payload.message;

  if (
    (event?.type === "message.created" || event?.type === "message.updated") &&
    (typeof properties.role === "string" || typeof properties.content !== "undefined")
  ) {
    return properties;
  }

  return undefined;
}

const AMM_INGEST_TIMEOUT_MS = 5_000;
const AMM_JOB_TIMEOUT_MS = 120_000;

function runAmmAsync(command, input, timeoutMs = AMM_INGEST_TIMEOUT_MS) {
  const ammBin = process.env.AMM_BIN ?? "/usr/local/bin/amm";
  const dbPath = process.env.AMM_DB_PATH ?? `${process.env.HOME ?? "~"}/.amm/amm.db`;

  const child = spawn(ammBin, command, {
    stdio: ["pipe", "ignore", "ignore"],
    env: { ...process.env, AMM_DB_PATH: dbPath },
    detached: false,
  });

  if (input) {
    child.stdin.write(input);
    child.stdin.end();
  } else {
    child.stdin.end();
  }

  const timer = setTimeout(() => {
    try { child.kill("SIGTERM"); } catch {}
    setTimeout(() => {
      try { child.kill("SIGKILL"); } catch {}
    }, 2_000);
  }, timeoutMs);

  child.on("exit", () => clearTimeout(timer));
  child.on("error", () => clearTimeout(timer));

  child.unref();
}

function ingestEvent(event) {
  runAmmAsync(["ingest", "event", "--in", "-"], JSON.stringify(event), AMM_INGEST_TIMEOUT_MS);
}

let maintenanceRunning = false;
const maintenanceBySession = new Map();
const emittedMessageVersions = new Map();

function runMaintenanceAsync() {
  if (maintenanceRunning) return;
  maintenanceRunning = true;

  const ammBin = process.env.AMM_BIN ?? "/usr/local/bin/amm";
  const dbPath = process.env.AMM_DB_PATH ?? `${process.env.HOME ?? "~"}/.amm/amm.db`;
  const lockDir = `${dbPath}.opencode-maintenance.lock`;

  const maintenanceScript = `
lock_dir="$1"
amm_bin="$2"

if mkdir "$lock_dir" 2>/dev/null; then
  printf '%s\n' "$$" > "$lock_dir/pid"
else
  if [ -f "$lock_dir/pid" ]; then
    pid=$(cat "$lock_dir/pid" 2>/dev/null || true)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      exit 0
    fi
  fi
  rm -rf "$lock_dir" 2>/dev/null || exit 0
  mkdir "$lock_dir" 2>/dev/null || exit 0
  printf '%s\n' "$$" > "$lock_dir/pid"
fi

cleanup() {
  trap - EXIT INT TERM
  kill 0 2>/dev/null || true
  rm -rf "$lock_dir" 2>/dev/null || true
}

trap cleanup EXIT INT TERM

"$amm_bin" jobs run reflect >/dev/null 2>&1 || true
"$amm_bin" jobs run compress_history >/dev/null 2>&1 || true
`;

  const child = spawn(
    "/bin/sh",
    ["-c", maintenanceScript, "sh", lockDir, ammBin],
    {
      stdio: "ignore",
      env: { ...process.env, AMM_DB_PATH: dbPath },
      detached: true,
    },
  );

  const timer = setTimeout(() => {
    try { process.kill(-child.pid, "SIGTERM"); } catch {}
    setTimeout(() => {
      try { process.kill(-child.pid, "SIGKILL"); } catch {}
    }, 2_000);
    maintenanceRunning = false;
  }, AMM_JOB_TIMEOUT_MS);

  child.on("exit", () => {
    clearTimeout(timer);
    maintenanceRunning = false;
  });
  child.on("error", () => {
    clearTimeout(timer);
    maintenanceRunning = false;
  });

  child.unref();
}

export const AMMMemoryPlugin = async ({ project }) => {
  const projectID = project?.id ?? "opencode-project";

  return {
    "shell.env": async (input, output) => {
      output.env.AMM_BIN ??= "/usr/local/bin/amm";
      output.env.AMM_DB_PATH ??= `${process.env.HOME ?? "~"}/.amm/amm.db`;
      output.env.AMM_PROJECT_ID ??= projectID;
      if (input.sessionID) {
        output.env.AMM_SESSION_ID ??= input.sessionID;
      }
    },

    "tool.execute.before": async (input) => {
      ingestEvent({
        kind: "tool_call",
        source_system: "opencode",
        session_id: input.sessionID,
        project_id: projectID,
        actor_type: "tool",
        content: stringifyValue(input.args) ?? "",
        metadata: stringifyRecord({
          hook_event: "tool.execute.before",
          tool_name: input.tool,
          call_id: input.callID,
          tool_input: input.args,
        }),
        occurred_at: nowRfc3339(),
      });
    },

    "tool.execute.after": async (input, output) => {
      ingestEvent({
        kind: "tool_result",
        source_system: "opencode",
        session_id: input.sessionID,
        project_id: projectID,
        actor_type: "tool",
        content: output.output,
        metadata: stringifyRecord({
          hook_event: "tool.execute.after",
          tool_name: input.tool,
          call_id: input.callID,
          tool_input: input.args,
          title: output.title,
          tool_metadata: output.metadata,
        }),
        occurred_at: nowRfc3339(),
      });
    },

    event: async ({ event }) => {
      if (event.type === "message.created" || event.type === "message.updated") {
        const message = extractMessagePayload(event);
        const role = extractMessageRole(message);
        if (role === "user" || role === "assistant") {
          const content = extractMessageText(message);
          if (content && isMessageFinal(message, event.type)) {
            const messageID = extractMessageID(message) ?? "unknown";
            const versionKey = `${event.properties?.sessionID ?? "unknown"}:${messageID}:${role}`;
            const fingerprint = `${content.length}:${content}`;

            if (emittedMessageVersions.get(versionKey) !== fingerprint) {
              emittedMessageVersions.set(versionKey, fingerprint);
              ingestEvent({
                kind: role === "user" ? "message_user" : "message_assistant",
                source_system: "opencode",
                session_id: event.properties?.sessionID,
                project_id: projectID,
                actor_type: role,
                content,
                metadata: stringifyRecord({
                  hook_event: event.type,
                  message_id: extractMessageID(message),
                  status: message?.status,
                }),
                occurred_at: nowRfc3339(extractMessageTimestamp(message) ?? Date.now()),
              });
            }
          }
        }
      }

      if (event.type === "session.created") {
        ingestEvent({
          kind: "session_start",
          source_system: "opencode",
          session_id: event.properties.info.id,
          project_id: projectID,
          content: `OpenCode session created in ${event.properties.info.directory}`,
          metadata: stringifyRecord({
            hook_event: event.type,
            directory: event.properties.info.directory,
            project_ref: event.properties.info.projectID,
          }),
          occurred_at: nowRfc3339(event.properties.info.time.created),
        });
      }

      if (event.type === "session.idle") {
        const lastRun = maintenanceBySession.get(event.properties.sessionID) ?? 0;
        const now = Date.now();
        if (now - lastRun < 60_000) {
          return;
        }
        maintenanceBySession.set(event.properties.sessionID, now);

        ingestEvent({
          kind: "session_idle",
          source_system: "opencode",
          session_id: event.properties.sessionID,
          project_id: projectID,
          content: "OpenCode session became idle.",
          metadata: stringifyRecord({ hook_event: event.type }),
          occurred_at: nowRfc3339(now),
        });

        runMaintenanceAsync();
      }
    },
  };
};
