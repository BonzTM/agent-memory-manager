/**
 * HTTP-only transport layer for amm.
 *
 * Used by the npm-published package. For local binary mode, use
 * install.sh which swaps in the full transport.ts.
 */

import type { AmmConfig } from "./config.ts";

function httpHeaders(config: AmmConfig): Record<string, string> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (config.apiKey) {
    headers["Authorization"] = `Bearer ${config.apiKey}`;
  }
  return headers;
}

async function postJson(
  config: AmmConfig,
  path: string,
  payload: Record<string, unknown>,
  timeoutMs = 10_000,
): Promise<Record<string, unknown>> {
  if (!config.apiUrl) {
    console.warn("[amm] No apiUrl configured — HTTP transport requires AMM_API_URL or plugin config apiUrl. For local binary mode, reinstall via install.sh.");
    return {};
  }

  const url = `${config.apiUrl}${path}`;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const response = await fetch(url, {
      method: "POST",
      headers: httpHeaders(config),
      body: JSON.stringify(payload),
      signal: controller.signal,
    });
    if (!response.ok) return {};
    const text = await response.text();
    return text ? (JSON.parse(text) as Record<string, unknown>) : {};
  } catch {
    return {};
  } finally {
    clearTimeout(timer);
  }
}

async function getJson(
  config: AmmConfig,
  path: string,
  timeoutMs = 10_000,
): Promise<Record<string, unknown>> {
  if (!config.apiUrl) return {};

  const url = `${config.apiUrl}${path}`;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const response = await fetch(url, {
      method: "GET",
      headers: httpHeaders(config),
      signal: controller.signal,
    });
    if (!response.ok) return {};
    const text = await response.text();
    return text ? (JSON.parse(text) as Record<string, unknown>) : {};
  } catch {
    return {};
  } finally {
    clearTimeout(timer);
  }
}

/** Ingest a single event into amm history. */
export async function ingestEvent(
  config: AmmConfig,
  event: Record<string, unknown>,
): Promise<void> {
  await postJson(config, "/events", event, 5_000);
}

/** Run ambient recall and return the raw result object. */
export async function recall(
  config: AmmConfig,
  query: string,
  sessionId?: string,
): Promise<Record<string, unknown>> {
  return postJson(config, "/recall", {
    query,
    opts: {
      mode: "ambient",
      limit: config.recallLimit,
      session_id: sessionId ?? "",
      project_id: config.projectId,
    },
  });
}

/** Search memories by query text. */
export async function memorySearch(
  config: AmmConfig,
  query: string,
  opts?: { limit?: number; type?: string; scope?: string; projectId?: string },
): Promise<Record<string, unknown>> {
  return postJson(config, "/recall", {
    query,
    opts: {
      mode: "hybrid",
      limit: opts?.limit ?? config.recallLimit,
      type: opts?.type ?? "",
      scope: opts?.scope ?? "",
      project_id: opts?.projectId ?? config.projectId,
    },
  });
}

/** Get a single memory by ID. */
export async function memoryGet(
  config: AmmConfig,
  memoryId: string,
): Promise<Record<string, unknown>> {
  return getJson(config, `/memories/${encodeURIComponent(memoryId)}`);
}
