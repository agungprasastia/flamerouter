import { EventEmitter } from "events";
import { getAdapter } from "../driver";
import { parseJson, stringifyJson } from "../helpers/jsonCol";
import { getMeta, setMeta } from "../helpers/metaStore";

function maskApiKey(key?: string | null): string | null {
  if (!key || typeof key !== "string") return null;
  if (key.length <= 8) return key.charAt(0) + "***";
  return key.slice(0, 8) + "***";
}

const PENDING_TIMEOUT_MS = 60 * 1000;
const RING_CAP = 50;
const CONN_CACHE_TTL_MS = 30 * 1000;
const PERIOD_MS: Record<string, number> = {
  "24h": 86400000,
  "7d": 604800000,
  "30d": 2592000000,
  "60d": 5184000000,
};

interface PendingRequestsState {
  byModel: Record<string, number>;
  byAccount: Record<string, Record<string, number>>;
}

interface RecentRingItem {
  timestamp: string;
  provider: string;
  model: string;
  connectionId?: string | null;
  apiKey?: string | null;
  endpoint?: string | null;
  cost?: number;
  status?: string;
  tokens?: Record<string, number>;
}

interface UsageRepoGlobals {
  _pendingRequests?: PendingRequestsState;
  _lastErrorProvider?: { provider: string; ts: number };
  _statsEmitter?: EventEmitter;
  _pendingTimers?: Record<string, NodeJS.Timeout>;
  _recentRing?: { items: RecentRingItem[]; initialized: boolean };
  _connectionMapCache?: { map: Record<string, string>; ts: number };
  _statsEmitTimers?: { pending: NodeJS.Timeout | null; update: NodeJS.Timeout | null };
}

declare global {
  // eslint-disable-next-line no-var
  var _pendingRequests: PendingRequestsState | undefined;
  // eslint-disable-next-line no-var
  var _lastErrorProvider: { provider: string; ts: number } | undefined;
  // eslint-disable-next-line no-var
  var _statsEmitter: EventEmitter | undefined;
  // eslint-disable-next-line no-var
  var _pendingTimers: Record<string, NodeJS.Timeout> | undefined;
  // eslint-disable-next-line no-var
  var _recentRing: { items: RecentRingItem[]; initialized: boolean } | undefined;
  // eslint-disable-next-line no-var
  var _connectionMapCache: { map: Record<string, string>; ts: number } | undefined;
  // eslint-disable-next-line no-var
  var _statsEmitTimers: { pending: NodeJS.Timeout | null; update: NodeJS.Timeout | null } | undefined;
}

if (!global._pendingRequests)
  global._pendingRequests = { byModel: {}, byAccount: {} };
if (!global._lastErrorProvider)
  global._lastErrorProvider = { provider: "", ts: 0 };
if (!global._statsEmitter) {
  global._statsEmitter = new EventEmitter();
  global._statsEmitter.setMaxListeners(50);
}
if (!global._pendingTimers) global._pendingTimers = {};
if (!global._recentRing) global._recentRing = { items: [], initialized: false };
if (!global._connectionMapCache)
  global._connectionMapCache = { map: {}, ts: 0 };
if (!global._statsEmitTimers)
  global._statsEmitTimers = { pending: null, update: null };

const pendingRequests = global._pendingRequests as PendingRequestsState;
const lastErrorProvider = global._lastErrorProvider as { provider: string; ts: number };
const pendingTimers = global._pendingTimers as Record<string, NodeJS.Timeout>;
const recentRing = global._recentRing as { items: RecentRingItem[]; initialized: boolean };
const connCache = global._connectionMapCache as { map: Record<string, string>; ts: number };
const statsEmitTimers = global._statsEmitTimers as { pending: NodeJS.Timeout | null; update: NodeJS.Timeout | null };

export const statsEmitter = global._statsEmitter as EventEmitter;

function scheduleStatsEvent(event: string, delayMs = 150) {
  const key = event === "update" ? "update" : "pending";
  if (statsEmitTimers[key]) return;
  statsEmitTimers[key] = setTimeout(() => {
    statsEmitTimers[key] = null;
    statsEmitter.emit(event);
  }, delayMs);
  statsEmitTimers[key]?.unref?.();
}

function getLocalDateKey(timestamp?: string | number | Date): string {
  const d = timestamp ? new Date(timestamp) : new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export interface UsageCounterItem {
  requests: number;
  promptTokens: number;
  completionTokens: number;
  cachedTokens: number;
  cost: number;
  rawModel?: string;
  provider?: string;
  connectionId?: string;
  accountName?: string;
  apiKeyMasked?: string | null;
  keyName?: string;
  apiKeyKey?: string | null;
  endpoint?: string;
  lastUsed?: string;
  [key: string]: unknown;
}

function addToCounter(
  target: Record<string, UsageCounterItem>,
  key: string,
  values: {
    requests?: number;
    promptTokens?: number;
    completionTokens?: number;
    cachedTokens?: number;
    cost?: number;
    meta?: Record<string, unknown>;
  },
) {
  if (!target[key])
    target[key] = {
      requests: 0,
      promptTokens: 0,
      completionTokens: 0,
      cachedTokens: 0,
      cost: 0,
    };
  target[key].requests += values.requests || 1;
  target[key].promptTokens += values.promptTokens || 0;
  target[key].completionTokens += values.completionTokens || 0;
  target[key].cachedTokens += values.cachedTokens || 0;
  target[key].cost += values.cost || 0;
  if (values.meta) Object.assign(target[key], values.meta);
}

export interface DayUsageAggregate {
  requests?: number;
  promptTokens?: number;
  completionTokens?: number;
  cachedTokens?: number;
  cost?: number;
  byProvider?: Record<string, UsageCounterItem>;
  byModel?: Record<string, UsageCounterItem>;
  byAccount?: Record<string, UsageCounterItem>;
  byApiKey?: Record<string, UsageCounterItem>;
  byEndpoint?: Record<string, UsageCounterItem>;
}

export interface UsageHistoryEntry {
  timestamp?: string;
  provider?: string | null;
  model?: string | null;
  connectionId?: string | null;
  apiKey?: string | null;
  endpoint?: string | null;
  tokens?: {
    prompt_tokens?: number;
    input_tokens?: number;
    completion_tokens?: number;
    output_tokens?: number;
    cached_tokens?: number;
    cache_read_input_tokens?: number;
    [key: string]: number | undefined;
  };
  cost?: number;
  status?: string;
}

function aggregateEntryToDay(day: DayUsageAggregate, entry: UsageHistoryEntry) {
  const promptTokens =
    entry.tokens?.prompt_tokens || entry.tokens?.input_tokens || 0;
  const completionTokens =
    entry.tokens?.completion_tokens || entry.tokens?.output_tokens || 0;
  const cachedTokens =
    entry.tokens?.cached_tokens || entry.tokens?.cache_read_input_tokens || 0;
  const cost = entry.cost || 0;
  const vals = { promptTokens, completionTokens, cachedTokens, cost };

  day.requests = (day.requests || 0) + 1;
  day.promptTokens = (day.promptTokens || 0) + promptTokens;
  day.completionTokens = (day.completionTokens || 0) + completionTokens;
  day.cachedTokens = (day.cachedTokens || 0) + cachedTokens;
  day.cost = (day.cost || 0) + cost;

  day.byProvider ||= {};
  day.byModel ||= {};
  day.byAccount ||= {};
  day.byApiKey ||= {};
  day.byEndpoint ||= {};

  if (entry.provider) addToCounter(day.byProvider, entry.provider, vals);

  const modelKey = entry.provider
    ? `${entry.model}|${entry.provider}`
    : (entry.model || "unknown");
  addToCounter(day.byModel, modelKey, {
    ...vals,
    meta: { rawModel: entry.model, provider: entry.provider },
  });

  if (entry.connectionId) {
    addToCounter(day.byAccount, entry.connectionId, {
      ...vals,
      meta: { rawModel: entry.model, provider: entry.provider },
    });
  }

  const apiKeyVal =
    entry.apiKey && typeof entry.apiKey === "string"
      ? entry.apiKey
      : "local-no-key";
  const akModelKey = `${apiKeyVal}|${entry.model}|${entry.provider || "unknown"}`;
  addToCounter(day.byApiKey, akModelKey, {
    ...vals,
    meta: {
      rawModel: entry.model,
      provider: entry.provider,
      apiKey: entry.apiKey || null,
    },
  });

  const endpoint = entry.endpoint || "Unknown";
  const epKey = `${endpoint}|${entry.model}|${entry.provider || "unknown"}`;
  addToCounter(day.byEndpoint, epKey, {
    ...vals,
    meta: { endpoint, rawModel: entry.model, provider: entry.provider },
  });
}

function pushToRing(entry: RecentRingItem) {
  recentRing.items.push(entry);
  if (recentRing.items.length > RING_CAP) {
    recentRing.items = recentRing.items.slice(-RING_CAP);
  }
}

async function getConnectionMapCached(): Promise<Record<string, string>> {
  if (Date.now() - connCache.ts < CONN_CACHE_TTL_MS) return connCache.map;
  try {
    const { getProviderConnections } = await import("./connectionsRepo");
    const all = await getProviderConnections();
    const map: Record<string, string> = {};
    for (const c of all) map[c.id] = c.name || c.email || c.id;
    connCache.map = map;
    connCache.ts = Date.now();
  } catch {}
  return connCache.map;
}

async function ensureRingInitialized() {
  if (recentRing.initialized) return;
  recentRing.initialized = true;
  try {
    const db = await getAdapter();
    const rows = db.all<{
      timestamp: string;
      provider: string;
      model: string;
      connectionId?: string | null;
      apiKey?: string | null;
      endpoint?: string | null;
      cost?: number;
      status?: string;
      tokens?: string;
    }>(
      `SELECT timestamp, provider, model, connectionId, apiKey, endpoint, cost, status, tokens FROM usageHistory ORDER BY id DESC LIMIT ?`,
      [RING_CAP],
    );
    recentRing.items = rows.reverse().map((r) => ({
      timestamp: r.timestamp,
      provider: r.provider,
      model: r.model,
      connectionId: r.connectionId,
      apiKey: r.apiKey,
      endpoint: r.endpoint,
      cost: r.cost,
      status: r.status,
      tokens: parseJson<Record<string, number>>(r.tokens, {}) || {},
    }));
  } catch {}
}

async function calculateCost(provider?: string | null, model?: string | null, tokens?: Record<string, number>): Promise<number> {
  if (!tokens || !provider || !model) return 0;
  try {
    const { getPricingForModel } = await import("./pricingRepo");
    const pricing = await getPricingForModel(provider, model);
    if (!pricing) return 0;

    const { calculateCostFromTokens } =
      await import("@/shared/constants/pricing");
    return calculateCostFromTokens(tokens, {
      ...pricing,
      input: pricing.input ?? 0,
      output: pricing.output ?? 0,
    });
  } catch (e) {
    console.error("Error calculating cost:", e);
    return 0;
  }
}

export function trackPendingRequest(
  model: string,
  provider: string,
  connectionId?: string | null,
  started: boolean = true,
  error: boolean = false,
) {
  const modelKey = provider ? `${model} (${provider})` : model;
  const timerKey = `${connectionId || ""}|${modelKey}`;

  if (!pendingRequests.byModel[modelKey]) pendingRequests.byModel[modelKey] = 0;
  pendingRequests.byModel[modelKey] = Math.max(
    0,
    (pendingRequests.byModel[modelKey] || 0) + (started ? 1 : -1),
  );
  if (pendingRequests.byModel[modelKey] === 0)
    delete pendingRequests.byModel[modelKey];

  if (connectionId) {
    if (!pendingRequests.byAccount[connectionId])
      pendingRequests.byAccount[connectionId] = {};
    if (!pendingRequests.byAccount[connectionId][modelKey])
      pendingRequests.byAccount[connectionId][modelKey] = 0;
    pendingRequests.byAccount[connectionId][modelKey] = Math.max(
      0,
      (pendingRequests.byAccount[connectionId][modelKey] || 0) + (started ? 1 : -1),
    );
    if (pendingRequests.byAccount[connectionId][modelKey] === 0) {
      delete pendingRequests.byAccount[connectionId][modelKey];
      if (Object.keys(pendingRequests.byAccount[connectionId] || {}).length === 0) {
        delete pendingRequests.byAccount[connectionId];
      }
    }
  }

  if (started) {
    clearTimeout(pendingTimers[timerKey]);
    pendingTimers[timerKey] = setTimeout(() => {
      delete pendingTimers[timerKey];
      if ((pendingRequests.byModel[modelKey] || 0) > 0)
        pendingRequests.byModel[modelKey] = 0;
      if (
        connectionId &&
        (pendingRequests.byAccount[connectionId]?.[modelKey] || 0) > 0
      ) {
        pendingRequests.byAccount[connectionId][modelKey] = 0;
      }
      scheduleStatsEvent("pending");
    }, PENDING_TIMEOUT_MS);
  } else {
    clearTimeout(pendingTimers[timerKey]);
    delete pendingTimers[timerKey];
  }

  if (!started && error && provider) {
    lastErrorProvider.provider = provider.toLowerCase();
    lastErrorProvider.ts = Date.now();
  }

  scheduleStatsEvent("pending");
}

export interface ActiveRequestItem {
  model: string;
  provider: string;
  account: string;
  count: number;
}

export interface RecentRequestItem {
  timestamp: string;
  model: string;
  provider: string;
  promptTokens: number;
  completionTokens: number;
  status: string;
}

export async function getActiveRequests(): Promise<{
  activeRequests: ActiveRequestItem[];
  recentRequests: RecentRequestItem[];
  errorProvider: string;
}> {
  const activeRequests: ActiveRequestItem[] = [];
  const connectionMap = await getConnectionMapCached();

  for (const [connectionId, models] of Object.entries(
    pendingRequests.byAccount,
  )) {
    for (const [modelKey, count] of Object.entries(models as Record<string, number>)) {
      if (typeof count === "number" && count > 0) {
        const accountName =
          connectionMap[connectionId] ||
          `Account ${connectionId.slice(0, 8)}...`;
        const match = modelKey.match(/^(.*) \((.*)\)$/);
        activeRequests.push({
          model: match ? match[1] || modelKey : modelKey,
          provider: match ? match[2] || "unknown" : "unknown",
          account: accountName,
          count,
        });
      }
    }
  }

  await ensureRingInitialized();
  const seen = new Set<string>();
  const recentRequests: RecentRequestItem[] = [...recentRing.items]
    .sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
    .map((e) => {
      const t = e.tokens || {};
      return {
        timestamp: e.timestamp,
        model: e.model,
        provider: e.provider || "",
        promptTokens: t.prompt_tokens || t.input_tokens || 0,
        completionTokens: t.completion_tokens || t.output_tokens || 0,
        status: e.status || "ok",
      };
    })
    .filter((e) => {
      if (e.promptTokens === 0 && e.completionTokens === 0) return false;
      const minute = e.timestamp ? e.timestamp.slice(0, 16) : "";
      const key = `${e.model}|${e.provider}|${e.promptTokens}|${e.completionTokens}|${minute}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .slice(0, 20);

  const errorProvider =
    Date.now() - lastErrorProvider.ts < 10000 ? lastErrorProvider.provider : "";
  return { activeRequests, recentRequests, errorProvider };
}

export async function saveRequestUsage(entry: UsageHistoryEntry): Promise<void> {
  try {
    const db = await getAdapter();

    if (!entry.timestamp) entry.timestamp = new Date().toISOString();
    entry.cost = await calculateCost(entry.provider, entry.model, entry.tokens as Record<string, number>);

    const tokens = entry.tokens || {};
    const promptTokens = tokens.prompt_tokens || tokens.input_tokens || 0;
    const completionTokens =
      tokens.completion_tokens || tokens.output_tokens || 0;

    let inserted = false;

    db.transaction(() => {
      const existing = db.get<{ id: string | number; endpoint?: string }>(
        `SELECT id, endpoint FROM usageHistory
         WHERE timestamp = ?
           AND COALESCE(provider, '') = COALESCE(?, '')
           AND COALESCE(model, '') = COALESCE(?, '')
           AND COALESCE(connectionId, '') = COALESCE(?, '')
           AND COALESCE(apiKey, '') = COALESCE(?, '')
           AND promptTokens = ?
           AND completionTokens = ?
         ORDER BY id DESC LIMIT 1`,
        [
          entry.timestamp,
          entry.provider || null,
          entry.model || null,
          entry.connectionId || null,
          entry.apiKey || null,
          promptTokens,
          completionTokens,
        ],
      );

      if (existing) {
        if (!existing.endpoint && entry.endpoint) {
          db.run(`UPDATE usageHistory SET endpoint = ? WHERE id = ?`, [
            entry.endpoint,
            existing.id,
          ]);
        }
        return;
      }

      db.run(
        `INSERT INTO usageHistory(timestamp, provider, model, connectionId, apiKey, endpoint, promptTokens, completionTokens, cost, status, tokens, meta) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        [
          entry.timestamp,
          entry.provider || null,
          entry.model || null,
          entry.connectionId || null,
          entry.apiKey || null,
          entry.endpoint || null,
          promptTokens,
          completionTokens,
          entry.cost || 0,
          entry.status || "ok",
          stringifyJson(tokens),
          stringifyJson({}),
        ],
      );

      const dateKey = getLocalDateKey(entry.timestamp);
      const row = db.get<{ data: string }>(`SELECT data FROM usageDaily WHERE dateKey = ?`, [
        dateKey,
      ]);
      const day = row
        ? (parseJson<DayUsageAggregate>(row.data, {}) || {})
        : {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cost: 0,
            byProvider: {},
            byModel: {},
            byAccount: {},
            byApiKey: {},
            byEndpoint: {},
          };
      aggregateEntryToDay(day, entry);
      db.run(
        `INSERT INTO usageDaily(dateKey, data) VALUES(?, ?) ON CONFLICT(dateKey) DO UPDATE SET data = excluded.data`,
        [dateKey, stringifyJson(day)],
      );

      const cur = db.get<{ value: string }>(
        `SELECT value FROM _meta WHERE key = 'totalRequestsLifetime'`,
      );
      const next = (cur ? parseInt(cur.value, 10) : 0) + 1;
      db.run(
        `INSERT INTO _meta(key, value) VALUES('totalRequestsLifetime', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
        [String(next)],
      );
      inserted = true;
    });

    if (inserted) {
      pushToRing({
        timestamp: entry.timestamp,
        provider: entry.provider || "",
        model: entry.model || "",
        connectionId: entry.connectionId,
        apiKey: entry.apiKey,
        endpoint: entry.endpoint,
        cost: entry.cost,
        status: entry.status,
        tokens: entry.tokens as Record<string, number>,
      });
      scheduleStatsEvent("update", 250);
    }
  } catch (e) {
    console.error("Failed to save usage stats:", e);
  }
}

export interface UsageHistoryFilter {
  provider?: string;
  model?: string;
  startDate?: string | number | Date;
  endDate?: string | number | Date;
}

export async function getUsageHistory(filter: UsageHistoryFilter = {}) {
  const db = await getAdapter();
  const conds: string[] = [];
  const params: unknown[] = [];

  if (filter.provider) {
    conds.push("provider = ?");
    params.push(filter.provider);
  }
  if (filter.model) {
    conds.push("model = ?");
    params.push(filter.model);
  }
  if (filter.startDate) {
    conds.push("timestamp >= ?");
    params.push(new Date(filter.startDate).toISOString());
  }
  if (filter.endDate) {
    conds.push("timestamp <= ?");
    params.push(new Date(filter.endDate).toISOString());
  }

  const where = conds.length ? `WHERE ${conds.join(" AND ")}` : "";
  const rows = db.all<{
    timestamp: string;
    provider: string;
    model: string;
    connectionId: string | null;
    apiKey: string | null;
    endpoint: string | null;
    cost: number;
    status: string;
    tokens: string;
  }>(
    `SELECT timestamp, provider, model, connectionId, apiKey, endpoint, cost, status, tokens FROM usageHistory ${where} ORDER BY id ASC`,
    params,
  );

  return rows.map((r) => ({
    timestamp: r.timestamp,
    provider: r.provider,
    model: r.model,
    connectionId: r.connectionId,
    apiKeyMasked: maskApiKey(r.apiKey),
    endpoint: r.endpoint,
    cost: r.cost,
    status: r.status,
    tokens: parseJson(r.tokens, {}),
  }));
}

import type { DatabaseAdapter } from "../driver";

function loadDaysInRange(adapter: DatabaseAdapter, maxDays: number | null): Array<{ dateKey: string; data: string }> {
  if (maxDays == null) {
    return adapter.all<{ dateKey: string; data: string }>(`SELECT dateKey, data FROM usageDaily`);
  }
  const today = new Date();
  const cutoff = new Date(
    today.getFullYear(),
    today.getMonth(),
    today.getDate() - maxDays + 1,
  );
  const cutoffKey = `${cutoff.getFullYear()}-${String(cutoff.getMonth() + 1).padStart(2, "0")}-${String(cutoff.getDate()).padStart(2, "0")}`;
  return adapter.all<{ dateKey: string; data: string }>(
    `SELECT dateKey, data FROM usageDaily WHERE dateKey >= ?`,
    [cutoffKey],
  );
}

export async function getUsageStats(period = "all") {
  const db = await getAdapter();

  const [{ getProviderConnections }, { getApiKeys }, { getProviderNodes }] =
    await Promise.all([
      import("./connectionsRepo"),
      import("./apiKeysRepo"),
      import("./nodesRepo"),
    ]);

  let allConnections: Array<{ id: string; name?: string | null; email?: string | null }> = [];
  try {
    allConnections = await getProviderConnections();
  } catch {}
  const connectionMap: Record<string, string> = {};
  for (const c of allConnections)
    connectionMap[c.id] = c.name || c.email || c.id;

  const providerNodeNameMap: Record<string, string> = {};
  try {
    const nodes = await getProviderNodes();
    for (const n of nodes)
      if (n.id && n.name) providerNodeNameMap[n.id] = n.name;
  } catch {}

  let allApiKeys: Array<{ key: string; name?: string | null; id: string; createdAt: string }> = [];
  try {
    allApiKeys = await getApiKeys();
  } catch {}
  const apiKeyMap: Record<string, { name?: string | null; id: string; createdAt: string }> = {};
  for (const k of allApiKeys)
    apiKeyMap[k.key] = { name: k.name, id: k.id, createdAt: k.createdAt };

  // recentRequests from live history (last 100 entries enough for 20 deduped)
  const recentRows = db.all<{
    timestamp: string;
    provider?: string;
    model: string;
    tokens?: string;
    status?: string;
  }>(
    `SELECT timestamp, provider, model, tokens, status FROM usageHistory ORDER BY id DESC LIMIT 100`,
  );
  const seen = new Set<string>();
  const recentRequests = recentRows
    .map((r) => {
      const t = parseJson<{ prompt_tokens?: number; input_tokens?: number; completion_tokens?: number; output_tokens?: number; cached_tokens?: number; cache_read_input_tokens?: number }>(r.tokens, {}) || {};
      return {
        timestamp: r.timestamp,
        model: r.model,
        provider: r.provider || "",
        promptTokens: t.prompt_tokens || t.input_tokens || 0,
        completionTokens: t.completion_tokens || t.output_tokens || 0,
        cachedTokens: t.cached_tokens || t.cache_read_input_tokens || 0,
        status: r.status || "ok",
      };
    })
    .filter((e) => {
      if (e.promptTokens === 0 && e.completionTokens === 0) return false;
      const minute = e.timestamp ? e.timestamp.slice(0, 16) : "";
      const key = `${e.model}|${e.provider}|${e.promptTokens}|${e.completionTokens}|${minute}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .slice(0, 20);

  const stats: {
    totalRequests: number;
    totalPromptTokens: number;
    totalCompletionTokens: number;
    totalCachedTokens: number;
    totalCost: number;
    byProvider: Record<string, UsageCounterItem>;
    byModel: Record<string, UsageCounterItem>;
    byAccount: Record<string, UsageCounterItem>;
    byApiKey: Record<string, UsageCounterItem>;
    byEndpoint: Record<string, UsageCounterItem>;
    last10Minutes: Array<{ requests: number; promptTokens: number; completionTokens: number; cost: number }>;
    pending: PendingRequestsState;
    activeRequests: ActiveRequestItem[];
    recentRequests: typeof recentRequests;
    errorProvider: string;
  } = {
    totalRequests: 0,
    totalPromptTokens: 0,
    totalCompletionTokens: 0,
    totalCachedTokens: 0,
    totalCost: 0,
    byProvider: {},
    byModel: {},
    byAccount: {},
    byApiKey: {},
    byEndpoint: {},
    last10Minutes: [],
    pending: pendingRequests,
    activeRequests: [],
    recentRequests,
    errorProvider:
      Date.now() - lastErrorProvider.ts < 10000
        ? lastErrorProvider.provider
        : "",
  };

  // Active requests
  for (const [connectionId, models] of Object.entries(
    pendingRequests.byAccount,
  )) {
    for (const [modelKey, count] of Object.entries(models as Record<string, number>)) {
      if (typeof count === "number" && count > 0) {
        const accountName =
          connectionMap[connectionId] ||
          `Account ${connectionId.slice(0, 8)}...`;
        const match = modelKey.match(/^(.*) \((.*)\)$/);
        stats.activeRequests.push({
          model: match ? match[1] || modelKey : modelKey,
          provider: match ? match[2] || "unknown" : "unknown",
          account: accountName,
          count,
        });
      }
    }
  }

  // last10Minutes — query 10min window
  const now = new Date();
  const currentMinuteStart = new Date(
    Math.floor(now.getTime() / 60000) * 60000,
  );
  const tenMinutesAgo = new Date(currentMinuteStart.getTime() - 9 * 60 * 1000);
  const bucketMap: Record<number, { requests: number; promptTokens: number; completionTokens: number; cost: number }> = {};
  for (let i = 0; i < 10; i++) {
    const ts = currentMinuteStart.getTime() - (9 - i) * 60 * 1000;
    bucketMap[ts] = {
      requests: 0,
      promptTokens: 0,
      completionTokens: 0,
      cost: 0,
    };
    stats.last10Minutes.push(bucketMap[ts] as { requests: number; promptTokens: number; completionTokens: number; cost: number });
  }
  const recent10 = db.all<{ timestamp: string; promptTokens: number; completionTokens: number; cost: number }>(
    `SELECT timestamp, promptTokens, completionTokens, cost FROM usageHistory WHERE timestamp >= ? AND timestamp <= ?`,
    [tenMinutesAgo.toISOString(), now.toISOString()],
  );
  for (const r of recent10) {
    const tt = new Date(r.timestamp).getTime();
    const minuteStart = Math.floor(tt / 60000) * 60000;
    if (bucketMap[minuteStart]) {
      bucketMap[minuteStart].requests++;
      bucketMap[minuteStart].promptTokens += r.promptTokens || 0;
      bucketMap[minuteStart].completionTokens += r.completionTokens || 0;
      bucketMap[minuteStart].cost += r.cost || 0;
    }
  }

  const useDailySummary = period !== "24h" && period !== "today";

  if (useDailySummary) {
    const periodDays: Record<string, number> = { "7d": 7, "30d": 30, "60d": 60 };
    const maxDays = periodDays[period] ?? null;
    const dayRows = loadDaysInRange(db, maxDays);

    for (const dr of dayRows) {
      const dateKey = dr.dateKey;
      const day = parseJson<DayUsageAggregate>(dr.data, {}) || {};
      stats.totalPromptTokens += day.promptTokens || 0;
      stats.totalCompletionTokens += day.completionTokens || 0;
      stats.totalCachedTokens += day.cachedTokens || 0;
      stats.totalCost += day.cost || 0;

      for (const [prov, p] of Object.entries(day.byProvider || {})) {
        if (!stats.byProvider[prov])
          stats.byProvider[prov] = {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cachedTokens: 0,
            cost: 0,
          };
        stats.byProvider[prov].requests += p.requests || 0;
        stats.byProvider[prov].promptTokens += p.promptTokens || 0;
        stats.byProvider[prov].completionTokens += p.completionTokens || 0;
        stats.byProvider[prov].cachedTokens += p.cachedTokens || 0;
        stats.byProvider[prov].cost += p.cost || 0;
      }

      for (const [mk, m] of Object.entries(day.byModel || {})) {
        const rawModel = m.rawModel || mk.split("|")[0] || mk;
        const provider = m.provider || mk.split("|")[1] || "";
        const statsKey = provider ? `${rawModel} (${provider})` : rawModel;
        const providerDisplayName = providerNodeNameMap[provider] || provider;
        if (!stats.byModel[statsKey]) {
          stats.byModel[statsKey] = {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cachedTokens: 0,
            cost: 0,
            rawModel,
            provider: providerDisplayName,
            lastUsed: dateKey,
          };
        }
        stats.byModel[statsKey].requests += m.requests || 0;
        stats.byModel[statsKey].promptTokens += m.promptTokens || 0;
        stats.byModel[statsKey].completionTokens += m.completionTokens || 0;
        stats.byModel[statsKey].cachedTokens += m.cachedTokens || 0;
        stats.byModel[statsKey].cost += m.cost || 0;
        if (dateKey > (stats.byModel[statsKey].lastUsed || ""))
          stats.byModel[statsKey].lastUsed = dateKey;
      }

      for (const [connId, a] of Object.entries(day.byAccount || {})) {
        const accountName =
          connectionMap[connId] || `Account ${connId.slice(0, 8)}...`;
        const rawModel = a.rawModel || "";
        const provider = a.provider || "";
        const providerDisplayName = providerNodeNameMap[provider] || provider;
        const accountKey = `${rawModel} (${provider} - ${accountName})`;
        if (!stats.byAccount[accountKey]) {
          stats.byAccount[accountKey] = {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cachedTokens: 0,
            cost: 0,
            rawModel,
            provider: providerDisplayName,
            connectionId: connId,
            accountName,
            lastUsed: dateKey,
          };
        }
        stats.byAccount[accountKey].requests += a.requests || 0;
        stats.byAccount[accountKey].promptTokens += a.promptTokens || 0;
        stats.byAccount[accountKey].completionTokens += a.completionTokens || 0;
        stats.byAccount[accountKey].cachedTokens += a.cachedTokens || 0;
        stats.byAccount[accountKey].cost += a.cost || 0;
        if (dateKey > (stats.byAccount[accountKey].lastUsed || ""))
          stats.byAccount[accountKey].lastUsed = dateKey;
      }

      for (const [akKey, ak] of Object.entries(day.byApiKey || {})) {
        const rawModel = ak.rawModel || "";
        const provider = ak.provider || "";
        const providerDisplayName = providerNodeNameMap[provider] || provider;
        const apiKeyVal = (ak.apiKey as string) || undefined;
        const keyInfo = apiKeyVal ? apiKeyMap[apiKeyVal] : null;
        const keyName =
          keyInfo?.name ||
          (apiKeyVal ? apiKeyVal.slice(0, 8) + "..." : "Local (No API Key)");
        const apiKeyMasked = maskApiKey(apiKeyVal);
        const apiKeyKey = apiKeyMasked || "local-no-key";
        if (!stats.byApiKey[akKey]) {
          stats.byApiKey[akKey] = {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cachedTokens: 0,
            cost: 0,
            rawModel,
            provider: providerDisplayName,
            apiKeyMasked,
            keyName,
            apiKeyKey,
            lastUsed: dateKey,
          };
        }
        stats.byApiKey[akKey].requests += ak.requests || 0;
        stats.byApiKey[akKey].promptTokens += ak.promptTokens || 0;
        stats.byApiKey[akKey].completionTokens += ak.completionTokens || 0;
        stats.byApiKey[akKey].cachedTokens += ak.cachedTokens || 0;
        stats.byApiKey[akKey].cost += ak.cost || 0;
        if (dateKey > (stats.byApiKey[akKey].lastUsed || ""))
          stats.byApiKey[akKey].lastUsed = dateKey;
      }

      for (const [epKey, ep] of Object.entries(day.byEndpoint || {})) {
        const endpoint = ep.endpoint || epKey.split("|")[0] || "Unknown";
        const rawModel = ep.rawModel || "";
        const provider = ep.provider || "";
        const providerDisplayName = providerNodeNameMap[provider] || provider;
        if (!stats.byEndpoint[epKey]) {
          stats.byEndpoint[epKey] = {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cachedTokens: 0,
            cost: 0,
            endpoint,
            rawModel,
            provider: providerDisplayName,
            lastUsed: dateKey,
          };
        }
        stats.byEndpoint[epKey].requests += ep.requests || 0;
        stats.byEndpoint[epKey].promptTokens += ep.promptTokens || 0;
        stats.byEndpoint[epKey].completionTokens += ep.completionTokens || 0;
        stats.byEndpoint[epKey].cachedTokens += ep.cachedTokens || 0;
        stats.byEndpoint[epKey].cost += ep.cost || 0;
        if (dateKey > (stats.byEndpoint[epKey].lastUsed || ""))
          stats.byEndpoint[epKey].lastUsed = dateKey;
      }
    }

    // Overlay precise lastUsed timestamps from history
    const overlayCutoff = maxDays ? Date.now() - maxDays * 86400000 : 0;
    const histRows = db.all<{
      timestamp: string;
      provider?: string;
      model?: string;
      connectionId?: string | null;
      apiKey?: string | null;
      endpoint?: string | null;
    }>(
      `SELECT timestamp, provider, model, connectionId, apiKey, endpoint FROM usageHistory WHERE timestamp >= ?`,
      [new Date(overlayCutoff).toISOString()],
    );
    for (const e of histRows) {
      const ts = e.timestamp;
      const modelKey = e.provider ? `${e.model} (${e.provider})` : (e.model || "");
      if (
        stats.byModel[modelKey] &&
        new Date(ts).getTime() > new Date(stats.byModel[modelKey].lastUsed || 0).getTime()
      )
        stats.byModel[modelKey].lastUsed = ts;

      if (e.connectionId) {
        const accountName =
          connectionMap[e.connectionId] ||
          `Account ${e.connectionId.slice(0, 8)}...`;
        const accountKey = `${e.model} (${e.provider} - ${accountName})`;
        if (
          stats.byAccount[accountKey] &&
          new Date(ts).getTime() > new Date(stats.byAccount[accountKey].lastUsed || 0).getTime()
        )
          stats.byAccount[accountKey].lastUsed = ts;
      }

      const apiKeyKey =
        e.apiKey && typeof e.apiKey === "string"
          ? `${e.apiKey}|${e.model}|${e.provider || "unknown"}`
          : "local-no-key";
      if (
        stats.byApiKey[apiKeyKey] &&
        new Date(ts).getTime() > new Date(stats.byApiKey[apiKeyKey].lastUsed || 0).getTime()
      )
        stats.byApiKey[apiKeyKey].lastUsed = ts;

      const endpoint = e.endpoint || "Unknown";
      const endpointKey = `${endpoint}|${e.model}|${e.provider || "unknown"}`;
      if (
        stats.byEndpoint[endpointKey] &&
        new Date(ts).getTime() > new Date(stats.byEndpoint[endpointKey].lastUsed || 0).getTime()
      )
        stats.byEndpoint[endpointKey].lastUsed = ts;
    }
  } else {
    // 24h / today: live history
    let cutoff: string;
    if (period === "today") {
      const startOfDay = new Date();
      startOfDay.setHours(0, 0, 0, 0);
      cutoff = startOfDay.toISOString();
    } else {
      cutoff = new Date(Date.now() - (PERIOD_MS["24h"] || 86400000)).toISOString();
    }
    const filtered = db.all<{
      timestamp: string;
      provider?: string;
      model?: string;
      connectionId?: string | null;
      apiKey?: string | null;
      endpoint?: string | null;
      promptTokens: number;
      completionTokens: number;
      cost: number;
      tokens?: string;
    }>(
      `SELECT timestamp, provider, model, connectionId, apiKey, endpoint, promptTokens, completionTokens, cost, tokens FROM usageHistory WHERE timestamp >= ?`,
      [cutoff],
    );

    for (const r of filtered) {
      const tokens = parseJson<{ prompt_tokens?: number; completion_tokens?: number; cached_tokens?: number; cache_read_input_tokens?: number }>(r.tokens, {}) || {};
      const promptTokens = tokens.prompt_tokens || 0;
      const completionTokens = tokens.completion_tokens || 0;
      const cachedTokens =
        tokens.cached_tokens || tokens.cache_read_input_tokens || 0;
      const entryCost = r.cost || 0;
      const providerDisplayName = (r.provider && providerNodeNameMap[r.provider]) || r.provider || "";

      stats.totalPromptTokens += promptTokens;
      stats.totalCompletionTokens += completionTokens;
      stats.totalCachedTokens += cachedTokens;
      stats.totalCost += entryCost;

      const provKey = r.provider || "unknown";
      if (!stats.byProvider[provKey])
        stats.byProvider[provKey] = {
          requests: 0,
          promptTokens: 0,
          completionTokens: 0,
          cachedTokens: 0,
          cost: 0,
        };
      stats.byProvider[provKey].requests++;
      stats.byProvider[provKey].promptTokens += promptTokens;
      stats.byProvider[provKey].completionTokens += completionTokens;
      stats.byProvider[provKey].cachedTokens += cachedTokens;
      stats.byProvider[provKey].cost += entryCost;

      const modelKey = r.provider ? `${r.model} (${r.provider})` : (r.model || "unknown");
      if (!stats.byModel[modelKey]) {
        stats.byModel[modelKey] = {
          requests: 0,
          promptTokens: 0,
          completionTokens: 0,
          cachedTokens: 0,
          cost: 0,
          rawModel: r.model || "",
          provider: providerDisplayName,
          lastUsed: r.timestamp,
        };
      }
      stats.byModel[modelKey].requests++;
      stats.byModel[modelKey].promptTokens += promptTokens;
      stats.byModel[modelKey].completionTokens += completionTokens;
      stats.byModel[modelKey].cachedTokens += cachedTokens;
      stats.byModel[modelKey].cost += entryCost;
      if (new Date(r.timestamp).getTime() > new Date(stats.byModel[modelKey].lastUsed || 0).getTime())
        stats.byModel[modelKey].lastUsed = r.timestamp;

      if (r.connectionId) {
        const accountName =
          connectionMap[r.connectionId] ||
          `Account ${r.connectionId.slice(0, 8)}...`;
        const accountKey = `${r.model} (${r.provider} - ${accountName})`;
        if (!stats.byAccount[accountKey]) {
          stats.byAccount[accountKey] = {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cachedTokens: 0,
            cost: 0,
            rawModel: r.model || "",
            provider: providerDisplayName,
            connectionId: r.connectionId,
            accountName,
            lastUsed: r.timestamp,
          };
        }
        stats.byAccount[accountKey].requests++;
        stats.byAccount[accountKey].promptTokens += promptTokens;
        stats.byAccount[accountKey].completionTokens += completionTokens;
        stats.byAccount[accountKey].cachedTokens += cachedTokens;
        stats.byAccount[accountKey].cost += entryCost;
        if (
          new Date(r.timestamp).getTime() > new Date(stats.byAccount[accountKey].lastUsed || 0).getTime()
        )
          stats.byAccount[accountKey].lastUsed = r.timestamp;
      }

      if (r.apiKey && typeof r.apiKey === "string") {
        const keyInfo = apiKeyMap[r.apiKey];
        const keyName = keyInfo?.name || r.apiKey.slice(0, 8) + "...";
        const apiKeyMasked = maskApiKey(r.apiKey);
        const akKey = `${apiKeyMasked}|${r.model}|${r.provider || "unknown"}`;
        if (!stats.byApiKey[akKey]) {
          stats.byApiKey[akKey] = {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cachedTokens: 0,
            cost: 0,
            rawModel: r.model || "",
            provider: providerDisplayName,
            apiKeyMasked,
            keyName,
            apiKeyKey: apiKeyMasked,
            lastUsed: r.timestamp,
          };
        }
        const ake = stats.byApiKey[akKey];
        ake.requests++;
        ake.promptTokens += promptTokens;
        ake.completionTokens += completionTokens;
        ake.cachedTokens += cachedTokens;
        ake.cost += entryCost;
        if (new Date(r.timestamp).getTime() > new Date(ake.lastUsed || 0).getTime())
          ake.lastUsed = r.timestamp;
      } else {
        if (!stats.byApiKey["local-no-key"]) {
          stats.byApiKey["local-no-key"] = {
            requests: 0,
            promptTokens: 0,
            completionTokens: 0,
            cachedTokens: 0,
            cost: 0,
            rawModel: r.model || "",
            provider: providerDisplayName,
            apiKeyMasked: null,
            keyName: "Local (No API Key)",
            apiKeyKey: "local-no-key",
            lastUsed: r.timestamp,
          };
        }
        const ake = stats.byApiKey["local-no-key"];
        ake.requests++;
        ake.promptTokens += promptTokens;
        ake.completionTokens += completionTokens;
        ake.cachedTokens += cachedTokens;
        ake.cost += entryCost;
        if (new Date(r.timestamp).getTime() > new Date(ake.lastUsed || 0).getTime())
          ake.lastUsed = r.timestamp;
      }

      const endpoint = r.endpoint || "Unknown";
      const epKey = `${endpoint}|${r.model}|${r.provider || "unknown"}`;
      if (!stats.byEndpoint[epKey]) {
        stats.byEndpoint[epKey] = {
          requests: 0,
          promptTokens: 0,
          completionTokens: 0,
          cachedTokens: 0,
          cost: 0,
          endpoint,
          rawModel: r.model || "",
          provider: providerDisplayName,
          lastUsed: r.timestamp,
        };
      }
      const epe = stats.byEndpoint[epKey];
      epe.requests++;
      epe.promptTokens += promptTokens;
      epe.completionTokens += completionTokens;
      epe.cachedTokens += cachedTokens;
      epe.cost += entryCost;
      if (new Date(r.timestamp).getTime() > new Date(epe.lastUsed || 0).getTime())
        epe.lastUsed = r.timestamp;
    }
  }

  stats.totalRequests = Object.values(stats.byProvider).reduce(
    (sum, p) => sum + (p.requests || 0),
    0,
  );
  return stats;
}

export async function getChartData(period = "7d") {
  const db = await getAdapter();
  const now = Date.now();

  if (period === "today") {
    const bucketCount = 24;
    const bucketMs = 3600000;
    const startOfDay = new Date();
    startOfDay.setHours(0, 0, 0, 0);
    const startTime = startOfDay.getTime();
    const endTime = startTime + bucketCount * bucketMs;
    const labelFn = (ts: number) =>
      new Date(ts).toLocaleTimeString("en-US", {
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
      });
    const buckets = Array.from({ length: bucketCount }, (_, i) => ({
      label: labelFn(startTime + i * bucketMs),
      tokens: 0,
      cost: 0,
    }));

    const rows = db.all<{ timestamp: string; promptTokens?: number; completionTokens?: number; cost?: number }>(
      `SELECT timestamp, promptTokens, completionTokens, cost FROM usageHistory WHERE timestamp >= ?`,
      [new Date(startTime).toISOString()],
    );
    for (const r of rows) {
      const t = new Date(r.timestamp).getTime();
      if (t < startTime || t >= endTime) continue;
      const idx = Math.floor((t - startTime) / bucketMs);
      if (idx >= 0 && idx < bucketCount) {
        buckets[idx].tokens +=
          (r.promptTokens || 0) + (r.completionTokens || 0);
        buckets[idx].cost += r.cost || 0;
      }
    }
    return buckets;
  }

  if (period === "24h") {
    const bucketCount = 24;
    const bucketMs = 3600000;
    const labelFn = (ts: number) =>
      new Date(ts).toLocaleTimeString("en-US", {
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
      });
    const startTime = now - bucketCount * bucketMs;
    const buckets = Array.from({ length: bucketCount }, (_, i) => ({
      label: labelFn(startTime + i * bucketMs),
      tokens: 0,
      cost: 0,
    }));

    const rows = db.all<{ timestamp: string; promptTokens?: number; completionTokens?: number; cost?: number }>(
      `SELECT timestamp, promptTokens, completionTokens, cost FROM usageHistory WHERE timestamp >= ?`,
      [new Date(startTime).toISOString()],
    );
    for (const r of rows) {
      const t = new Date(r.timestamp).getTime();
      if (t < startTime || t > now) continue;
      const idx = Math.min(
        Math.floor((t - startTime) / bucketMs),
        bucketCount - 1,
      );
      buckets[idx].tokens += (r.promptTokens || 0) + (r.completionTokens || 0);
      buckets[idx].cost += r.cost || 0;
    }
    return buckets;
  }

  const bucketCount = period === "7d" ? 7 : period === "30d" ? 30 : 60;
  const today = new Date();
  const labelFn = (d: Date) =>
    d.toLocaleDateString("en-US", { month: "short", day: "numeric" });

  // Build map of dateKey → day data
  const dayRows = loadDaysInRange(db, bucketCount);
  const dayMap: Record<string, DayUsageAggregate> = {};
  for (const r of dayRows) dayMap[r.dateKey] = parseJson<DayUsageAggregate>(r.data, {}) || {};

  return Array.from({ length: bucketCount }, (_, i) => {
    const d = new Date(today);
    d.setDate(d.getDate() - (bucketCount - 1 - i));
    const dateKey = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
    const dayData = dayMap[dateKey];
    return {
      label: labelFn(d),
      tokens: dayData
        ? (dayData.promptTokens || 0) + (dayData.completionTokens || 0)
        : 0,
      cost: dayData ? dayData.cost || 0 : 0,
    };
  });
}

function formatLogDate(date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(date.getDate())}-${pad(date.getMonth() + 1)}-${date.getFullYear()} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

// No-op: request log is now derived from usageHistory table on read.
export async function appendRequestLog(): Promise<void> {}

export async function getRecentLogs(limit = 200): Promise<string[]> {
  try {
    const db = await getAdapter();
    const rows = db.all<{
      timestamp: string;
      provider?: string | null;
      model?: string | null;
      connectionId?: string | null;
      promptTokens?: number | null;
      completionTokens?: number | null;
      status?: string | null;
      tokens?: string | null;
    }>(
      `SELECT timestamp, provider, model, connectionId, promptTokens, completionTokens, status, tokens FROM usageHistory ORDER BY id DESC LIMIT ?`,
      [limit],
    );
    if (!rows.length) return [];

    const connMap: Record<string, string> = {};
    try {
      const { getProviderConnections } = await import("./connectionsRepo");
      const connections = await getProviderConnections();
      for (const c of connections) connMap[c.id] = c.name || c.email || "";
    } catch {}

    return rows.map((r) => {
      const ts = formatLogDate(new Date(r.timestamp));
      const p = r.provider?.toUpperCase() || "-";
      const m = r.model || "-";
      const account =
        (r.connectionId && connMap[r.connectionId]) ||
        (r.connectionId ? r.connectionId.slice(0, 8) : "-");
      const tk = r.tokens ? parseJson<{ prompt_tokens?: number; completion_tokens?: number }>(r.tokens, {}) : {};
      const sent = r.promptTokens ?? tk?.prompt_tokens ?? "-";
      const received = r.completionTokens ?? tk?.completion_tokens ?? "-";
      return `${ts} | ${m} | ${p} | ${account} | ${sent} | ${received} | ${r.status || "-"}`;
    });
  } catch (e: unknown) {
    const err = e as Error;
    console.error("[usageRepo] getRecentLogs failed:", err.message);
    return [];
  }
}
