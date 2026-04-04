/**
 * AMM native OpenClaw plugin.
 *
 * Registers hooks for:
 * - Ambient recall injection + two-tier memory guidance via before_prompt_build
 * - Event capture via message and tool hooks
 * - Optional curated memory mirroring (MEMORY.md/USER.md → AMM) via agent_end
 *
 * This plugin is intentionally hot-path only. It does not run maintenance
 * jobs. Keep maintenance on an external schedule via host cron or systemd
 * running `amm jobs run <kind>` or `examples/scripts/run-workers.sh`.
 */

import { resolveConfig, type AmmConfig } from "./src/config.ts";
import { captureEvent, type HookEvent } from "./src/capture.ts";
import { ambientRecall } from "./src/recall.ts";
import { snapshotCurated, reconcileCurated } from "./src/curated-sync.ts";

// ---------------------------------------------------------------------------
// System prompt guidance — two-tier memory model
// ---------------------------------------------------------------------------

function systemPromptBlock(config: AmmConfig): string {
  const curatedProject = config.curatedProjectId || config.projectId;
  const projectHint = curatedProject
    ? `\nWhen saving your own memories (preferences, decisions, lessons), ` +
      `pass \`project_id: "${curatedProject}"\` to amm_remember or ` +
      `\`--project ${curatedProject}\` to \`amm remember\` so they are ` +
      `scoped to your memory space rather than the general store.`
    : "";
  return (
    "## Long-term memory (AMM)\n" +
    "You have two memory tiers:\n" +
    "- **Built-in memory** (MEMORY.md/USER.md): small, always visible. " +
    "Keep only high-frequency context here — active preferences, current constraints, " +
    "things you need on every turn.\n" +
    "- **AMM** (via MCP tools or CLI): unlimited durable memory. " +
    "Relevant AMM memories are automatically surfaced each turn. " +
    "Use amm_remember (MCP) or `amm remember` (CLI) for detailed decisions, " +
    "procedures, project context, and anything too large for built-in memory. " +
    "Use amm_recall (MCP) or `amm recall` (CLI) for targeted search " +
    "when you need specific history.\n" +
    "When a recalled memory looks relevant but the summary is too thin to act on, " +
    "use amm_expand (MCP) or `amm expand` (CLI) with max_depth 1-2 to get the " +
    "full context — linked entities, related decisions, and child summaries.\n" +
    "When built-in memory is full, save detail to AMM rather than compressing " +
    "or discarding entries. Let built-in memory stay lean." +
    projectHint
  );
}

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
        const guidance = systemPromptBlock(config);
        const parts: string[] = [];
        if (guidance) parts.push(guidance);
        if (context) parts.push(context);
        if (parts.length === 0) return {};
        return { prependContext: `<amm-context>\n${parts.join("\n\n")}\n</amm-context>` };
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

    // --- Curated memory mirroring -----------------------------------------
    if (config.syncCuratedMemory) {
      // Snapshot MEMORY.md/USER.md at session start.
      api.registerHook(
        "before_agent_start",
        async () => {
          snapshotCurated();
        },
        { name: "amm.curated.snapshot", description: "Snapshot curated memory files for diff-based sync" },
      );

      // Diff and reconcile after each turn.
      api.registerHook(
        "agent_end",
        async () => {
          await reconcileCurated(config, "memory");
          await reconcileCurated(config, "user");
        },
        { name: "amm.curated.reconcile", description: "Mirror curated memory changes to AMM durable memories" },
      );
    }
  },
};
