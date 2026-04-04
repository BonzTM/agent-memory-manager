/**
 * Configuration resolution for the AMM OpenClaw plugin.
 *
 * Priority: plugin configSchema values > environment variables > defaults.
 */

export interface AmmConfig {
  /** Path to the local amm binary. */
  ammBin: string;
  /** Path to the amm SQLite database (binary mode only). */
  dbPath: string;
  /** Base URL for the AMM HTTP API. When set, switches to HTTP transport. */
  apiUrl: string;
  /** Bearer token for AMM HTTP API authentication. */
  apiKey: string;
  /** Stable project identifier for scoped recall. */
  projectId: string;
  /** Maximum number of recall items to render in the context block. */
  recallLimit: number;
  /** Enable curated memory mirroring from MEMORY.md/USER.md to AMM. */
  syncCuratedMemory: boolean;
  /** Override project ID for curated memory writes. Falls back to projectId. */
  curatedProjectId: string;
  /** AMM scope for MEMORY.md entries. */
  memoryScope: string;
  /** AMM scope for USER.md entries. */
  userScope: string;
  /** AMM memory type for MEMORY.md entries. */
  memoryType: string;
  /** AMM memory type for USER.md entries. */
  userType: string;
  /** Directory for curated memory sync state files. */
  stateDir: string;
}

const DEFAULT_AMM_BIN = "amm";
const DEFAULT_DB_PATH = `${process.env.HOME ?? "~"}/.amm/amm.db`;
const DEFAULT_RECALL_LIMIT = 5;

function normalizeApiUrl(raw: string | undefined | null): string {
  if (!raw) return "";
  const trimmed = raw.trim().replace(/\/+$/, "");
  if (!trimmed) return "";
  return trimmed.endsWith("/v1") ? trimmed : `${trimmed}/v1`;
}

function clampRecallLimit(raw: unknown): number {
  const n = typeof raw === "number" ? raw : Number(raw);
  if (!Number.isFinite(n) || n < 1) return DEFAULT_RECALL_LIMIT;
  return Math.floor(n);
}

/**
 * Resolve configuration from plugin config (passed by OpenClaw) and
 * environment variables. Plugin config takes precedence over env.
 */
export function resolveConfig(pluginConfig?: Record<string, unknown>): AmmConfig {
  const cfg = pluginConfig ?? {};

  const projectId =
    asString(cfg["projectId"]) ??
    process.env.AMM_PROJECT_ID ??
    "";

  return {
    ammBin:
      asString(cfg["ammBin"]) ??
      process.env.AMM_BIN ??
      DEFAULT_AMM_BIN,
    dbPath:
      asString(cfg["dbPath"]) ??
      process.env.AMM_DB_PATH ??
      DEFAULT_DB_PATH,
    apiUrl: normalizeApiUrl(
      asString(cfg["apiUrl"]) ??
      process.env.AMM_API_URL ??
      "",
    ),
    apiKey:
      asString(cfg["apiKey"]) ??
      process.env.AMM_API_KEY ??
      "",
    projectId,
    recallLimit: clampRecallLimit(
      cfg["recallLimit"] ?? process.env.AMM_OPENCLAW_RECALL_LIMIT ?? DEFAULT_RECALL_LIMIT,
    ),
    syncCuratedMemory:
      envBool(asString(cfg["syncCuratedMemory"]) ?? process.env.AMM_OPENCLAW_SYNC_CURATED_MEMORY),
    curatedProjectId:
      asString(cfg["curatedProjectId"]) ??
      process.env.AMM_OPENCLAW_CURATED_PROJECT_ID ??
      projectId,
    memoryScope:
      asString(cfg["memoryScope"]) ??
      process.env.AMM_OPENCLAW_MEMORY_SCOPE ??
      "project",
    userScope:
      asString(cfg["userScope"]) ??
      process.env.AMM_OPENCLAW_USER_SCOPE ??
      "global",
    memoryType:
      asString(cfg["memoryType"]) ??
      process.env.AMM_OPENCLAW_MEMORY_TYPE ??
      "fact",
    userType:
      asString(cfg["userType"]) ??
      process.env.AMM_OPENCLAW_USER_TYPE ??
      "preference",
    stateDir:
      asString(cfg["stateDir"]) ??
      process.env.AMM_OPENCLAW_STATE_DIR ??
      `${process.env.HOME ?? "~"}/.openclaw/state/amm-plugin`,
  };
}

/** True when the resolved config points at an HTTP API endpoint. */
export function useHttpApi(config: AmmConfig): boolean {
  return config.apiUrl.length > 0;
}

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function envBool(value: string | undefined | null): boolean {
  if (!value) return false;
  return ["1", "true", "yes", "on"].includes(value.trim().toLowerCase());
}
