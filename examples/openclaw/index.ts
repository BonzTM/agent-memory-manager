/**
 * AMM native OpenClaw plugin.
 *
 * Registers hooks for:
 * - Ambient recall injection via before_prompt_build
 * - Event capture via message and tool hooks
 *
 * This plugin is intentionally hot-path only. It does not run maintenance
 * jobs. Keep maintenance on an external schedule via host cron or systemd
 * running `amm jobs run <kind>` or `examples/scripts/run-workers.sh`.
 */

import { resolveConfig, type AmmConfig } from "./src/config.ts";
import { captureEvent, type HookEvent } from "./src/capture.ts";
import { ambientRecall } from "./src/recall.ts";
import { memorySearch, memoryGet } from "./src/transport.ts";

// ---------------------------------------------------------------------------
// OpenClaw plugin SDK types (minimal surface used by this plugin)
// ---------------------------------------------------------------------------

interface ToolSchema {
  type: "object";
  properties: Record<string, { type: string; description: string }>;
  required?: string[];
}

interface OpenClawPluginApi {
  pluginConfig: Record<string, unknown>;
  on(
    event: string,
    handler: (event: unknown, ctx: unknown) => Promise<Record<string, unknown> | void>,
    opts?: { name?: string; description?: string },
  ): void;
  registerHook(
    event: string,
    handler: (event: unknown) => Promise<void>,
    opts?: { name?: string; description?: string },
  ): void;
  registerTool(
    name: string,
    handler: (args: Record<string, unknown>) => Promise<unknown>,
    opts: { description: string; inputSchema: ToolSchema },
  ): void;
}

interface PluginEntry {
  id: string;
  name: string;
  kind?: string;
  register(api: OpenClawPluginApi): void;
}

declare function definePluginEntry(entry: PluginEntry): PluginEntry;

// ---------------------------------------------------------------------------
// Hook context extraction helpers
// ---------------------------------------------------------------------------

interface PromptBuildEvent {
  messages?: Array<{ role?: string; content?: string }>;
  sessionKey?: string;
}

function extractUserQuery(event: PromptBuildEvent): string {
  const messages = event.messages;
  if (!Array.isArray(messages) || messages.length === 0) return "";
  // Walk backwards to find the most recent user message.
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i];
    if (msg?.role === "user" && typeof msg.content === "string" && msg.content.trim()) {
      return msg.content.trim();
    }
  }
  return "";
}

// ---------------------------------------------------------------------------
// Plugin definition
// ---------------------------------------------------------------------------

export default definePluginEntry({
  id: "amm-memory",
  name: "AMM Memory",
  kind: "memory",

  register(api: OpenClawPluginApi) {
    const config: AmmConfig = resolveConfig(api.pluginConfig);

    // --- Memory slot tools (memory_search / memory_get) --------------------
    api.registerTool(
      "memory_search",
      async (args) => {
        const query = String(args["query"] ?? "");
        if (!query.trim()) return { items: [] };
        const limit = typeof args["limit"] === "number" ? args["limit"] : config.recallLimit;
        const type = typeof args["type"] === "string" ? args["type"] : undefined;
        const projectId = typeof args["project_id"] === "string" ? args["project_id"] : undefined;
        return memorySearch(config, query, { limit, type, projectId });
      },
      {
        description: "Search durable memories by natural language query. Returns scored results with type, subject, body, and tight_description.",
        inputSchema: {
          type: "object",
          properties: {
            query: { type: "string", description: "Natural language search query" },
            limit: { type: "string", description: "Maximum results to return (default: 5)" },
            type: { type: "string", description: "Filter by memory type (preference, fact, decision, etc.)" },
            project_id: { type: "string", description: "Filter by project scope" },
          },
          required: ["query"],
        },
      },
    );

    api.registerTool(
      "memory_get",
      async (args) => {
        const id = String(args["id"] ?? "");
        if (!id.trim()) return { error: "id is required" };
        return memoryGet(config, id);
      },
      {
        description: "Retrieve a single memory by its ID. Returns the full memory record including body, metadata, and source event IDs.",
        inputSchema: {
          type: "object",
          properties: {
            id: { type: "string", description: "Memory ID (e.g. mem_abc123)" },
          },
          required: ["id"],
        },
      },
    );

    // --- Ambient recall injection (before_prompt_build) -------------------
    api.on(
      "before_prompt_build",
      async (event) => {
        const promptEvent = event as PromptBuildEvent;
        const query = extractUserQuery(promptEvent);
        if (!query) return {};

        // Do not ingest the user message here — message:preprocessed
        // already captures it via captureEvent(). Duplicating would bloat
        // history and produce redundant reflections/memories.

        const context = await ambientRecall(config, query, promptEvent.sessionKey);
        if (!context) return {};
        return { prependContext: `<amm-context>\n${context}\n</amm-context>` };
      },
      { name: "amm-memory.recall", description: "Inject AMM ambient recall before LLM prompt" },
    );

    // --- Event capture hooks ----------------------------------------------
    const captureHandler = async (event: unknown) => {
      await captureEvent(config, event as HookEvent);
    };

    for (const hookEvent of [
      "message:preprocessed",
      "message:sent",
      "tool:called",
      "tool:completed",
    ]) {
      api.registerHook(hookEvent, captureHandler, {
        name: `amm-memory.capture.${hookEvent}`,
        description: `Capture ${hookEvent} events into AMM history`,
      });
    }
  },
});
