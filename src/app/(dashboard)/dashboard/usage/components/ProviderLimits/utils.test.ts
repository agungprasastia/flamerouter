import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  QUOTA_CACHE_KEY,
  REFRESH_INTERVAL_MS,
  CLAUDE_REFRESH_INTERVAL_MS,
  DEPLETED_QUOTA_THRESHOLD,
  AUTO_REFRESH_STORAGE_KEY,
  CONNECTIONS_PAGE_SIZE,
  ACCOUNT_PAGE_SIZE_OPTIONS,
  ACCOUNT_PAGE_SIZE_MAX,
  ACCOUNT_FILTER_OPTIONS,
  QUOTA_SORT_OPTIONS,
  getConnectionLabel,
  getConnectionQuotaRemaining,
  sortVisibleConnections,
  buildLoadingState,
  filterQuotaStateByConnections,
  getConnectionsPageRange,
  getConnectionsEmptyMessage,
  sortRequestFromExpiringFirst,
  getPageSizeLabel,
  getConnectionsPaginationSummary,
  getSafePagination,
  getSafeTotals,
  shouldResetPage,
  getPaginationPageValue,
  getProviderOptions,
  reconcileConnectionsPage,
  getQuotaCache,
  setQuotaCache,
  formatResetTime,
  getStatusColor,
  getStatusEmoji,
  calculatePercentage,
  getRemainingPercentage,
  getQuotaVisibilityKey,
  filterQuotasByVisibility,
  getHiddenQuotaRows,
  parseQuotaData,
  ConnectionItem,
  QuotaData,
  QuotaEntry,
  Pagination,
  Totals,
} from "./utils";

describe("ProviderLimits / utils", () => {
  describe("Constants", () => {
    it("should export expected constants", () => {
      expect(QUOTA_CACHE_KEY).toBe("quotaCacheData");
      expect(REFRESH_INTERVAL_MS).toBe(60000);
      expect(CLAUDE_REFRESH_INTERVAL_MS).toBe(180000);
      expect(DEPLETED_QUOTA_THRESHOLD).toBe(5);
      expect(AUTO_REFRESH_STORAGE_KEY).toBe("quotaAutoRefresh");
      expect(CONNECTIONS_PAGE_SIZE).toBe(20);
      expect(ACCOUNT_PAGE_SIZE_OPTIONS).toEqual([10, 20, 50, 100]);
      expect(ACCOUNT_PAGE_SIZE_MAX).toBe(500);
      expect(ACCOUNT_FILTER_OPTIONS).toHaveLength(3);
      expect(QUOTA_SORT_OPTIONS).toHaveLength(3);
    });
  });

  describe("getConnectionLabel", () => {
    it("should return name when present and non-empty", () => {
      const conn: ConnectionItem = { id: "1", name: "  My Conn  ", email: "user@test.com", displayName: "Display" };
      expect(getConnectionLabel(conn)).toBe("My Conn");
    });

    it("should return email when name is missing or whitespace", () => {
      const conn: ConnectionItem = { id: "1", name: "   ", email: "  user@test.com  ", displayName: "Display" };
      expect(getConnectionLabel(conn)).toBe("user@test.com");
    });

    it("should return displayName when name and email are missing or whitespace", () => {
      const conn: ConnectionItem = { id: "1", displayName: " Display Name " };
      expect(getConnectionLabel(conn)).toBe("Display Name");
    });

    it("should return null if no label properties exist or all are whitespace", () => {
      const conn: ConnectionItem = { id: "1", name: "  ", email: "", displayName: "  " };
      expect(getConnectionLabel(conn)).toBeNull();
    });
  });

  describe("getConnectionQuotaRemaining", () => {
    it("should return remaining value when available as a number", () => {
      const conn: ConnectionItem = { id: "c1" };
      const quotaData: QuotaData = {
        c1: { quotas: [{ remaining: 42 }] },
      };
      expect(getConnectionQuotaRemaining(conn, quotaData)).toBe(42);
    });

    it("should return POSITIVE_INFINITY if quota entry is missing or remaining is not a number", () => {
      const conn: ConnectionItem = { id: "c1" };
      expect(getConnectionQuotaRemaining(conn, {})).toBe(Number.POSITIVE_INFINITY);

      const quotaDataNoNum: QuotaData = {
        c1: { quotas: [{ name: "test" }] },
      };
      expect(getConnectionQuotaRemaining(conn, quotaDataNoNum)).toBe(Number.POSITIVE_INFINITY);
    });
  });

  describe("sortVisibleConnections", () => {
    const connA: ConnectionItem = { id: "a", name: "Alpha", provider: "github" };
    const connB: ConnectionItem = { id: "b", name: "Beta", provider: "antigravity" };

    it("should sort by codex quota remaining asc", () => {
      const quotaData: QuotaData = {
        a: { quotas: [{ remaining: 80 }] },
        b: { quotas: [{ remaining: 20 }] },
      };
      const sorted = sortVisibleConnections([connA, connB], quotaData, false, "codex", "remaining-asc");
      expect(sorted.map((c) => c.id)).toEqual(["b", "a"]);
    });

    it("should sort by codex quota remaining desc", () => {
      const quotaData: QuotaData = {
        a: { quotas: [{ remaining: 80 }] },
        b: { quotas: [{ remaining: 20 }] },
      };
      const sorted = sortVisibleConnections([connA, connB], quotaData, false, "codex", "remaining-desc");
      expect(sorted.map((c) => c.id)).toEqual(["a", "b"]);
    });

    it("should fallback to connection label tie-breaker for codex sorting", () => {
      const conn1: ConnectionItem = { id: "1", name: "Zeta" };
      const conn2: ConnectionItem = { id: "2", name: "Alpha" };
      const quotaData: QuotaData = {
        "1": { quotas: [{ remaining: 50 }] },
        "2": { quotas: [{ remaining: 50 }] },
      };
      const sorted = sortVisibleConnections([conn1, conn2], quotaData, false, "codex", "remaining-asc");
      expect(sorted.map((c) => c.id)).toEqual(["2", "1"]);
    });

    it("should return original connections array if expiringFirst is false and not codex sorting", () => {
      const conns = [connA, connB];
      const result = sortVisibleConnections(conns, {}, false, "all", "default");
      expect(result).toBe(conns);
    });

    it("should sort by earliest resetAt time when expiringFirst is true", () => {
      const now = Date.now();
      const quotaData: QuotaData = {
        a: { quotas: [{ resetAt: new Date(now + 100000).toISOString() }] },
        b: { quotas: [{ resetAt: new Date(now + 1000).toISOString() }] },
      };
      const sorted = sortVisibleConnections([connA, connB], quotaData, true, "all", "default");
      expect(sorted.map((c) => c.id)).toEqual(["b", "a"]);
    });

    it("should break ties in expiringFirst by provider and label", () => {
      const conn1: ConnectionItem = { id: "1", name: "B", provider: "github" };
      const conn2: ConnectionItem = { id: "2", name: "A", provider: "github" };
      const quotaData: QuotaData = {};
      const sorted = sortVisibleConnections([conn1, conn2], quotaData, true, "all", "default");
      expect(sorted.map((c) => c.id)).toEqual(["2", "1"]);
    });
  });

  describe("buildLoadingState", () => {
    it("should build map of connection IDs with true values", () => {
      const conns: ConnectionItem[] = [{ id: "1" }, { id: "2" }];
      expect(buildLoadingState(conns)).toEqual({ "1": true, "2": true });
    });
  });

  describe("filterQuotaStateByConnections", () => {
    it("should retain only quota state for visible connections", () => {
      const state = { "1": { data: 1 }, "2": { data: 2 }, "3": { data: 3 } };
      const conns: ConnectionItem[] = [{ id: "1" }, { id: "3" }];
      expect(filterQuotaStateByConnections(state, conns)).toEqual({
        "1": { data: 1 },
        "3": { data: 3 },
      });
    });
  });

  describe("getConnectionsPageRange", () => {
    it("should return 0, 0 when total is 0 or falsy", () => {
      expect(getConnectionsPageRange({ page: 1, pageSize: 20, total: 0, totalPages: 0 })).toEqual({ start: 0, end: 0 });
    });

    it("should calculate correct start and end range", () => {
      const pagination: Pagination = { page: 2, pageSize: 10, total: 25, totalPages: 3 };
      expect(getConnectionsPageRange(pagination)).toEqual({ start: 11, end: 20 });
    });

    it("should cap end range at total", () => {
      const pagination: Pagination = { page: 3, pageSize: 10, total: 25, totalPages: 3 };
      expect(getConnectionsPageRange(pagination)).toEqual({ start: 21, end: 25 });
    });
  });

  describe("getConnectionsEmptyMessage", () => {
    it("should return No Providers Connected when eligibleConnections is 0", () => {
      const totals: Totals = { eligibleConnections: 0, providerFilteredConnections: 0 };
      const msg = getConnectionsEmptyMessage(totals, "all", "all");
      expect(msg.title).toBe("No Providers Connected");
      expect(msg.icon).toBe("cloud_off");
    });

    it("should return No Accounts Match Current Filters when providerFilteredConnections is 0", () => {
      const totals: Totals = { eligibleConnections: 5, providerFilteredConnections: 0 };
      const msgAll = getConnectionsEmptyMessage(totals, "all", "all");
      expect(msgAll.title).toBe("No Accounts Match Current Filters");
      expect(msgAll.description).toContain("account status filter");

      const msgInactive = getConnectionsEmptyMessage(totals, "github", "inactive");
      expect(msgInactive.description).toContain("turned off");

      const msgActive = getConnectionsEmptyMessage(totals, "github", "active");
      expect(msgActive.description).toContain("active");
    });

    it("should return No Accounts On This Page when filters match but current page is empty", () => {
      const totals: Totals = { eligibleConnections: 5, providerFilteredConnections: 5 };
      const msg = getConnectionsEmptyMessage(totals, "github", "all");
      expect(msg.title).toBe("No Accounts On This Page");
    });
  });

  describe("Simple Helper Functions", () => {
    it("sortRequestFromExpiringFirst", () => {
      expect(sortRequestFromExpiringFirst(true)).toBe("expiring");
      expect(sortRequestFromExpiringFirst(false)).toBe("priority");
    });

    it("getPageSizeLabel", () => {
      expect(getPageSizeLabel(20, false)).toBe("20 / page");
      expect(getPageSizeLabel(15, true)).toBe("Custom: 15 / page");
    });

    it("getConnectionsPaginationSummary", () => {
      const pagination: Pagination = { page: 1, pageSize: 10, total: 25, totalPages: 3 };
      expect(getConnectionsPaginationSummary(pagination)).toBe("Showing 1-10 of 25");
    });

    it("getSafePagination", () => {
      expect(getSafePagination(null, 20)).toEqual({ page: 1, pageSize: 20, total: 0, totalPages: 1 });
      const custom: Pagination = { page: 2, pageSize: 10, total: 100, totalPages: 10 };
      expect(getSafePagination(custom, 20)).toBe(custom);
    });

    it("getSafeTotals", () => {
      expect(getSafeTotals(null)).toEqual({ eligibleConnections: 0, providerFilteredConnections: 0 });
      expect(getSafeTotals(undefined, 10)).toEqual({ eligibleConnections: 10, providerFilteredConnections: 10 });
      const totals: Totals = { eligibleConnections: 3, providerFilteredConnections: 2 };
      expect(getSafeTotals(totals)).toBe(totals);
    });

    it("shouldResetPage", () => {
      expect(shouldResetPage("a", "b")).toBe(true);
      expect(shouldResetPage("a", "a")).toBe(false);
    });

    it("getPaginationPageValue", () => {
      expect(getPaginationPageValue({ page: 3 }, 1)).toBe(3);
      expect(getPaginationPageValue(null, 1)).toBe(1);
      expect(getPaginationPageValue({}, 1)).toBe(1);
    });

    it("getProviderOptions", () => {
      expect(getProviderOptions(null)).toEqual([]);
      const options = [{ value: "1", label: "One" }];
      expect(getProviderOptions(options)).toBe(options);
    });

    it("reconcileConnectionsPage", async () => {
      const fetchFn = vi.fn().mockResolvedValue({ success: true });
      const result = await reconcileConnectionsPage(fetchFn, 2);
      expect(fetchFn).toHaveBeenCalledWith(2);
      expect(result).toEqual({ success: true });
    });
  });

  describe("Quota Cache (getQuotaCache / setQuotaCache)", () => {
    const originalWindow = (globalThis as unknown as { window?: unknown }).window;

    beforeEach(() => {
      let store: Record<string, string> = {};
      const mockStorage = {
        getItem: (key: string) => store[key] || null,
        setItem: (key: string, value: string) => {
          store[key] = value;
        },
        clear: () => {
          store = {};
        },
      };
      (globalThis as unknown as { window: unknown }).window = {
        localStorage: mockStorage,
      };
    });

    afterEach(() => {
      (globalThis as unknown as { window: unknown }).window = originalWindow;
    });

    it("getQuotaCache should return empty object when window is undefined", () => {
      delete (globalThis as unknown as { window?: unknown }).window;
      expect(getQuotaCache()).toEqual({});
    });

    it("getQuotaCache should return empty object when no cache exists", () => {
      expect(getQuotaCache()).toEqual({});
    });

    it("getQuotaCache should return parsed cache", () => {
      ((globalThis as unknown as { window: { localStorage: Storage } }).window.localStorage).setItem(
        QUOTA_CACHE_KEY,
        JSON.stringify({ c1: { name: "test" } })
      );
      expect(getQuotaCache()).toEqual({ c1: { name: "test" } });
    });

    it("getQuotaCache should handle JSON parse errors gracefully", () => {
      ((globalThis as unknown as { window: { localStorage: Storage } }).window.localStorage).setItem(
        QUOTA_CACHE_KEY,
        "invalid json"
      );
      const spy = vi.spyOn(console, "error").mockImplementation(() => {});
      expect(getQuotaCache()).toEqual({});
      expect(spy).toHaveBeenCalled();
      spy.mockRestore();
    });

    it("setQuotaCache should store entry with cachedAt timestamp", () => {
      const quota: QuotaEntry = { name: "quota1", used: 10, total: 100 };
      setQuotaCache("c1", quota);
      const cache = getQuotaCache();
      expect(cache.c1).toBeDefined();
      expect(cache.c1.name).toBe("quota1");
      expect(cache.c1.cachedAt).toBeDefined();
    });

    it("setQuotaCache should do nothing when window is undefined", () => {
      delete (globalThis as unknown as { window?: unknown }).window;
      expect(() => setQuotaCache("c1", { name: "quota1" })).not.toThrow();
    });
  });

  describe("formatResetTime", () => {
    it("should return '-' for missing, null, or invalid dates", () => {
      expect(formatResetTime(null)).toBe("-");
      expect(formatResetTime(undefined)).toBe("-");
      expect(formatResetTime("invalid-date-string")).toBe("-");
    });

    it("should return '-' for dates in the past or right now", () => {
      const pastDate = new Date(Date.now() - 10000);
      expect(formatResetTime(pastDate)).toBe("-");
    });

    it("should format time in minutes when reset is < 60 minutes away", () => {
      const futureDate = new Date(Date.now() + 30 * 60 * 1000); // 30 mins
      expect(formatResetTime(futureDate)).toBe("30m");
    });

    it("should format time in hours and minutes when reset is < 24 hours away", () => {
      const futureDate = new Date(Date.now() + (4 * 60 + 25) * 60 * 1000); // 4h 25m
      expect(formatResetTime(futureDate)).toBe("4h 25m");
    });

    it("should format time in days, hours, and minutes when reset is >= 24 hours away", () => {
      const futureDate = new Date(Date.now() + (26 * 60 + 15) * 60 * 1000); // 1d 2h 15m
      expect(formatResetTime(futureDate)).toBe("1d 2h 15m");
    });
  });

  describe("getStatusColor & getStatusEmoji", () => {
    it("getStatusColor", () => {
      expect(getStatusColor(80)).toBe("green");
      expect(getStatusColor(71)).toBe("green");
      expect(getStatusColor(70)).toBe("yellow");
      expect(getStatusColor(30)).toBe("yellow");
      expect(getStatusColor(29)).toBe("red");
      expect(getStatusColor(0)).toBe("red");
    });

    it("getStatusEmoji", () => {
      expect(getStatusEmoji(85)).toBe("🟢");
      expect(getStatusEmoji(50)).toBe("🟡");
      expect(getStatusEmoji(10)).toBe("🔴");
    });
  });

  describe("calculatePercentage & getRemainingPercentage", () => {
    describe("calculatePercentage", () => {
      it("should return 0 when total is falsy or 0", () => {
        expect(calculatePercentage(10, 0)).toBe(0);
      });

      it("should return 100 when used is null/undefined or < 0", () => {
        expect(calculatePercentage(-5, 100)).toBe(100);
      });

      it("should return 0 when used >= total", () => {
        expect(calculatePercentage(100, 100)).toBe(0);
        expect(calculatePercentage(150, 100)).toBe(0);
      });

      it("should return calculated rounded percentage", () => {
        expect(calculatePercentage(25, 100)).toBe(75);
        expect(calculatePercentage(1, 3)).toBe(67); // ((3-1)/3)*100 = 66.666 -> 67
      });
    });

    describe("getRemainingPercentage", () => {
      it("should prefer quota.remaining if present", () => {
        const quota: QuotaEntry = { remaining: 45.6 };
        expect(getRemainingPercentage(quota)).toBe(46);
      });

      it("should bound negative quota.remaining to 0", () => {
        const quota: QuotaEntry = { remaining: -10 };
        expect(getRemainingPercentage(quota)).toBe(0);
      });

      it("should use quota.remainingPercentage if remaining is undefined", () => {
        const quota: QuotaEntry = { remainingPercentage: 88.4 };
        expect(getRemainingPercentage(quota)).toBe(88);
      });

      it("should fallback to calculatePercentage if neither remaining field is present", () => {
        const quota: QuotaEntry = { used: 30, total: 100 };
        expect(getRemainingPercentage(quota)).toBe(70);
      });
    });
  });

  describe("Quota Visibility Helpers", () => {
    const quota1: QuotaEntry = { modelKey: "gpt-4", name: "GPT-4" };
    const quota2: QuotaEntry = { name: "Claude Sonnet" };

    it("getQuotaVisibilityKey", () => {
      expect(getQuotaVisibilityKey(quota1)).toBe("gpt-4");
      expect(getQuotaVisibilityKey(quota2)).toBe("Claude Sonnet");
      expect(getQuotaVisibilityKey(null)).toBe("");
    });

    it("filterQuotasByVisibility", () => {
      const visibility = {
        openai: { hidden: ["gpt-4"] },
      };
      const filtered = filterQuotasByVisibility("openai", [quota1, quota2], visibility);
      expect(filtered).toEqual([quota2]);
    });

    it("getHiddenQuotaRows", () => {
      const visibility = {
        openai: { hidden: ["gpt-4"] },
      };
      const hidden = getHiddenQuotaRows("openai", [quota1, quota2], visibility);
      expect(hidden).toEqual([quota1]);
    });

    it("should return empty arrays when quotas are empty or non-array", () => {
      expect(filterQuotasByVisibility("openai", [])).toEqual([]);
      expect(getHiddenQuotaRows("openai", [])).toEqual([]);
    });
  });

  describe("parseQuotaData", () => {
    it("should return empty array for null/undefined or invalid data", () => {
      expect(parseQuotaData("github", null)).toEqual([]);
      expect(parseQuotaData("github", undefined)).toEqual([]);
    });

    it("should unwrap wrapped quota object response { quota: ... }", () => {
      const rawData = {
        quota: {
          quotas: {
            "model-a": { used: 5, total: 10, resetAt: "2025-01-01" },
          },
        },
      };
      const result = parseQuotaData("github", rawData);
      expect(result).toHaveLength(1);
      expect(result[0].name).toBe("model-a");
      expect(result[0].used).toBe(5);
    });

    it("github provider parsing", () => {
      const rawData = {
        quotas: {
          copilot: { used: 10, total: 100, resetAt: "2025-06-01" },
        },
      };
      const result = parseQuotaData("github", rawData);
      expect(result).toEqual([
        { name: "copilot", used: 10, total: 100, resetAt: "2025-06-01" },
      ]);
    });

    it("antigravity provider parsing", () => {
      const rawData = {
        quotas: {
          "gemini-flash": { displayName: "Gemini Flash", used: 2, total: 50, remainingPercentage: 96, resetAt: null },
        },
      };
      const result = parseQuotaData("antigravity", rawData);
      expect(result).toEqual([
        {
          name: "Gemini Flash",
          modelKey: "gemini-flash",
          used: 2,
          total: 50,
          resetAt: null,
          remainingPercentage: 96,
        },
      ]);
    });

    it("codex provider parsing", () => {
      const rawData = {
        quotas: {
          "5hr_window": { used: 5, total: 10, remaining: 5, resetAt: null },
        },
      };
      const result = parseQuotaData("codex", rawData);
      expect(result).toEqual([
        { name: "5hr_window", used: 5, total: 10, remaining: 5, resetAt: null },
      ]);
    });

    it("kiro provider parsing", () => {
      const rawData = {
        quotas: {
          requests: { used: 100, total: 1000, resetAt: "2025-02-01" },
        },
      };
      const result = parseQuotaData("kiro", rawData);
      expect(result).toEqual([
        { name: "requests", used: 100, total: 1000, resetAt: "2025-02-01" },
      ]);
    });

    it("qoder provider parsing", () => {
      const rawData = {
        quotas: {
          user: { used: 20, total: 100, unit: "credits", resetAt: null },
          organization: { used: 0, total: 0, resetAt: null },
        },
      };
      const result = parseQuotaData("qoder", rawData);
      // organization bucket with total 0 should be skipped
      expect(result).toEqual([
        { name: "Personal", used: 20, total: 100, unit: "credits", resetAt: null },
      ]);
    });

    it("claude provider parsing", () => {
      // Error message case
      const errorData = { message: "Rate limit exceeded" };
      expect(parseQuotaData("claude", errorData)).toEqual([
        { name: "error", used: 0, total: 0, resetAt: null, message: "Rate limit exceeded" },
      ]);

      // Normal quotas case
      const normalData = {
        quotas: {
          session: { used: 10, total: 100, resetAt: null },
        },
      };
      expect(parseQuotaData("claude", normalData)).toEqual([
        { name: "session", used: 10, total: 100, resetAt: null },
      ]);
    });

    it("vercel-ai-gateway provider parsing", () => {
      const rawData = {
        quotas: {
          usd: { used: 4.5, total: 100, resetAt: null, remainingPercentage: 95.5 },
        },
      };
      expect(parseQuotaData("vercel-ai-gateway", rawData)).toEqual([
        { name: "usd", used: 4.5, total: 100, resetAt: null, remainingPercentage: 95.5 },
      ]);
    });

    it("codebuddy-cn provider parsing", () => {
      const rawData = {
        quotas: {
          "Bonus Pack 1": { used: 5, total: 10, resetAt: "2025-05-01", recurring: false },
        },
      };
      expect(parseQuotaData("codebuddy-cn", rawData)).toEqual([
        { name: "Bonus Pack 1", used: 5, total: 10, resetAt: "2025-05-01", recurring: false },
      ]);
    });

    it("grok-cli, kimi, deepseek, ollama provider parsing", () => {
      for (const provider of ["grok-cli", "kimi", "deepseek", "ollama"]) {
        const rawData = {
          quotas: {
            standard: { used: 10, total: 100, resetAt: null, remainingPercentage: 90 },
          },
        };
        expect(parseQuotaData(provider, rawData)).toEqual([
          { name: "standard", used: 10, total: 100, resetAt: null, remainingPercentage: 90 },
        ]);
      }
    });

    it("default unknown provider parsing", () => {
      const rawData = {
        quotas: {
          customQuota: { used: 1, total: 5, resetAt: null },
        },
      };
      expect(parseQuotaData("some-unknown-provider", rawData)).toEqual([
        { name: "customQuota", used: 1, total: 5, resetAt: null },
      ]);
    });
  });
});
