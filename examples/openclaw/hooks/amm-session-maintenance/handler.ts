import { spawnSync } from "node:child_process";

type HookContext = Record<string, unknown>;

type HookEvent = {
  type: string;
  action: string;
  sessionKey?: string;
  timestamp?: Date | string | number;
  context?: HookContext;
};

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function toIsoTimestamp(value: unknown): string {
  if (value instanceof Date && !Number.isNaN(value.getTime())) {
    return value.toISOString();
  }
  if (typeof value === "string" || typeof value === "number") {
    const parsed = new Date(value);
    if (!Number.isNaN(parsed.getTime())) {
      return parsed.toISOString();
    }
  }
  return new Date().toISOString();
}

function withoutUndefined(record: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(record).filter(([, value]) => value !== undefined));
}

function runAmm(command: string[], input?: string): void {
  const ammBin = process.env.AMM_BIN ?? "amm";
  const dbPath = process.env.AMM_DB_PATH ?? `${process.env.HOME ?? "~"}/.amm/amm.db`;
  const result = spawnSync(ammBin, command, {
    input,
    encoding: "utf8",
    env: { ...process.env, AMM_DB_PATH: dbPath },
  });

  if (result.error || result.status !== 0) {
    const detail = result.error?.message ?? result.stderr ?? `exit ${result.status ?? "unknown"}`;
    console.error(`[amm-session-maintenance] ${command.join(" ")}: ${detail}`);
  }
}

const handler = async (event: HookEvent) => {
  if (event.type !== "command" || event.action !== "stop") {
    return;
  }

  const context = event.context ?? {};
  const metadata = withoutUndefined({
    hook_event: `${event.type}:${event.action}`,
    session_id: asString(context["sessionId"]),
    sender_id: asString(context["senderId"]),
    command_source: asString(context["commandSource"]),
    workspace_dir: asString(context["workspaceDir"]),
  });

  runAmm(
    ["ingest", "event", "--in", "-"],
    JSON.stringify({
      kind: "session_stop",
      source_system: "openclaw",
      session_id: asString(context["sessionId"]) ?? event.sessionKey,
      project_id: process.env.AMM_PROJECT_ID,
      content: "OpenClaw command:stop triggered AMM session maintenance.",
      metadata,
      occurred_at: toIsoTimestamp(event.timestamp),
    }),
  );

  for (const job of ["reflect", "compress_history", "consolidate_sessions"]) {
    runAmm(["jobs", "run", job]);
  }
};

export default handler;
