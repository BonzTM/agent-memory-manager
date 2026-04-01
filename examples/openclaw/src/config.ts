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
}

const DEFAULT_AMM_BIN = "amm";
const DEFAULT_DB_PATH = `${process.env.HOME ?? "~"}/.amm/amm.db`;
const DEFAULT_RECALL_LIMIT = 5;

function normalizeApiUrl(raw: string): string {
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
    projectId:
      asString(cfg["projectId"]) ??
      process.env.AMM_PROJECT_ID ??
      "",
    recallLimit: clampRecallLimit(
      cfg["recallLimit"] ?? process.env.AMM_OPENCLAW_RECALL_LIMIT ?? DEFAULT_RECALL_LIMIT,
    ),
  };
}

/** True when the resolved config points at an HTTP API endpoint. */
export function useHttpApi(config: AmmConfig): boolean {
  return config.apiUrl.length > 0;
}

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}
