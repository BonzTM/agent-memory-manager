import { spawnSync } from "node:child_process";

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

function runAmm(command, input) {
  const ammBin = process.env.AMM_BIN ?? "/usr/local/bin/amm";
  const dbPath = process.env.AMM_DB_PATH ?? `${process.env.HOME ?? "~"}/.amm/amm.db`;
  const result = spawnSync(ammBin, command, {
    input,
    encoding: "utf8",
    env: { ...process.env, AMM_DB_PATH: dbPath },
  });

  if (result.error || result.status !== 0) {
    const detail = result.error?.message ?? result.stderr ?? `exit ${result.status ?? "unknown"}`;
    console.error(`[amm-opencode] ${command.join(" ")}: ${detail}`);
  }
}

function ingestEvent(event) {
  runAmm(["ingest", "event", "--in", "-"], JSON.stringify(event));
}

const maintenanceBySession = new Map();

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
            worktree: event.properties.info.projectID,
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

        runAmm(["jobs", "run", "reflect"]);
        runAmm(["jobs", "run", "compress_history"]);
      }
    },
  };
};
