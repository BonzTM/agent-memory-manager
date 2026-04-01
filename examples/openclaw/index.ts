/**
 * AMM native OpenClaw plugin.
 *
 * Claims the memory slot via registerMemoryRuntime, providing
 * memory_search and memory_get to the agent. Injects ambient recall
 * via registerMemoryPromptSection and captures conversation events
 * via registerHook.
 *
 * This plugin is intentionally hot-path only. It does not run maintenance
 * jobs. Keep maintenance on an external schedule via host cron or systemd
 * running `amm jobs run <kind>` or `examples/scripts/run-workers.sh`.
 */

import { resolveConfig, type AmmConfig } from "./src/config.ts";
import { captureEvent, type HookEvent } from "./src/capture.ts";
import { ambientRecall, renderRecall } from "./src/recall.ts";
import { memorySearch, memoryGet, recall } from "./src/transport-http.ts";

// ---------------------------------------------------------------------------
// OpenClaw plugin SDK types (minimal surface used by this plugin)
// ---------------------------------------------------------------------------

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
  registerMemoryRuntime(runtime: Record<string, unknown>): void;
  registerMemoryPromptSection(section: Record<string, unknown>): void;
}

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

export default {
  id: "amm",
  name: "AMM Memory",
  kind: "memory",

  register(api: OpenClawPluginApi) {
    const config: AmmConfig = resolveConfig(api.pluginConfig);

    // --- Memory slot runtime (memory_search / memory_get) ------------------
    api.registerMemoryRuntime({
      search: async (query: string, opts?: Record<string, unknown>) => {
        if (!query || !query.trim()) return [];
        const limit = typeof opts?.limit === "number" ? opts.limit : config.recallLimit;
        const type = typeof opts?.type === "string" ? opts.type : undefined;
        const projectId = typeof opts?.project_id === "string" ? opts.project_id : undefined;
        return memorySearch(config, query, { limit, type, projectId });
      },
      get: async (id: string) => {
        if (!id || !id.trim()) return null;
        return memoryGet(config, id);
      },
    });

    // --- Memory prompt builder (required by memory slot contract) -----------
    api.registerMemoryPromptSection({
      id: "amm-recall",
      promptBuilder: async (ctx: Record<string, unknown>) => {
        const query = typeof ctx?.query === "string" ? ctx.query : "";
        const sessionId = typeof ctx?.sessionKey === "string" ? ctx.sessionKey : "";
        if (!query.trim()) return "";

        const raw = await recall(config, query, sessionId);
        const rendered = renderRecall(raw, config.recallLimit);
        if (!rendered) return "";
        return `<amm-context>\n${rendered}\n</amm-context>`;
      },
    });

    // --- Fallback: before_prompt_build for non-slot recall -----------------
    api.on(
      "before_prompt_build",
      async (event) => {
        const promptEvent = event as PromptBuildEvent;
        const query = extractUserQuery(promptEvent);
        if (!query) return {};

        const context = await ambientRecall(config, query, promptEvent.sessionKey);
        if (!context) return {};
        return { prependContext: `<amm-context>\n${context}\n</amm-context>` };
      },
      { name: "amm.recall", description: "Inject AMM ambient recall before LLM prompt" },
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
        name: `amm.capture.${hookEvent}`,
        description: `Capture ${hookEvent} events into AMM history`,
      });
    }
  },
};
