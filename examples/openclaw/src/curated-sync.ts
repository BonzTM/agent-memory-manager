/**
 * Curated memory mirroring for the AMM OpenClaw plugin.
 *
 * Snapshots MEMORY.md/USER.md at session start and diffs at agent_end
 * to mirror adds, removes, and replacements to AMM durable memories.
 * Full reconciliation runs at session end as a safety net.
 *
 * Mirrors the same pattern as the Hermes memory provider plugin.
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { join, dirname } from "node:path";
import type { AmmConfig } from "./config.ts";
import {
  rememberMemory,
  updateMemory,
  forgetMemory,
} from "./transport-http.ts";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface SyncRecord {
  target: string;
  scope: string;
  content: string;
  fingerprint: string;
  amm_memory_id: string;
  project_id: string;
  updated_at: string;
}

interface SyncState {
  records: SyncRecord[];
}

interface QueueEntry {
  occurred_at: string;
  source_system: string;
  kind: string;
  action: string;
  target: string;
  project_id: string;
  error: string;
  details: Record<string, unknown>;
}

type Target = "memory" | "user";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const ENTRY_DELIMITER = "\n§\n";
const MAX_TIGHT_DESCRIPTION = 120;
const MAX_QUEUE_LINES = 500;

// ---------------------------------------------------------------------------
// In-memory snapshot (per plugin lifecycle)
// ---------------------------------------------------------------------------

const snapshot: Record<Target, string[]> = { memory: [], user: [] };

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function nowRfc3339(): string {
  return new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
}

function normalizeText(value: string): string {
  return value
    .trim()
    .split("\n")
    .map((line) => line.trimEnd())
    .join("\n")
    .trim();
}

function tightDescription(content: string): string {
  const lines = content
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l.length > 0);
  let candidate = lines[0] ?? content.trim();
  candidate = candidate.replace(/\s+/g, " ");
  if (candidate.length <= MAX_TIGHT_DESCRIPTION) return candidate;
  return candidate.slice(0, MAX_TIGHT_DESCRIPTION - 1).trimEnd() + "…";
}

function fingerprint(
  target: string,
  content: string,
  scope: string,
  projectId: string,
): string {
  const hash = createHash("sha256");
  hash.update(target);
  hash.update("\n");
  hash.update(scope);
  hash.update("\n");
  hash.update(scope === "project" ? projectId : "");
  hash.update("\n");
  hash.update(content);
  return hash.digest("hex");
}

function curatedScope(config: AmmConfig, target: Target, projectId: string): string {
  const configured = target === "user" ? config.userScope : config.memoryScope;
  const valid = configured === "global" || configured === "project" || configured === "session";
  const scope = valid ? configured : target === "user" ? "global" : "project";
  if (scope === "project" && !projectId) return "global";
  return scope;
}

function curatedType(config: AmmConfig, target: Target): string {
  return target === "user" ? config.userType : config.memoryType;
}

function curatedSubject(target: Target): string {
  return target === "user" ? "openclaw_curated_user" : "openclaw_curated_memory";
}

function curatedProjectId(config: AmmConfig): string {
  return config.curatedProjectId || config.projectId;
}

// ---------------------------------------------------------------------------
// File paths
// ---------------------------------------------------------------------------

function curatedFilePath(target: Target): string {
  const home = process.env.HOME ?? "~";
  const openclawDir = process.env.OPENCLAW_HOME ?? join(home, ".openclaw");
  const memoriesDir = join(openclawDir, "memories");
  return join(memoriesDir, target === "user" ? "USER.md" : "MEMORY.md");
}

function mapPath(config: AmmConfig): string {
  return join(config.stateDir, "curated-memory-id-map.json");
}

function queuePath(config: AmmConfig): string {
  return join(config.stateDir, "curated-memory-sync-queue.jsonl");
}

// ---------------------------------------------------------------------------
// Curated file reading
// ---------------------------------------------------------------------------

function readCuratedEntries(target: Target): string[] {
  const path = curatedFilePath(target);
  if (!existsSync(path)) return [];
  try {
    const raw = readFileSync(path, "utf-8");
    if (!raw.trim()) return [];
    return raw
      .split(ENTRY_DELIMITER)
      .map(normalizeText)
      .filter((entry) => entry.length > 0);
  } catch {
    return [];
  }
}

// ---------------------------------------------------------------------------
// Sync state persistence
// ---------------------------------------------------------------------------

function loadSyncState(config: AmmConfig): SyncState {
  const path = mapPath(config);
  if (!existsSync(path)) return { records: [] };
  try {
    const data = JSON.parse(readFileSync(path, "utf-8")) as SyncState;
    if (!Array.isArray(data.records)) data.records = [];
    return data;
  } catch {
    return { records: [] };
  }
}

function saveSyncState(config: AmmConfig, state: SyncState): void {
  const path = mapPath(config);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(state, null, 2) + "\n", "utf-8");
}

function appendSyncQueue(config: AmmConfig, entry: QueueEntry): void {
  const path = queuePath(config);
  mkdirSync(dirname(path), { recursive: true });
  let lines: string[] = [];
  if (existsSync(path)) {
    try {
      lines = readFileSync(path, "utf-8").split("\n").filter((l) => l.length > 0);
    } catch {
      lines = [];
    }
  }
  lines.push(JSON.stringify(entry));
  if (lines.length > MAX_QUEUE_LINES) {
    lines = lines.slice(-MAX_QUEUE_LINES);
  }
  writeFileSync(path, lines.join("\n") + "\n", "utf-8");
}

function queueFailedSync(
  config: AmmConfig,
  action: string,
  target: string,
  projectId: string,
  details: Record<string, unknown>,
  error: string,
): void {
  appendSyncQueue(config, {
    occurred_at: nowRfc3339(),
    source_system: "openclaw",
    kind: "curated_memory_sync_failed",
    action,
    target,
    project_id: projectId,
    error,
    details,
  });
}

// ---------------------------------------------------------------------------
// AMM memory ID extraction
// ---------------------------------------------------------------------------

function extractMemoryId(result: Record<string, unknown>): string {
  if (typeof result["id"] === "string" && result["id"]) return result["id"];
  const nested = result["result"];
  if (typeof nested === "object" && nested !== null) {
    const id = (nested as Record<string, unknown>)["id"];
    if (typeof id === "string" && id) return id;
  }
  const data = result["data"];
  if (typeof data === "object" && data !== null) {
    const id = (data as Record<string, unknown>)["id"];
    if (typeof id === "string" && id) return id;
  }
  return "";
}

// ---------------------------------------------------------------------------
// Sync operations
// ---------------------------------------------------------------------------

async function syncAdd(
  config: AmmConfig,
  target: Target,
  content: string,
  projectId: string,
  scope: string,
  effectiveProjectId: string,
): Promise<void> {
  const state = loadSyncState(config);
  const fp = fingerprint(target, content, scope, effectiveProjectId);
  if (state.records.some((r) => r.fingerprint === fp)) return;

  const payload: Record<string, unknown> = {
    type: curatedType(config, target),
    scope,
    body: content,
    tight_description: tightDescription(content),
    subject: curatedSubject(target),
  };
  if (scope === "project" && projectId) payload["project_id"] = projectId;

  const result = await rememberMemory(config, payload);
  const memoryId = extractMemoryId(result);
  if (!memoryId) {
    queueFailedSync(config, "add", target, projectId, { content, scope }, "create_failed");
    return;
  }

  state.records.push({
    target,
    scope,
    content,
    fingerprint: fp,
    amm_memory_id: memoryId,
    project_id: effectiveProjectId,
    updated_at: nowRfc3339(),
  });
  saveSyncState(config, state);
}

async function syncRemove(
  config: AmmConfig,
  target: Target,
  content: string,
  projectId: string,
): Promise<void> {
  const state = loadSyncState(config);
  const matches = state.records
    .map((r, i) => ({ record: r, index: i }))
    .filter(
      ({ record }) =>
        record.target === target && normalizeText(record.content) === content,
    );
  if (matches.length !== 1) return;

  const { record, index } = matches[0]!;
  if (!record.amm_memory_id) {
    queueFailedSync(config, "remove", target, projectId, { content }, "missing_memory_id");
    return;
  }
  if (!(await forgetMemory(config, record.amm_memory_id))) {
    queueFailedSync(
      config,
      "remove",
      target,
      projectId,
      { content, amm_memory_id: record.amm_memory_id },
      "delete_failed",
    );
    return;
  }
  state.records.splice(index, 1);
  saveSyncState(config, state);
}

async function syncReplace(
  config: AmmConfig,
  target: Target,
  newContent: string,
  projectId: string,
  scope: string,
  effectiveProjectId: string,
): Promise<void> {
  const state = loadSyncState(config);
  const currentEntries = new Set(readCuratedEntries(target));

  // Find the record whose content no longer appears in the curated file.
  const disappeared = state.records
    .map((r, i) => ({ record: r, index: i }))
    .filter(
      ({ record }) =>
        record.target === target &&
        !currentEntries.has(normalizeText(record.content)),
    );

  if (disappeared.length === 1) {
    const { record, index } = disappeared[0]!;
    if (record.amm_memory_id) {
      const payload: Record<string, unknown> = {
        body: newContent,
        tight_description: tightDescription(newContent),
        type: curatedType(config, target),
        scope,
        status: "active",
      };
      if (await updateMemory(config, record.amm_memory_id, payload)) {
        state.records[index] = {
          ...record,
          scope,
          content: newContent,
          fingerprint: fingerprint(target, newContent, scope, effectiveProjectId),
          project_id: effectiveProjectId,
          updated_at: nowRfc3339(),
        };
        saveSyncState(config, state);
        return;
      }
      queueFailedSync(
        config,
        "replace",
        target,
        projectId,
        { content: newContent, amm_memory_id: record.amm_memory_id },
        "update_failed",
      );
    }
  }

  // Fall back to add if we couldn't find or update the old record.
  await syncAdd(config, target, newContent, projectId, scope, effectiveProjectId);
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/** Take a snapshot of curated files. Call at session start. */
export function snapshotCurated(): void {
  snapshot.memory = readCuratedEntries("memory");
  snapshot.user = readCuratedEntries("user");
}

/**
 * Reconcile curated file changes against AMM. Call at agent_end.
 * Diffs current file contents against the snapshot, syncing
 * adds/removes/replacements.
 */
export async function reconcileCurated(
  config: AmmConfig,
  target: Target,
): Promise<void> {
  const currentEntries = readCuratedEntries(target);
  const previousEntries = snapshot[target];

  const projectId = curatedProjectId(config);
  const scope = curatedScope(config, target, projectId);
  const effectiveProjectId = scope === "project" ? projectId : "";

  const previousSet = new Set(previousEntries);
  const currentSet = new Set(currentEntries);

  const removed = previousEntries.filter((e) => !currentSet.has(e));
  const added = currentEntries.filter((e) => !previousSet.has(e));

  // If entries were both removed and added, treat as replacements.
  if (removed.length > 0 && added.length > 0) {
    // Process replacements (paired removes + adds).
    const pairCount = Math.min(removed.length, added.length);
    for (let i = 0; i < pairCount; i++) {
      await syncReplace(config, target, added[i]!, projectId, scope, effectiveProjectId);
    }
    // Process remaining unpaired removes.
    for (let i = pairCount; i < removed.length; i++) {
      await syncRemove(config, target, removed[i]!, projectId);
    }
    // Process remaining unpaired adds.
    for (let i = pairCount; i < added.length; i++) {
      await syncAdd(config, target, added[i]!, projectId, scope, effectiveProjectId);
    }
  } else {
    for (const entry of removed) {
      await syncRemove(config, target, entry, projectId);
    }
    for (const entry of added) {
      await syncAdd(config, target, entry, projectId, scope, effectiveProjectId);
    }
  }

  // Update snapshot.
  snapshot[target] = currentEntries;
}
