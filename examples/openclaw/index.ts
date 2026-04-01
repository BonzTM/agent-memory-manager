/**
 * AMM native OpenClaw plugin.
 *
 * Claims the memory slot via registerMemoryRuntime, providing the full
 * MemoryPluginRuntime contract. Injects ambient recall via
 * registerMemoryPromptSection and captures conversation events via
 * registerHook.
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
  registerMemoryRuntime(runtime: unknown): void;
  registerMemoryPromptSection(builder: unknown): void;
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
// AMM Memory Search Manager (implements RegisteredMemorySearchManager)
// ---------------------------------------------------------------------------

function createAmmSearchManager(config: AmmConfig) {
  return {
    status() {
      return {
        ready: config.apiUrl !== "" || config.ammBin !== "",
        provider: "amm",
        backend: config.apiUrl ? "http" : "local",
        summary: config.apiUrl
          ? `AMM HTTP API at ${config.apiUrl}`
          : `AMM local binary (${config.ammBin})`,
      };
    },
    async probeEmbeddingAvailability() {
      return { available: false, reason: "AMM manages its own embeddings" };
    },
    async probeVectorAvailability() {
      return false; // AMM manages its own vector indexes
    },
    async close() {
      // No persistent connections to close
    },
  };
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

    // --- Memory runtime (required by memory slot contract) -----------------
    // Implements MemoryPluginRuntime interface.
    const searchManager = createAmmSearchManager(config);

    api.registerMemoryRuntime({
      async getMemorySearchManager(_params: unknown) {
        return { manager: searchManager };
      },
      resolveMemoryBackendConfig(_params: unknown) {
        return { backend: "builtin" };
      },
      async closeAllMemorySearchManagers() {
        await searchManager.close();
      },
    });

    // --- Memory prompt section builder (synchronous, returns string[]) -----
    api.registerMemoryPromptSection((_params: unknown) => {
      // The prompt section builder must be synchronous per the contract.
      // We can't do async recall here. Return empty and rely on the
      // before_prompt_build hook for actual recall injection.
      return [];
    });

    // --- Ambient recall injection (before_prompt_build) -------------------
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
