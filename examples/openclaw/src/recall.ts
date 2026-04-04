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

  const lines = ["amm ambient memory recall:"];
  for (const item of items.slice(0, limit)) {
    const desc = item.tight_description;
    if (!desc) continue;
    const kind = item.kind ?? "item";
    lines.push(`- [${kind}] ${desc} (score: ${formatScore(item.score)})`);
  }

  return lines.length > 1 ? lines.join("\n") : undefined;
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
