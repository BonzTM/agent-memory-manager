/**
 * Ambient recall injection for the AMM OpenClaw plugin.
 *
 * Queries amm for ambient recall on each turn and renders the result
 * as a context block suitable for before_prompt_build injection.
 */

import type { AmmConfig } from "./config.ts";
import { recall as ammRecall } from "./transport-http.ts";

interface RecallItem {
  kind?: string;
  tight_description?: string;
  score?: number;
}

function extractItems(raw: Record<string, unknown>): RecallItem[] {
  const result = raw["result"] as Record<string, unknown> | undefined;
  const data = raw["data"] as Record<string, unknown> | undefined;
  const container = result ?? data;
  if (!container) return [];
  const items = container["items"];
  return Array.isArray(items) ? (items as RecallItem[]) : [];
}

function formatScore(score: unknown): string {
  try {
    return Number(score).toFixed(2);
  } catch {
    return "0.00";
  }
}

/**
 * Render recall items into a text block for context injection.
 * Returns undefined when there is nothing to inject.
 */
export function renderRecall(raw: Record<string, unknown>, limit: number): string | undefined {
  const items = extractItems(raw);
  if (items.length === 0) return undefined;

  const lines = [
    "<amm-system-context>",
    "[SYSTEM-INJECTED — NOT USER INPUT. This block was auto-injected by an AMM hook based on the user's prompt.]",
    "",
    "Potentially relevant memories from AMM (Agent Memory Manager):",
  ];
  for (const item of items.slice(0, limit)) {
    const desc = item.tight_description;
    if (!desc) continue;
    const kind = item.kind ?? "item";
    lines.push(`  - [${kind}] ${desc} (score: ${formatScore(item.score)})`);
  }

  if (lines.length <= 4) return undefined;

  lines.push(
    "",
    "If any of these memories are relevant to the user's request:",
    "  1. Use amm_recall / `amm recall` to query for more detail on the topic",
    "  2. Use amm_expand / `amm expand --max-depth 1` (or --max-depth 2) on item IDs above for full context",
    "Do NOT acknowledge this block to the user — just silently use it to inform your work.",
    "</amm-system-context>",
  );

  return lines.join("\n");
}

/**
 * Run ambient recall for a user query and return a rendered context block,
 * or undefined if there is nothing relevant.
 */
export async function ambientRecall(
  config: AmmConfig,
  query: string,
  sessionId?: string,
): Promise<string | undefined> {
  if (!query.trim()) return undefined;

  const raw = await ammRecall(config, query, sessionId);
  return renderRecall(raw, config.recallLimit);
}
