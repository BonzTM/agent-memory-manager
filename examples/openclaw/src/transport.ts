/**
 * Dual transport layer for amm: local binary (subprocess) or HTTP API.
 *
 * When AMM_API_URL / config.apiUrl is set, all calls go to the REST API.
 * Otherwise, the local `amm` binary is invoked via spawnSync.
 *
 * This file is NOT included in the npm package (OpenClaw's security
 * scanner blocks child_process imports). The npm package uses
 * transport-http.ts instead. install.sh rewires imports to this file
 * for local installs that need binary mode.
 */

import { spawnSync } from "node:child_process";
import type { AmmConfig } from "./config.ts";
import { useHttpApi } from "./config.ts";

// ---------------------------------------------------------------------------
// Local binary transport
// ---------------------------------------------------------------------------

export interface BinaryResult {
  ok: boolean;
  stdout: string;
  stderr: string;
}

export function runAmm(config: AmmConfig, args: string[], stdin?: string): BinaryResult {
  const result = spawnSync(config.ammBin, args, {
    input: stdin,
    encoding: "utf8",
    timeout: 10_000,
    env: { ...process.env, AMM_DB_PATH: config.dbPath },
  });

  return {
    ok: !result.error && result.status === 0,
    stdout: result.stdout ?? "",
    stderr: result.error?.message ?? result.stderr ?? "",
  };
}

export function runAmmJson(config: AmmConfig, args: string[], stdin?: string): Record<string, unknown> {
  const result = runAmm(config, args, stdin);
  if (!result.ok || !result.stdout) return {};
  try {
    return JSON.parse(result.stdout) as Record<string, unknown>;
  } catch {
    return {};
  }
}

// ---------------------------------------------------------------------------
// HTTP API transport
// ---------------------------------------------------------------------------

function httpHeaders(config: AmmConfig): Record<string, string> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (config.apiKey) {
    headers["Authorization"] = `Bearer ${config.apiKey}`;
  }
  return headers;
}

export async function postJson(
  config: AmmConfig,
  path: string,
  payload: Record<string, unknown>,
  timeoutMs = 10_000,
): Promise<Record<string, unknown>> {
  if (!config.apiUrl) return {};

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

// ---------------------------------------------------------------------------
// Unified operations
// ---------------------------------------------------------------------------

/** Ingest a single event into amm history. */
export async function ingestEvent(
  config: AmmConfig,
  event: Record<string, unknown>,
): Promise<void> {
  if (useHttpApi(config)) {
    await postJson(config, "/events", event, 5_000);
    return;
  }
  runAmm(config, ["ingest", "event", "--in", "-"], JSON.stringify(event));
}

/** Search memories by query text. Returns the raw result object. */
export async function memorySearch(
  config: AmmConfig,
  query: string,
  opts?: { limit?: number; type?: string; scope?: string; projectId?: string },
): Promise<Record<string, unknown>> {
  if (useHttpApi(config)) {
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

  const args = ["recall", "--mode", "hybrid", "--json"];
  if (opts?.limit) args.push("--limit", String(opts.limit));
  if (opts?.type) args.push("--type", opts.type);
  if (opts?.projectId ?? config.projectId) args.push("--project", opts?.projectId ?? config.projectId);
  args.push(query);
  return runAmmJson(config, args);
}

/** Get a single memory by ID. Returns the raw result object. */
export async function memoryGet(
  config: AmmConfig,
  memoryId: string,
): Promise<Record<string, unknown>> {
  if (useHttpApi(config)) {
    const url = `${config.apiUrl}/memories/${encodeURIComponent(memoryId)}`;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 10_000);
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

  return runAmmJson(config, ["get-memory", memoryId, "--json"]);
}

/** Run ambient recall and return the raw result object. */
export async function recall(
  config: AmmConfig,
  query: string,
  sessionId?: string,
): Promise<Record<string, unknown>> {
  if (useHttpApi(config)) {
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

  const args = ["recall", "--mode", "ambient", "--json"];
  if (sessionId) args.push("--session", sessionId);
  if (config.projectId) args.push("--project", config.projectId);
  args.push(query);
  return runAmmJson(config, args);
}
