/**
 * Event capture for the AMM OpenClaw plugin.
 *
 * Normalizes OpenClaw hook events into amm event schema and ingests them.
 */

import type { AmmConfig } from "./config.ts";
import { ingestEvent } from "./transport-http.ts";

type HookContext = Record<string, unknown>;

export interface HookEvent {
  type: string;
  action: string;
  sessionKey?: string;
  timestamp?: Date | string | number;
  context?: HookContext;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function toText(value: unknown): string | undefined {
  if (typeof value === "string" && value.length > 0) return value;
  if (typeof value === "number" || typeof value === "boolean" || typeof value === "bigint") {
    return String(value);
  }
  if (value === null) return "null";
  if (value === undefined) return undefined;
  try {
    return JSON.stringify(value);
  } catch {
    return undefined;
  }
}

function toIsoTimestamp(value: unknown): string {
  if (value instanceof Date && !Number.isNaN(value.getTime())) {
    return value.toISOString();
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    const ms = value > 1_000_000_000_000 ? value : value * 1000;
    const d = new Date(ms);
    if (!Number.isNaN(d.getTime())) return d.toISOString();
  }
  if (typeof value === "string") {
    const d = new Date(value);
    if (!Number.isNaN(d.getTime())) return d.toISOString();
  }
  return new Date().toISOString();
}

function withoutUndefined(record: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(record).filter(([, v]) => v !== undefined));
}

// ---------------------------------------------------------------------------
// Content pickers (polymorphic across OpenClaw payload shapes)
// ---------------------------------------------------------------------------

function pickInboundContent(ctx: HookContext): string | undefined {
  return (
    asString(ctx["bodyForAgent"]) ??
    asString(ctx["transcript"]) ??
    asString(ctx["body"]) ??
    asString(ctx["content"])
  );
}

function pickOutboundContent(ctx: HookContext): string | undefined {
  return asString(ctx["content"]);
}

function pickToolName(ctx: HookContext): string | undefined {
  return asString(ctx["name"]) ?? asString(ctx["toolName"]) ?? asString(ctx["functionName"]);
}

function pickToolInput(ctx: HookContext): unknown {
  return ctx["arguments"] ?? ctx["args"] ?? ctx["input"];
}

function pickToolOutput(ctx: HookContext): unknown {
  return ctx["output"] ?? ctx["result"] ?? ctx["content"];
}

function buildToolCallContent(ctx: HookContext): string | undefined {
  const name = pickToolName(ctx);
  const input = toText(pickToolInput(ctx));
  if (name === undefined && input === undefined) return undefined;
  return [
    name !== undefined ? `name: ${name}` : undefined,
    input !== undefined ? `arguments: ${input}` : undefined,
  ]
    .filter((p): p is string => p !== undefined)
    .join("\n");
}

function buildToolResultContent(ctx: HookContext): string | undefined {
  return toText(pickToolOutput(ctx));
}

// ---------------------------------------------------------------------------
// Event classification
// ---------------------------------------------------------------------------

interface EventClassification {
  kind: "message_user" | "message_assistant" | "tool_call" | "tool_result";
  actorType: "user" | "assistant" | "tool";
  content: string | undefined;
}

function classify(event: HookEvent): EventClassification | undefined {
  const ctx = event.context ?? {};
  const { type, action } = event;

  if (type === "message" && action === "preprocessed") {
    return { kind: "message_user", actorType: "user", content: pickInboundContent(ctx) };
  }
  if (type === "message" && action === "sent") {
    return { kind: "message_assistant", actorType: "assistant", content: pickOutboundContent(ctx) };
  }
  if ((type === "tool" || type === "function") && action === "called") {
    return { kind: "tool_call", actorType: "tool", content: buildToolCallContent(ctx) };
  }
  if ((type === "tool" || type === "function") && action === "completed") {
    return { kind: "tool_result", actorType: "tool", content: buildToolResultContent(ctx) };
  }
  return undefined;
}

// ---------------------------------------------------------------------------
// Public capture handler
// ---------------------------------------------------------------------------

/**
 * Process an OpenClaw hook event and ingest it into amm.
 * Returns the user message content when the event is an inbound message,
 * so the caller can use it for ambient recall.
 */
export async function captureEvent(
  config: AmmConfig,
  event: HookEvent,
): Promise<{ inboundContent?: string }> {
  const classification = classify(event);
  if (!classification || !classification.content) return {};

  const ctx = event.context ?? {};
  const toolName = pickToolName(ctx);

  const metadata = withoutUndefined({
    hook_event: `${event.type}:${event.action}`,
    channel_id: asString(ctx["channelId"]),
    conversation_id: asString(ctx["conversationId"]),
    message_id: asString(ctx["messageId"]),
    from: asString(ctx["from"]),
    to: asString(ctx["to"]),
    sender_id: asString(ctx["senderId"]),
    sender_name: asString(ctx["senderName"]),
    sender_username: asString(ctx["senderUsername"]),
    provider: asString(ctx["provider"]),
    surface: asString(ctx["surface"]),
    group_id: asString(ctx["groupId"]),
    is_group: ctx["isGroup"] === true ? true : undefined,
    tool_name: toolName,
  });

  await ingestEvent(config, {
    kind: classification.kind,
    source_system: "openclaw",
    session_id: event.sessionKey ?? asString(ctx["conversationId"]),
    project_id: config.projectId,
    actor_type: classification.actorType,
    content: classification.content,
    metadata,
    occurred_at: toIsoTimestamp(event.timestamp ?? ctx["timestamp"]),
  });

  return {
    inboundContent: classification.kind === "message_user" ? classification.content : undefined,
  };
}
