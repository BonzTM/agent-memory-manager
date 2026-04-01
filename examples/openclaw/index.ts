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

  register(api: OpenClawPluginApi) {
    const config: AmmConfig = resolveConfig(api.pluginConfig);

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
