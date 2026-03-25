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

trap 'rm -rf "$lock_dir"' EXIT INT TERM

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
    try { child.kill("SIGTERM"); } catch {}
    setTimeout(() => {
      try { child.kill("SIGKILL"); } catch {}
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
