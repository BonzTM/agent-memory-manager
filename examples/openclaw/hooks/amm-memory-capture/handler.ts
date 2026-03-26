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

function asNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function toIsoTimestamp(value: unknown): string {
  if (value instanceof Date && !Number.isNaN(value.getTime())) {
    return value.toISOString();
  }

  const numeric = asNumber(value);
  if (numeric !== undefined) {
    const milliseconds = numeric > 1_000_000_000_000 ? numeric : numeric * 1000;
    const parsed = new Date(milliseconds);
    if (!Number.isNaN(parsed.getTime())) {
      return parsed.toISOString();
    }
  }

  const text = asString(value);
  if (text !== undefined) {
    const parsed = new Date(text);
    if (!Number.isNaN(parsed.getTime())) {
      return parsed.toISOString();
    }
  }

  return new Date().toISOString();
}

function withoutUndefined(record: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(record).filter(([, value]) => value !== undefined));
}

function toText(value: unknown): string | undefined {
  const text = asString(value);
  if (text !== undefined) {
    return text;
  }

  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return String(value);
  }

  if (value === null) {
    return "null";
  }

  if (value === undefined) {
    return undefined;
  }

  try {
    return JSON.stringify(value);
  } catch {
    return undefined;
  }
}

function runAmmIngest(eventPayload: Record<string, unknown>): void {
  const ammBin = process.env.AMM_BIN ?? "amm";
  const dbPath = process.env.AMM_DB_PATH ?? `${process.env.HOME ?? "~"}/.amm/amm.db`;
  const result = spawnSync(ammBin, ["ingest", "event", "--in", "-"], {
    input: JSON.stringify(eventPayload),
    encoding: "utf8",
    env: { ...process.env, AMM_DB_PATH: dbPath },
  });

  if (result.error || result.status !== 0) {
    const detail = result.error?.message ?? result.stderr ?? `exit ${result.status ?? "unknown"}`;
    console.error(`[amm-memory-capture] ${detail}`);
  }
}

function pickInboundContent(context: HookContext): string | undefined {
  return (
    asString(context["bodyForAgent"]) ??
    asString(context["transcript"]) ??
    asString(context["body"]) ??
    asString(context["content"])
  );
}

function pickOutboundContent(context: HookContext): string | undefined {
  return asString(context["content"]);
}

function pickToolName(context: HookContext): string | undefined {
  return asString(context["name"]) ?? asString(context["toolName"]) ?? asString(context["functionName"]);
}

function pickToolInput(context: HookContext): unknown {
  return context["arguments"] ?? context["args"] ?? context["input"];
}

function pickToolOutput(context: HookContext): unknown {
  return context["output"] ?? context["result"] ?? context["content"];
}

function buildToolCallContent(context: HookContext): string | undefined {
  const toolName = pickToolName(context);
  const toolInput = toText(pickToolInput(context));

  if (toolName === undefined && toolInput === undefined) {
    return undefined;
  }

  return [
    toolName !== undefined ? `name: ${toolName}` : undefined,
    toolInput !== undefined ? `arguments: ${toolInput}` : undefined,
  ]
    .filter((part): part is string => part !== undefined)
    .join("\n");
}

function buildToolResultContent(context: HookContext): string | undefined {
  return toText(pickToolOutput(context));
}

const handler = async (event: HookEvent) => {
  const context = event.context ?? {};
  const isInbound = event.type === "message" && event.action === "preprocessed";
  const isOutbound = event.type === "message" && event.action === "sent";
  const isToolCall = (event.type === "tool" || event.type === "function") && event.action === "called";
  const isToolResult = (event.type === "tool" || event.type === "function") && event.action === "completed";

  if (!isInbound && !isOutbound && !isToolCall && !isToolResult) {
    return;
  }

  const content = isInbound
    ? pickInboundContent(context)
    : isOutbound
      ? pickOutboundContent(context)
      : isToolCall
        ? buildToolCallContent(context)
        : buildToolResultContent(context);

  if (content === undefined) {
    return;
  }

  const toolName = pickToolName(context);

  const metadata = withoutUndefined({
    hook_event: `${event.type}:${event.action}`,
    channel_id: asString(context["channelId"]),
    conversation_id: asString(context["conversationId"]),
    message_id: asString(context["messageId"]),
    from: asString(context["from"]),
    to: asString(context["to"]),
    sender_id: asString(context["senderId"]),
    sender_name: asString(context["senderName"]),
    sender_username: asString(context["senderUsername"]),
    provider: asString(context["provider"]),
    surface: asString(context["surface"]),
    group_id: asString(context["groupId"]),
    is_group: context["isGroup"] === true,
    tool_name: toolName,
  });

  runAmmIngest({
    kind: isInbound ? "message_user" : isOutbound ? "message_assistant" : isToolCall ? "tool_call" : "tool_result",
    source_system: "openclaw",
    session_id: event.sessionKey ?? asString(context["conversationId"]),
    project_id: process.env.AMM_PROJECT_ID,
    actor_type: isInbound ? "user" : isOutbound ? "assistant" : "tool",
    content,
    metadata,
    occurred_at: toIsoTimestamp(event.timestamp ?? context["timestamp"]),
  });
};

export default handler;
