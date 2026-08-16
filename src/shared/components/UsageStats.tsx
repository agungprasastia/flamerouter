"use client";

import { useState, useEffect, useMemo, useCallback, useRef, ReactNode } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { FREE_PROVIDERS, AI_PROVIDERS } from "@/shared/constants/providers";

// Keep providers without serviceKinds (default LLM) or with "llm" in serviceKinds
function isLLMProvider(id: string) {
  const p = (AI_PROVIDERS as Record<string, { serviceKinds?: string[] }>)[id];
  if (!p?.serviceKinds) return true;
  return p.serviceKinds.includes("llm");
}
import Badge from "./Badge";
import { LoaderCircle } from "lucide-react";
import OverviewCards from "@/app/(dashboard)/dashboard/usage/components/OverviewCards";
import UsageTable, {
  fmt,
  fmtTime,
  UsageTableColumn,
  UsageGroupedData,
  UsageItemData,
} from "@/app/(dashboard)/dashboard/usage/components/UsageTable";
import dynamic from "next/dynamic";
// Lazy-load: keeps @xyflow/react out of the shared bundle until topology renders
const ProviderTopology = dynamic(
  () => import("@/app/(dashboard)/dashboard/usage/components/ProviderTopology"),
  { ssr: false },
);
import type { ProviderItem, ActiveRequestItem } from "@/app/(dashboard)/dashboard/usage/components/ProviderTopology";
import UsageChart from "@/app/(dashboard)/dashboard/usage/components/UsageChart";

export interface RequestItem {
  model?: string;
  provider?: string;
  promptTokens?: number;
  completionTokens?: number;
  latencyMs?: number;
  durationMs?: number;
  latency?: { total?: number };
  status?: string;
  timestamp?: string | number;
}

export interface UsageDataItem {
  promptTokens?: number;
  completionTokens?: number;
  cachedTokens?: number;
  cost?: number;
  requests?: number;
  lastUsed?: string | number | null;
  rawModel?: string;
  provider?: string;
  accountName?: string;
  connectionId?: string;
  keyName?: string;
  endpoint?: string;
  [key: string]: unknown;
}

export interface SortedUsageItem extends UsageDataItem {
  key: string;
  totalTokens: number;
  totalCost: number;
  inputCost: number;
  cachedCost: number;
  outputCost: number;
  pending: number;
  [key: string]: unknown;
}

export interface UsageStatsData {
  byModel?: Record<string, UsageDataItem>;
  byAccount?: Record<string, UsageDataItem>;
  byApiKey?: Record<string, UsageDataItem>;
  byEndpoint?: Record<string, UsageDataItem>;
  activeRequests?: Array<{ connectionId?: string; model?: string; provider?: string }>;
  recentRequests?: RequestItem[];
  errorProvider?: string;
  pending?: {
    byModel?: Record<string, number>;
    byAccount?: Record<string, Record<string, number>>;
  };
  totalRequests?: number;
  totalTokens?: number;
  totalCost?: number;
  activeConnections?: number;
}

function timeAgo(timestamp?: string | number) {
  if (!timestamp) return "—";
  const diff = Math.floor((Date.now() - new Date(timestamp).getTime()) / 1000);
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

// Auto-update time display every second without re-rendering parent
function TimeAgo({ timestamp }: { timestamp?: string | number }) {
  const [, setTick] = useState(0);

  useEffect(() => {
    const timer = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  return <>{timeAgo(timestamp)}</>;
}

function RecentRequests({ requests = [] }: { requests?: RequestItem[] }) {
  return (
    <section className="flex h-[480px] min-w-0 flex-col overflow-hidden border-y border-border lg:border-l lg:pl-6">
      {/* Header */}
      <div className="px-1 py-2 border-b border-border shrink-0">
        <span className="text-xs font-semibold text-text-muted uppercase tracking-wide">
          Recent Requests
        </span>
      </div>

      {!requests.length ? (
        <div className="flex-1 flex items-center justify-center text-text-muted text-sm">
          No requests yet.
        </div>
      ) : (
        <div className="flex-1 overflow-y-auto">
          <table className="w-full min-w-[560px] border-collapse text-xs">
            <thead className="sticky top-0 bg-bg z-10">
              <tr className="border-b border-border">
                <th className="py-1.5 text-left font-semibold text-text-muted">
                  Model
                </th>
                <th className="py-1.5 text-left font-semibold text-text-muted">
                  Provider
                </th>
                <th className="py-1.5 text-right font-semibold text-text-muted whitespace-nowrap">
                  In / Out
                </th>
                <th className="py-1.5 text-right font-semibold text-text-muted">
                  Latency
                </th>
                <th className="py-1.5 text-left font-semibold text-text-muted">
                  Status
                </th>
                <th className="py-1.5 text-right font-semibold text-text-muted">
                  When
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/50">
              {requests.map((r, i) => {
                const ok =
                  !r.status || r.status === "ok" || r.status === "success";
                const status = ok ? "Success" : String(r.status || "Error");
                const latency = r.latencyMs ?? r.latency?.total ?? r.durationMs;
                return (
                  <tr key={i} className="hover:bg-bg-subtle transition-colors">
                    <td
                      className="py-1.5 font-mono truncate max-w-[120px]"
                      title={r.model}
                    >
                      {r.model}
                    </td>
                    <td className="max-w-[100px] truncate py-1.5 text-text-muted">
                      {r.provider || "Unknown"}
                    </td>
                    <td className="py-1.5 text-right whitespace-nowrap">
                      <span className="text-primary">
                        {fmt(r.promptTokens)}↑
                      </span>{" "}
                      <span className="text-success">
                        {fmt(r.completionTokens)}↓
                      </span>
                    </td>
                    <td className="py-1.5 text-right tabular-nums text-text-muted">
                      {latency != null ? `${latency}ms` : "—"}
                    </td>
                    <td className="py-1.5">
                      <span className="inline-flex items-center gap-1.5">
                        <span
                          aria-hidden="true"
                          className={`block size-1.5 rounded-full ${ok ? "bg-success" : "bg-error"}`}
                        />
                        {status}
                      </span>
                    </td>
                    <td className="py-1.5 text-right text-text-muted whitespace-nowrap">
                      <TimeAgo timestamp={r.timestamp} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function sortData(
  dataMap?: Record<string, UsageDataItem> | null,
  pendingMap: Record<string, number> = {},
  sortBy = "rawModel",
  sortOrder = "asc",
): SortedUsageItem[] {
  return Object.entries(dataMap || {})
    .map(([key, data]) => {
      const promptTokens = data.promptTokens || 0;
      const completionTokens = data.completionTokens || 0;
      const totalTokens = promptTokens + completionTokens;
      const totalCost = data.cost || 0;
      const cachedTokens = data.cachedTokens || 0;
      const nonCachedInput = Math.max(0, promptTokens - cachedTokens);
      const inputCost =
        totalTokens > 0 ? nonCachedInput * (totalCost / totalTokens) : 0;
      const cachedCost =
        totalTokens > 0 ? cachedTokens * (totalCost / totalTokens) : 0;
      const outputCost =
        totalTokens > 0
          ? completionTokens * (totalCost / totalTokens)
          : 0;
      return {
        ...data,
        key,
        totalTokens,
        totalCost,
        inputCost,
        cachedCost,
        outputCost,
        pending: pendingMap[key] || 0,
      };
    })
    .sort((a: Record<string, unknown>, b: Record<string, unknown>) => {
      const aVal = a[sortBy] as string | number | undefined;
      const bVal = b[sortBy] as string | number | undefined;
      let valA: string | number | undefined = typeof aVal === "string" ? aVal.toLowerCase() : aVal;
      let valB: string | number | undefined = typeof bVal === "string" ? bVal.toLowerCase() : bVal;
      if (typeof valA === "number" && typeof valB === "number") {
        return sortOrder === "asc" ? valA - valB : valB - valA;
      }
      if (String(valA) < String(valB)) return sortOrder === "asc" ? -1 : 1;
      if (String(valA) > String(valB)) return sortOrder === "asc" ? 1 : -1;
      return 0;
    });
}

function getGroupKey(item: SortedUsageItem, keyField: string): string {
  switch (keyField) {
    case "rawModel":
      return item.rawModel || "Unknown Model";
    case "accountName":
      return (
        item.accountName ||
        (item.connectionId ? `Account ${item.connectionId.slice(0, 8)}...` : "") ||
        "Unknown Account"
      );
    case "keyName":
      return item.keyName || "Unknown Key";
    case "endpoint":
      return item.endpoint || "Unknown Endpoint";
    default:
      return (item[keyField] as string) || "Unknown";
  }
}

export interface GroupSummary {
  requests: number;
  promptTokens: number;
  completionTokens: number;
  cachedTokens: number;
  totalTokens: number;
  cost: number;
  inputCost: number;
  cachedCost: number;
  outputCost: number;
  lastUsed: string | number | null;
  pending: number;
}

export interface GroupedUsageData {
  groupKey: string;
  summary: UsageItemData;
  items: SortedUsageItem[];
}

function groupDataByKey(data: SortedUsageItem[], keyField: string): UsageGroupedData<SortedUsageItem>[] {
  if (!Array.isArray(data)) return [];
  const groups: Record<string, UsageGroupedData<SortedUsageItem>> = {};
  data.forEach((item) => {
    const gk = getGroupKey(item, keyField);
    if (!groups[gk]) {
      groups[gk] = {
        groupKey: gk,
        summary: {
          requests: 0,
          promptTokens: 0,
          completionTokens: 0,
          cachedTokens: 0,
          totalTokens: 0,
          cost: 0,
          inputCost: 0,
          cachedCost: 0,
          outputCost: 0,
          lastUsed: null,
          pending: 0,
        },
        items: [],
      };
    }
    const currentGroup = groups[gk];
    if (currentGroup) {
      const s = currentGroup.summary;
      s.requests = (Number(s.requests) || 0) + (item.requests || 0);
      s.promptTokens = (Number(s.promptTokens) || 0) + (item.promptTokens || 0);
      s.completionTokens = (Number(s.completionTokens) || 0) + (item.completionTokens || 0);
      s.cachedTokens = (Number(s.cachedTokens) || 0) + (item.cachedTokens || 0);
      s.totalTokens = (Number(s.totalTokens) || 0) + (item.totalTokens || 0);
      s.cost = (Number(s.cost) || 0) + (item.cost || 0);
      s.inputCost = (Number(s.inputCost) || 0) + (item.inputCost || 0);
      s.cachedCost = (Number(s.cachedCost) || 0) + (item.cachedCost || 0);
      s.outputCost = (Number(s.outputCost) || 0) + (item.outputCost || 0);
      s.pending = (Number(s.pending) || 0) + (item.pending || 0);
      if (
        item.lastUsed &&
        (!s.lastUsed || new Date(item.lastUsed).getTime() > new Date(s.lastUsed as string | number).getTime())
      ) {
        s.lastUsed = item.lastUsed;
      }
      currentGroup.items.push(item);
    }
  });
  return Object.values(groups);
}

const MODEL_COLUMNS: UsageTableColumn[] = [
  { field: "rawModel", label: "Model" },
  { field: "provider", label: "Provider" },
  { field: "requests", label: "Requests", align: "right" },
  { field: "lastUsed", label: "Last Used", align: "right" },
];

const ACCOUNT_COLUMNS: UsageTableColumn[] = [
  { field: "rawModel", label: "Model" },
  { field: "provider", label: "Provider" },
  { field: "accountName", label: "Account" },
  { field: "requests", label: "Requests", align: "right" },
  { field: "lastUsed", label: "Last Used", align: "right" },
];

const API_KEY_COLUMNS: UsageTableColumn[] = [
  { field: "keyName", label: "API Key Name" },
  { field: "rawModel", label: "Model" },
  { field: "provider", label: "Provider" },
  { field: "requests", label: "Requests", align: "right" },
  { field: "lastUsed", label: "Last Used", align: "right" },
];

const ENDPOINT_COLUMNS: UsageTableColumn[] = [
  { field: "endpoint", label: "Endpoint" },
  { field: "rawModel", label: "Model" },
  { field: "provider", label: "Provider" },
  { field: "requests", label: "Requests", align: "right" },
  { field: "lastUsed", label: "Last Used", align: "right" },
];

const TABLE_OPTIONS = [
  { value: "model", label: "Usage by Model" },
  { value: "account", label: "Usage by Account" },
  { value: "apiKey", label: "Usage by API Key" },
  { value: "endpoint", label: "Usage by Endpoint" },
];

const PERIODS = [
  { value: "today", label: "Today" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7D" },
  { value: "30d", label: "30D" },
  { value: "60d", label: "60D" },
];

export interface UsageStatsProps {
  period?: string;
  setPeriod?: (period: string) => void;
  hidePeriodSelector?: boolean;
}

export default function UsageStats({
  period: periodProp,
  setPeriod: setPeriodProp,
  hidePeriodSelector = false,
}: UsageStatsProps = {}) {
  const router = useRouter();
  const searchParams = useSearchParams();

  const sortBy = searchParams.get("sortBy") || "rawModel";
  const sortOrder = searchParams.get("sortOrder") || "asc";

  const [stats, setStats] = useState<UsageStatsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [fetching, setFetching] = useState(false);
  const [tableView, setTableView] = useState("model");
  const [viewMode, setViewMode] = useState<"costs" | "tokens">("costs");
  const [providers, setProviders] = useState<ProviderItem[]>([]);
  const [periodLocal, setPeriodLocal] = useState("today");
  const isInitialLoad = useRef(true);
  const hasLoadedStats = useRef(false);
  const period = periodProp ?? periodLocal;
  const setPeriod = setPeriodProp ?? setPeriodLocal;

  // Fetch connected providers once, deduplicate by provider type
  // Always include noAuth free providers (e.g. opencode) regardless of connections
  useEffect(() => {
    Promise.all([
      fetch("/api/providers").then((r) => (r.ok ? r.json() : null)),
      fetch("/api/provider-nodes").then((r) => (r.ok ? r.json() : null)),
    ])
      .then(([d, nodesData]) => {
        // Build node name lookup for custom providers
        const nodeNameMap: Record<string, string> = {};
        for (const node of nodesData?.nodes || []) {
          if (node.id && node.name) nodeNameMap[node.id] = node.name;
        }
        const seen = new Set<string>();
        const unique = (d?.connections || [])
          .filter((c: { provider: string; isActive?: boolean }) => {
            if (c.isActive === false) return false;
            if (!isLLMProvider(c.provider)) return false;
            if (seen.has(c.provider)) return false;
            seen.add(c.provider);
            return true;
          })
          .map((c: { provider: string; name?: string; isActive?: boolean }) => ({
            ...c,
            nodeName: nodeNameMap[c.provider] || null,
          }));
        const noAuthProviders = Object.values(FREE_PROVIDERS)
          .filter((p) => p.noAuth && !seen.has(p.id) && isLLMProvider(p.id))
          .map((p) => ({ provider: p.id, name: p.name }));
        setProviders([...unique, ...noAuthProviders]);
      })
      .catch(() => {});
  }, []);

  // Fetch filtered stats via REST when period changes
  useEffect(() => {
    // First load: show full spinner; subsequent: show subtle fetching indicator
    if (isInitialLoad.current) {
      isInitialLoad.current = false;
      setLoading(true);
    } else {
      setFetching(true);
    }

    fetch(`/api/usage/stats?period=${period}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (data) {
          hasLoadedStats.current = true;
          setStats((prev) => ({ ...(prev || {}), ...data }));
        }
      })
      .catch(() => {})
      .finally(() => {
        setLoading(false);
        setFetching(false);
      });
  }, [period]);

  // SSE connection - real-time updates for activeRequests + recentRequests only
  useEffect(() => {
    const es = new EventSource("/api/usage/stream");

    es.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data);
        // Always merge only real-time fields, never overwrite full stats from REST
        setStats((prev) => {
          if (!prev) return prev;
          return {
            ...prev,
            activeRequests: data.activeRequests,
            recentRequests: data.recentRequests,
            errorProvider: data.errorProvider,
            pending: data.pending,
          };
        });
        if (hasLoadedStats.current) setLoading(false);
      } catch (err) {
        console.error("[SSE CLIENT] parse error:", err);
      }
    };

    es.onerror = () => setLoading(false);

    return () => es.close();
  }, []);

  const toggleSort = useCallback(
    (tableType: string, field: string) => {
      const params = new URLSearchParams(searchParams.toString());
      if (params.get("sortBy") === field) {
        params.set(
          "sortOrder",
          params.get("sortOrder") === "asc" ? "desc" : "asc",
        );
      } else {
        params.set("sortBy", field);
        params.set("sortOrder", "asc");
      }
      router.replace(`?${params.toString()}`, { scroll: false });
    },
    [searchParams, router],
  );

  // Compute active table data
  const activeTableConfig = useMemo(() => {
    if (!stats) return null;
    switch (tableView) {
      case "model": {
        const pendingMap = stats.pending?.byModel || {};
        return {
          columns: MODEL_COLUMNS,
          groupedData: groupDataByKey(
            sortData(stats.byModel, pendingMap, sortBy, sortOrder),
            "rawModel",
          ),
          storageKey: "usage-stats:expanded-models",
          emptyMessage: "No usage recorded yet.",
          renderSummaryCells: (group: UsageGroupedData<SortedUsageItem>) => (
            <>
              <td className="px-6 py-3 text-text-muted">—</td>
              <td className="px-6 py-3 text-right">
                {fmt(group.summary.requests as number | string | undefined)}
              </td>
              <td className="px-6 py-3 text-right text-text-muted whitespace-nowrap">
                {fmtTime(group.summary.lastUsed as string | number | null | undefined)}
              </td>
            </>
          ),
          renderDetailCells: (item: SortedUsageItem) => (
            <>
              <td
                className={`px-6 py-3 font-medium transition-colors ${item.pending > 0 ? "text-primary" : ""}`}
              >
                {item.rawModel}
              </td>
              <td className="px-6 py-3">
                <Badge
                  variant={item.pending > 0 ? "primary" : "neutral"}
                  size="sm"
                >
                  {item.provider}
                </Badge>
              </td>
              <td className="px-6 py-3 text-right">{fmt(item.requests)}</td>
              <td className="px-6 py-3 text-right text-text-muted whitespace-nowrap">
                {fmtTime(item.lastUsed)}
              </td>
            </>
          ),
        };
      }
      case "account": {
        const pendingMap: Record<string, number> = {};
        if (stats?.pending?.byAccount) {
          Object.entries(stats.byAccount || {}).forEach(
            ([accountKey, data]) => {
              const connId = data.connectionId as string;
              const connPending = connId ? stats?.pending?.byAccount?.[connId] : undefined;
              if (connPending) {
                const modelKey = data.provider
                  ? `${data.rawModel} (${data.provider})`
                  : (data.rawModel as string);
                pendingMap[accountKey] = (modelKey ? connPending[modelKey] : undefined) || 0;
              }
            },
          );
        }
        return {
          columns: ACCOUNT_COLUMNS,
          groupedData: groupDataByKey(
            sortData(stats.byAccount, pendingMap, sortBy, sortOrder),
            "accountName",
          ),
          storageKey: "usage-stats:expanded-accounts",
          emptyMessage: "No account-specific usage recorded yet.",
          renderSummaryCells: (group: UsageGroupedData<SortedUsageItem>) => (
            <>
              <td className="px-6 py-3 text-text-muted">—</td>
              <td className="px-6 py-3 text-text-muted">—</td>
              <td className="px-6 py-3 text-right">
                {fmt(group.summary.requests as number | string | undefined)}
              </td>
              <td className="px-6 py-3 text-right text-text-muted whitespace-nowrap">
                {fmtTime(group.summary.lastUsed as string | number | null | undefined)}
              </td>
            </>
          ),
          renderDetailCells: (item: SortedUsageItem) => (
            <>
              <td
                className={`px-6 py-3 font-medium transition-colors ${item.pending > 0 ? "text-primary" : ""}`}
              >
                {item.accountName ||
                  (item.connectionId ? `Account ${item.connectionId.slice(0, 8)}...` : "")}
              </td>
              <td
                className={`px-6 py-3 font-medium transition-colors ${item.pending > 0 ? "text-primary" : ""}`}
              >
                {item.rawModel}
              </td>
              <td className="px-6 py-3">
                <Badge
                  variant={item.pending > 0 ? "primary" : "neutral"}
                  size="sm"
                >
                  {item.provider}
                </Badge>
              </td>
              <td className="px-6 py-3 text-right">{fmt(item.requests)}</td>
              <td className="px-6 py-3 text-right text-text-muted whitespace-nowrap">
                {fmtTime(item.lastUsed)}
              </td>
            </>
          ),
        };
      }
      case "apiKey": {
        return {
          columns: API_KEY_COLUMNS,
          groupedData: groupDataByKey(
            sortData(stats.byApiKey, {}, sortBy, sortOrder),
            "keyName",
          ),
          storageKey: "usage-stats:expanded-apikeys",
          emptyMessage: "No API key usage recorded yet.",
          renderSummaryCells: (group: UsageGroupedData<SortedUsageItem>) => (
            <>
              <td className="px-6 py-3 text-text-muted">—</td>
              <td className="px-6 py-3 text-text-muted">—</td>
              <td className="px-6 py-3 text-right">
                {fmt(group.summary.requests as number | string | undefined)}
              </td>
              <td className="px-6 py-3 text-right text-text-muted whitespace-nowrap">
                {fmtTime(group.summary.lastUsed as string | number | null | undefined)}
              </td>
            </>
          ),
          renderDetailCells: (item: SortedUsageItem) => (
            <>
              <td className="px-6 py-3 font-medium">{item.keyName}</td>
              <td className="px-6 py-3">{item.rawModel}</td>
              <td className="px-6 py-3">
                <Badge variant="neutral" size="sm">
                  {item.provider}
                </Badge>
              </td>
              <td className="px-6 py-3 text-right">{fmt(item.requests)}</td>
              <td className="px-6 py-3 text-right text-text-muted whitespace-nowrap">
                {fmtTime(item.lastUsed)}
              </td>
            </>
          ),
        };
      }
      case "endpoint":
      default: {
        return {
          columns: ENDPOINT_COLUMNS,
          groupedData: groupDataByKey(
            sortData(stats.byEndpoint, {}, sortBy, sortOrder),
            "endpoint",
          ),
          storageKey: "usage-stats:expanded-endpoints",
          emptyMessage: "No endpoint usage recorded yet.",
          renderSummaryCells: (group: UsageGroupedData<SortedUsageItem>) => (
            <>
              <td className="px-6 py-3 text-text-muted">—</td>
              <td className="px-6 py-3 text-text-muted">—</td>
              <td className="px-6 py-3 text-right">
                {fmt(group.summary.requests as number | string | undefined)}
              </td>
              <td className="px-6 py-3 text-right text-text-muted whitespace-nowrap">
                {fmtTime(group.summary.lastUsed as string | number | null | undefined)}
              </td>
            </>
          ),
          renderDetailCells: (item: SortedUsageItem) => (
            <>
              <td className="px-6 py-3 font-medium font-mono text-sm">
                {item.endpoint}
              </td>
              <td className="px-6 py-3">{item.rawModel}</td>
              <td className="px-6 py-3">
                <Badge variant="neutral" size="sm">
                  {item.provider}
                </Badge>
              </td>
              <td className="px-6 py-3 text-right">{fmt(item.requests)}</td>
              <td className="px-6 py-3 text-right text-text-muted whitespace-nowrap">
                {fmtTime(item.lastUsed)}
              </td>
            </>
          ),
        };
      }
    }
  }, [stats, tableView, sortBy, sortOrder]);

  if (!stats && !loading)
    return (
      <div className="text-text-muted">Failed to load usage statistics.</div>
    );

  const spinner = (
    <div className="flex items-center justify-center py-12 text-text-muted">
      <LoaderCircle className="size-8 animate-spin" aria-label="Loading" />
    </div>
  );

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <section className="editorial-intro flex min-w-0 flex-col gap-6">
        {/* Period selector (hidden when controlled by parent) */}
        {!hidePeriodSelector && (
          <div className="flex w-full items-center gap-2 sm:w-auto sm:self-end">
            <div className="grid flex-1 grid-cols-5 items-center gap-1 rounded-lg border border-border bg-bg-subtle p-1 sm:flex sm:flex-none">
              {PERIODS.map((p) => (
                <button
                  key={p.value}
                  onClick={() => setPeriod(p.value)}
                  disabled={fetching}
                  className={`rounded-md px-3 py-1 text-sm font-medium transition-colors ${period === p.value ? "bg-primary text-white shadow-sm" : "text-text-muted hover:bg-bg-hover hover:text-text"}`}
                >
                  {p.label}
                </button>
              ))}
            </div>
            {fetching && (
              <LoaderCircle
                className="size-4 animate-spin text-text-muted"
                aria-label="Refreshing usage"
              />
            )}
          </div>
        )}

        {loading ? spinner : stats && <OverviewCards stats={stats} />}
      </section>

      {/* Provider topology + Recent Requests */}
      {loading ? (
        spinner
      ) : (
        <div className="grid min-w-0 grid-cols-1 items-stretch gap-2 lg:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]">
          <ProviderTopology
            providers={providers}
            activeRequests={stats?.activeRequests || []}
            lastProvider={stats?.recentRequests?.[0]?.provider || ""}
            errorProvider={stats?.errorProvider || ""}
          />
          <RecentRequests requests={stats?.recentRequests || []} />
        </div>
      )}

      {/* Token / Cost chart - sync period */}
      {loading ? spinner : <UsageChart period={period} />}

      {/* Table with dropdown selector */}
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <select
            value={tableView}
            onChange={(e) => setTableView(e.target.value)}
            className="w-full rounded-lg border border-border bg-surface px-3 py-1.5 text-sm font-medium text-text-main focus:outline-none focus:ring-2 focus:ring-primary/50 sm:w-auto"
            style={{ colorScheme: "auto" }}
          >
            {TABLE_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
          <div className="grid grid-cols-2 items-center gap-1 rounded-lg border border-border bg-bg-subtle p-1 sm:flex">
            <button
              onClick={() => setViewMode("costs")}
              className={`px-3 py-1 rounded-md text-sm font-medium transition-colors ${viewMode === "costs" ? "bg-primary text-white shadow-sm" : "text-text-muted hover:text-text hover:bg-bg-hover"}`}
            >
              Costs
            </button>
            <button
              onClick={() => setViewMode("tokens")}
              className={`px-3 py-1 rounded-md text-sm font-medium transition-colors ${viewMode === "tokens" ? "bg-primary text-white shadow-sm" : "text-text-muted hover:text-text hover:bg-bg-hover"}`}
            >
              Tokens
            </button>
          </div>
        </div>
        {loading
          ? spinner
          : activeTableConfig && (
              <UsageTable
                title=""
                columns={activeTableConfig.columns}
                groupedData={activeTableConfig.groupedData}
                tableType={tableView}
                sortBy={sortBy}
                sortOrder={sortOrder}
                onToggleSort={toggleSort}
                viewMode={viewMode}
                storageKey={activeTableConfig.storageKey}
                renderSummaryCells={activeTableConfig.renderSummaryCells}
                renderDetailCells={activeTableConfig.renderDetailCells}
                emptyMessage={activeTableConfig.emptyMessage}
              />
            )}
      </div>
    </div>
  );
}
