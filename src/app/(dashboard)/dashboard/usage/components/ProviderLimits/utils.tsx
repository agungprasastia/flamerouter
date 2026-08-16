import { getModelsByProviderId } from "@/shared/constants/providerModels";

// ─── Shared types ────────────────────────────────────────────────────────────
export type QuotaEntry = {
  name?: string;
  modelKey?: string;
  used?: number;
  total?: number;
  remaining?: number;
  remainingPercentage?: number;
  resetAt?: string | null;
  unit?: string;
  recurring?: boolean;
  message?: string;
  [key: string]: unknown;
};

export type ConnectionItem = {
  id: string;
  name?: string;
  email?: string;
  displayName?: string;
  provider?: string;
  isActive?: boolean;
  authType?: string;
  providerSpecificData?: Record<string, unknown>;
  [key: string]: unknown;
};

export type QuotaData = Record<string, { quotas?: QuotaEntry[] }>;

export type Pagination = {
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
};

export type Totals = {
  eligibleConnections: number;
  providerFilteredConnections: number;
};

// ─── Constants ───────────────────────────────────────────────────────────────
export const QUOTA_CACHE_KEY = "quotaCacheData";
export const REFRESH_INTERVAL_MS = 60000;
// Claude usage/quota endpoint rate-limits; poll it less often than other providers
export const CLAUDE_REFRESH_INTERVAL_MS = 180000;
export const DEPLETED_QUOTA_THRESHOLD = 5;
export const AUTO_REFRESH_STORAGE_KEY = "quotaAutoRefresh";
export const CONNECTIONS_PAGE_SIZE = 20;
export const ACCOUNT_PAGE_SIZE_OPTIONS = [10, 20, 50, 100];
export const ACCOUNT_PAGE_SIZE_MAX = 500;
export const ACCOUNT_FILTER_OPTIONS = [
  { value: "all", label: "All accounts" },
  { value: "active", label: "Active" },
  { value: "inactive", label: "Turned off" },
];
export const QUOTA_SORT_OPTIONS = [
  { value: "default", label: "Default quota order" },
  { value: "remaining-asc", label: "% quota: low to high" },
  { value: "remaining-desc", label: "% quota: high to low" },
];

// ─── Pure helpers ─────────────────────────────────────────────────────────────
export function getConnectionLabel(connection: ConnectionItem) {
  return (
    connection.name?.trim() ||
    connection.email?.trim() ||
    connection.displayName?.trim() ||
    null
  );
}

export function getConnectionQuotaRemaining(connection: ConnectionItem, quotaData: QuotaData) {
  const quota = quotaData[connection.id]?.quotas?.[0];
  if (!quota) return Number.POSITIVE_INFINITY;
  if (typeof quota.remaining === "number") return quota.remaining;
  return Number.POSITIVE_INFINITY;
}

export function sortVisibleConnections(
  connections: ConnectionItem[],
  quotaData: QuotaData,
  expiringFirst: boolean,
  providerFilter: string,
  quotaSortMode: string,
) {
  if (providerFilter === "codex" && quotaSortMode !== "default") {
    return [...connections].sort((a, b) => {
      const remainingA = getConnectionQuotaRemaining(a, quotaData);
      const remainingB = getConnectionQuotaRemaining(b, quotaData);
      const remainingDiff =
        quotaSortMode === "remaining-asc"
          ? remainingA - remainingB
          : remainingB - remainingA;
      if (remainingDiff !== 0) return remainingDiff;
      return (getConnectionLabel(a) || "").localeCompare(
        getConnectionLabel(b) || "",
      );
    });
  }

  if (!expiringFirst) return connections;

  const getEarliestResetTime = (connection: ConnectionItem) => {
    const resetTimes = (quotaData[connection.id]?.quotas || [])
      .map((quota: QuotaEntry) =>
        quota.resetAt
          ? new Date(quota.resetAt).getTime()
          : Number.POSITIVE_INFINITY,
      )
      .filter((time: number) => Number.isFinite(time));
    return resetTimes.length > 0
      ? Math.min(...resetTimes)
      : Number.POSITIVE_INFINITY;
  };

  return [...connections].sort((a, b) => {
    const expiryDiff = getEarliestResetTime(a) - getEarliestResetTime(b);
    if (expiryDiff !== 0) return expiryDiff;
    return (
      (a.provider || "").localeCompare(b.provider || "") ||
      (getConnectionLabel(a) || "").localeCompare(getConnectionLabel(b) || "")
    );
  });
}

export function buildLoadingState(connections: ConnectionItem[]) {
  const nextLoadingState: Record<string, boolean> = {};
  connections.forEach((connection: ConnectionItem) => {
    nextLoadingState[connection.id] = true;
  });
  return nextLoadingState;
}

export function filterQuotaStateByConnections<T extends Record<string, unknown>>(state: T, connections: ConnectionItem[]): T {
  const visibleIds = new Set(connections.map((connection: ConnectionItem) => connection.id));
  return Object.fromEntries(
    Object.entries(state).filter(([id]) => visibleIds.has(id)),
  ) as T;
}

export function getConnectionsPageRange(pagination: Pagination) {
  if (!pagination.total) {
    return { start: 0, end: 0 };
  }
  const start = (pagination.page - 1) * pagination.pageSize + 1;
  const end = Math.min(pagination.page * pagination.pageSize, pagination.total);
  return { start, end };
}

export function getConnectionsEmptyMessage(
  totals: Totals,
  providerFilter: string,
  accountFilter: string,
) {
  if (!totals.eligibleConnections) {
    return {
      icon: "cloud_off",
      title: "No Providers Connected",
      description:
        "Connect to providers with OAuth to track your API quota limits and usage.",
    };
  }
  if (!totals.providerFilteredConnections) {
    return {
      icon: "filter_alt_off",
      title: "No Accounts Match Current Filters",
      description:
        providerFilter === "all"
          ? "Try changing the account status filter to see more quota trackers."
          : `No ${accountFilter === "inactive" ? "turned off" : accountFilter === "active" ? "active" : "matching"} accounts found for ${providerFilter}.`,
    };
  }
  return {
    icon: "filter_alt_off",
    title: "No Accounts On This Page",
    description:
      "Try moving to another page or refreshing the current filters.",
  };
}

export function sortRequestFromExpiringFirst(expiringFirst: boolean) {
  return expiringFirst ? "expiring" : "priority";
}

export function getPageSizeLabel(pageSize: number, isCustomPageSize: boolean) {
  return isCustomPageSize ? `Custom: ${pageSize} / page` : `${pageSize} / page`;
}

export function getConnectionsPaginationSummary(pagination: Pagination) {
  const { start, end } = getConnectionsPageRange(pagination);
  return `Showing ${start}-${end} of ${pagination.total}`;
}

export function getSafePagination(pagination: Pagination | null | undefined, fallbackPageSize: number): Pagination {
  return (
    pagination || {
      page: 1,
      pageSize: fallbackPageSize,
      total: 0,
      totalPages: 1,
    }
  );
}

export function getSafeTotals(totals: Totals | null | undefined, fallbackTotal = 0): Totals {
  return (
    totals || {
      eligibleConnections: fallbackTotal,
      providerFilteredConnections: fallbackTotal,
    }
  );
}

export function shouldResetPage(previousValue: unknown, nextValue: unknown) {
  return previousValue !== nextValue;
}

export function getPaginationPageValue(dataPagination: { page?: number } | null | undefined, fallbackPage: number) {
  return dataPagination?.page || fallbackPage;
}

export function getProviderOptions(dataProviderOptions: unknown[] | null | undefined) {
  return dataProviderOptions || [];
}

export async function reconcileConnectionsPage(
  fetchConnections: (page: number) => Promise<unknown>,
  targetPage: number,
) {
  return await fetchConnections(targetPage);
}

export function getQuotaCache() {
  if (typeof window === "undefined") return {};
  try {
    const cached = window.localStorage.getItem(QUOTA_CACHE_KEY);
    return cached ? JSON.parse(cached) : {};
  } catch (error) {
    console.error("Error reading quota cache:", error);
    return {};
  }
}

export function setQuotaCache(connectionId: string, quotaEntry: QuotaEntry) {
  if (typeof window === "undefined") return;
  try {
    const cache = getQuotaCache();
    cache[connectionId] = {
      ...quotaEntry,
      cachedAt: new Date().toISOString(),
    };
    window.localStorage.setItem(QUOTA_CACHE_KEY, JSON.stringify(cache));
  } catch (error) {
    console.error("Error writing quota cache:", error);
  }
}

/**
 * Format ISO date string to countdown format (inspired by vscode-antigravity-cockpit)
 * @param {string|Date} date - ISO date string or Date object
 * @returns {string} Formatted countdown (e.g., "2d 5h 30m", "4h 40m", "15m") or "-"
 */
export function formatResetTime(date: string | number | Date | null | undefined) {
  if (!date) return "-";

  try {
    const resetDate = date instanceof Date ? date : new Date(date);
    const now = new Date();
    const diffMs = resetDate.getTime() - now.getTime();

    if (diffMs <= 0) return "-";

    const totalMinutes = Math.ceil(diffMs / (1000 * 60));

    // < 60 minutes: show only minutes
    if (totalMinutes < 60) {
      return `${totalMinutes}m`;
    }

    const totalHours = Math.floor(totalMinutes / 60);
    const remainingMinutes = totalMinutes % 60;

    // < 24 hours: show hours and minutes
    if (totalHours < 24) {
      return `${totalHours}h ${remainingMinutes}m`;
    }

    // >= 24 hours: show days, hours, and minutes
    const days = Math.floor(totalHours / 24);
    const remainingHours = totalHours % 24;
    return `${days}d ${remainingHours}h ${remainingMinutes}m`;
  } catch (error) {
    return "-";
  }
}

/**
 * Get Tailwind color class based on percentage
 * @param {number} percentage - Remaining percentage (0-100)
 * @returns {string} Color name: "green" | "yellow" | "red"
 */
export function getStatusColor(percentage: number) {
  if (percentage > 70) return "green";
  if (percentage >= 30) return "yellow";
  return "red"; // 0-29% including 0% (out of quota) - show red
}

/**
 * Get status emoji based on percentage
 * @param {number} percentage - Remaining percentage (0-100)
 * @returns {string} Emoji: "🟢" | "🟡" | "🔴"
 */
export function getStatusEmoji(percentage: number) {
  if (percentage > 70) return "🟢";
  if (percentage >= 30) return "🟡";
  return "🔴"; // 0-29% including 0% (out of quota) - show red
}

/**
 * Calculate remaining percentage
 * @param {number} used - Used amount
 * @param {number} total - Total amount
 * @returns {number} Remaining percentage (0-100)
 */
export function calculatePercentage(used: number, total: number) {
  if (!total || total === 0) return 0;
  if (!used || used < 0) return 100;
  if (used >= total) return 0;

  return Math.round(((total - used) / total) * 100);
}

/**
 * Get remaining percentage from a normalized quota row
 * @param {Object} quota - Normalized quota object
 * @returns {number} Remaining percentage (0-100)
 */
export function getRemainingPercentage(quota: QuotaEntry | null | undefined) {
  if (quota?.remaining !== undefined) {
    return Math.max(0, Math.round(quota.remaining));
  }

  if (quota?.remainingPercentage !== undefined) {
    return Math.round(quota.remainingPercentage);
  }

  return calculatePercentage(quota?.used ?? 0, quota?.total ?? 0);
}

export function getQuotaVisibilityKey(quota: QuotaEntry | null | undefined) {
  if (!quota || typeof quota !== "object") return "";
  return String(quota.modelKey || quota.name || "").trim();
}

function getProviderHiddenQuotaSet(
  provider: string,
  quotaVisibility: Record<string, { hidden?: unknown[] }>,
) {
  const hidden = quotaVisibility?.[provider]?.hidden;
  return new Set(Array.isArray(hidden) ? hidden.map(String) : []);
}

export function filterQuotasByVisibility(
  provider: string,
  quotas: QuotaEntry[] = [],
  quotaVisibility: Record<string, { hidden?: unknown[] }> = {},
) {
  if (!Array.isArray(quotas) || quotas.length === 0) return [];
  const hidden = getProviderHiddenQuotaSet(provider, quotaVisibility);
  if (hidden.size === 0) return quotas;
  return quotas.filter((quota) => !hidden.has(getQuotaVisibilityKey(quota)));
}

export function getHiddenQuotaRows(
  provider: string,
  quotas: QuotaEntry[] = [],
  quotaVisibility: Record<string, { hidden?: unknown[] }> = {},
) {
  if (!Array.isArray(quotas) || quotas.length === 0) return [];
  const hidden = getProviderHiddenQuotaSet(provider, quotaVisibility);
  if (hidden.size === 0) return [];
  return quotas.filter((quota) => hidden.has(getQuotaVisibilityKey(quota)));
}

/**
 * Parse provider-specific quota structures into normalized array
 * @param {string} provider - Provider name (github, antigravity, codex, kiro, claude)
 * @param {Object} data - Raw quota data from provider
 * @returns {Array<Object>} Normalized quota objects with { name, used, total, resetAt }
 */
export function parseQuotaData(provider: string, data: Record<string, unknown> | null | undefined): QuotaEntry[] {
  if (!data || typeof data !== "object") return [];

  const normalizedQuotas: QuotaEntry[] = [];

  try {
    switch (provider.toLowerCase()) {
      case "github":
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
            });
          });
        }
        break;

      case "antigravity":
        if (data.quotas) {
          Object.entries(data.quotas).forEach(([modelKey, quota]) => {
            normalizedQuotas.push({
              name: quota.displayName || modelKey,
              modelKey: modelKey, // Keep modelKey for sorting
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
              remainingPercentage: quota.remainingPercentage,
            });
          });
        }
        break;

      case "codex":
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([quotaType, quota]) => {
            normalizedQuotas.push({
              name: quotaType,
              used: quota.used || 0,
              total: quota.total || 0,
              remaining: quota.remaining,
              resetAt: quota.resetAt || null,
            });
          });
        }
        break;

      case "kiro":
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([quotaType, quota]) => {
            normalizedQuotas.push({
              name: quotaType,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
            });
          });
        }
        break;

      case "qoder":
        // Qoder ships a `user` quota and (optionally) an `organization`
        // quota, both with same shape: {total, used, remaining, unit, resetAt}.
        // Skip an organization bucket when its total is 0 — most personal
        // Qoder accounts won't have one and rendering "0/0" is misleading.
        // Don't forward Qoder's `remaining` field: it's an absolute credit
        // count, but getRemainingPercentage / QuotaTable interpret
        // `remaining` as a 0-100 percentage and would render 348 credits
        // as "348%". The percentage is computed from used/total instead.
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([quotaType, quota]) => {
            if (
              quotaType === "organization" &&
              (!quota || (Number(quota.total) || 0) === 0)
            ) {
              return;
            }
            normalizedQuotas.push({
              name:
                quotaType === "user"
                  ? "Personal"
                  : quotaType === "organization"
                    ? "Organization"
                    : quotaType,
              used: quota.used || 0,
              total: quota.total || 0,
              unit: quota.unit,
              resetAt: quota.resetAt || null,
            });
          });
        }
        break;

      case "claude":
        if (data.message) {
          // Handle error message case
          normalizedQuotas.push({
            name: "error",
            used: 0,
            total: 0,
            resetAt: null,
            message: data.message as string | undefined,
          });
        } else if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
            });
          });
        }
        break;

      case "vercel-ai-gateway":
        // Vercel returns currency credit balance, not request quotas.
        // The 'Remaining (USD)' row needs explicit remainingPercentage because
        // its used/total values would otherwise compute the wrong direction
        // (e.g. used=95.5 / total=100 → 4% instead of 96%).
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
              remainingPercentage: quota.remainingPercentage,
            });
          });
        }
        break;

      case "codebuddy-cn":
        // CodeBuddy CN mixes recurring refill packs ("Monthly"/"Weekly"/...)
        // with one-shot bonus packs ("Bonus Pack N"). Forward `recurring`
        // so the UI can show "Expires in" for bonus packs (whose resetAt is
        // a hard expiry, not a refresh) instead of "Reset in".
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
              recurring: quota.recurring !== false,
            });
          });
        }
        break;

      case "grok-cli":
        // Grok Build credits (on-demand window + prepaid balance).
        // Do NOT forward absolute `remaining` — getRemainingPercentage treats
        // it as a 0–100 percentage (same as Qoder). Use remainingPercentage.
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
              remainingPercentage: quota.remainingPercentage,
            });
          });
        }
        break;

      case "kimi":
        // Weekly / Ratelimit from /v1/usages. Prefer remainingPercentage only.
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
              remainingPercentage: quota.remainingPercentage,
            });
          });
        }
        break;

      case "deepseek":
        // Credit balance — remainingPercentage only (no absolute remaining).
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
              remainingPercentage: quota.remainingPercentage,
            });
          });
        }
        break;

      case "ollama":
        // Session (5h) / Weekly (7d) usage % from ollama.com/api/usage.
        // remainingPercentage only — no absolute remaining (UI treats remaining as %).
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
              remainingPercentage: quota.remainingPercentage,
            });
          });
        }
        break;

      default:
        // Generic fallback for unknown providers
        if (data.quotas) {
          Object.entries(data.quotas as Record<string, QuotaEntry>).forEach(([name, quota]) => {
            normalizedQuotas.push({
              name,
              used: quota.used || 0,
              total: quota.total || 0,
              resetAt: quota.resetAt || null,
            });
          });
        }
    }
  } catch (error) {
    console.error(`Error parsing quota data for ${provider}:`, error);
    return [];
  }

  // Sort quotas according to PROVIDER_MODELS order
  const modelOrder = getModelsByProviderId(provider);
  if (modelOrder.length > 0) {
    const orderMap = new Map(modelOrder.map((m, i) => [m.id, i]));

    normalizedQuotas.sort((a, b) => {
      // Use modelKey for antigravity, otherwise use name
      const keyA = a.modelKey || a.name || "";
      const keyB = b.modelKey || b.name || "";
      const orderA = orderMap.get(keyA) ?? 999;
      const orderB = orderMap.get(keyB) ?? 999;
      return orderA - orderB;
    });
  }

  return normalizedQuotas;
}
