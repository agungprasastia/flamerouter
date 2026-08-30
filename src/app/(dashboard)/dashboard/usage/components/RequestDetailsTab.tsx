"use client";
/* eslint-disable react-hooks/set-state-in-effect */

import { useState, useEffect, useCallback, useId, type ReactNode } from "react";
import Button from "@/shared/components/Button";
import Drawer from "@/shared/components/Drawer";
import Pagination from "@/shared/components/Pagination";
import { cn } from "@/shared/utils/cn";
import { AI_PROVIDERS, getProviderByAlias } from "@/shared/constants/providers";
import {
  Brain,
  Braces,
  ChevronRight,
  Image as ImageIcon,
  Languages,
  LogIn,
  LogOut,
} from "lucide-react";

interface ProviderItem {
  id: string;
  name: string;
}

interface ProviderCacheItem {
  name?: string;
  [key: string]: unknown;
}

type ProviderCache = Record<string, string | ProviderCacheItem>;

interface TokenStats {
  prompt_tokens?: number;
  input_tokens?: number;
  cached_tokens?: number;
  cache_read_input_tokens?: number;
  cache_creation_input_tokens?: number;
  completion_tokens?: number;
  [key: string]: unknown;
}

interface LatencyStats {
  ttft?: number;
  total?: number;
}

interface PxpipeStats {
  applied?: boolean;
  tokensBeforeEst?: number;
  tokensAfterEst?: number;
  savedPct?: number;
  imageCount?: number;
  durationMs?: number;
  reason?: string;
  detail?: string;
}

interface ResponseDetail {
  thinking?: string;
  content?: string;
}

interface RequestDetailItem {
  id: string;
  timestamp: string | number;
  provider: string;
  model: string;
  status: string;
  latency?: LatencyStats;
  tokens?: TokenStats;
  pxpipe?: PxpipeStats;
  request?: unknown;
  providerRequest?: unknown;
  providerResponse?: unknown;
  response?: ResponseDetail;
}

let providerNameCache: ProviderCache | null = null;
let providerNodesCache: Record<string, string> | null = null;

async function fetchProviderNames(): Promise<{
  providerNameCache: ProviderCache;
  providerNodesCache: Record<string, string>;
}> {
  if (providerNameCache && providerNodesCache) {
    return { providerNameCache, providerNodesCache };
  }

  const nodesRes = await fetch("/api/provider-nodes");
  const nodesData = (await nodesRes.json()) as { nodes?: ProviderItem[] };
  const nodes = nodesData.nodes || [];
  providerNodesCache = {};

  for (const node of nodes) {
    providerNodesCache[node.id] = node.name;
  }

  providerNameCache = {
    ...AI_PROVIDERS,
    ...providerNodesCache,
  };

  return { providerNameCache, providerNodesCache };
}

function getProviderName(providerId: string, cache: ProviderCache | null): string {
  if (!providerId) return providerId;
  if (!cache) return providerId;

  const cached = cache[providerId];

  if (typeof cached === "string") {
    return cached;
  }

  if (cached && typeof cached === "object" && cached.name) {
    return cached.name;
  }

  const providerConfig =
    getProviderByAlias(providerId) || (AI_PROVIDERS as Record<string, { name?: string }>)[providerId];
  return providerConfig?.name || providerId;
}

interface CollapsibleSectionProps {
  title: string;
  children: ReactNode;
  defaultOpen?: boolean;
  icon?: ReactNode;
}

function CollapsibleSection({
  title,
  children,
  defaultOpen = false,
  icon = null,
}: CollapsibleSectionProps) {
  const [isOpen, setIsOpen] = useState(defaultOpen);
  const contentId = useId();

  return (
    <div className="border border-black/5 dark:border-white/5 rounded-lg overflow-hidden">
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        aria-expanded={isOpen}
        aria-controls={contentId}
        className="w-full flex items-center justify-between p-3 bg-black/[0.02] dark:bg-white/[0.02] hover:bg-black/[0.04] dark:hover:bg-white/[0.04] transition-colors"
      >
        <div className="flex items-center gap-2">
          {icon}
          <span className="font-semibold text-sm text-text-main">{title}</span>
        </div>
        <ChevronRight
          className={cn(
            "size-5 text-text-muted transition-transform duration-200",
            isOpen ? "rotate-90" : "",
          )}
        />
      </button>

      {isOpen && (
        <div
          id={contentId}
          className="p-4 border-t border-black/5 dark:border-white/5"
        >
          {children}
        </div>
      )}
    </div>
  );
}

function getCachedTokens(tokens?: TokenStats): number {
  return Number(tokens?.cached_tokens ?? tokens?.cache_read_input_tokens ?? 0);
}

function getCacheCreationTokens(tokens?: TokenStats): number {
  return Number(tokens?.cache_creation_input_tokens ?? 0);
}

function getInputTokens(tokens?: TokenStats): number {
  const prompt = Number(tokens?.prompt_tokens ?? tokens?.input_tokens ?? 0);
  // Canonical storage keeps prompt cache-inclusive. Legacy Claude rows may have
  // stored prompt cache-exclusive; fall back to cache when it's larger so old
  // rows don't under-report input.
  const cache = getCachedTokens(tokens);
  return prompt < cache ? cache : prompt;
}

export default function RequestDetailsTab() {
  const [details, setDetails] = useState<RequestDetailItem[]>([]);
  const [pagination, setPagination] = useState({
    page: 1,
    pageSize: 20,
    totalItems: 0,
    totalPages: 0,
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [selectedDetail, setSelectedDetail] = useState<RequestDetailItem | null>(null);
  const [isDrawerOpen, setIsDrawerOpen] = useState(false);
  const [providers, setProviders] = useState<ProviderItem[]>([]);
  const [providerNameCache, setProviderNameCache] = useState<ProviderCache | null>(null);
  const [filters, setFilters] = useState({
    provider: "",
    startDate: "",
    endDate: "",
  });

  const fetchProviders = useCallback(async () => {
    try {
      const res = await fetch("/api/usage/providers");
      const data = (await res.json()) as { providers?: ProviderItem[] };
      setProviders(data.providers || []);

      const cache = await fetchProviderNames();
      setProviderNameCache(cache.providerNameCache);
    } catch (error) {
      setError("Failed to fetch providers.");
    }
  }, []);

  const fetchDetails = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const params = new URLSearchParams({
        page: pagination.page.toString(),
        pageSize: pagination.pageSize.toString(),
      });
      if (filters.provider) params.append("provider", filters.provider);
      if (filters.startDate) params.append("startDate", filters.startDate);
      if (filters.endDate) params.append("endDate", filters.endDate);

      const res = await fetch(`/api/usage/request-details?${params}`);
      if (!res.ok) throw new Error(`Request details failed: ${res.status}`);
      const data = (await res.json()) as { details?: RequestDetailItem[]; pagination?: Partial<typeof pagination> };

      setDetails(data.details || []);
      if (data.pagination) {
        setPagination((prev) => ({ ...prev, ...data.pagination }));
      }
    } catch (error) {
      console.error("Failed to fetch request details:", error);
      setError("Request details could not be loaded.");
    } finally {
      setLoading(false);
    }
  }, [pagination.page, pagination.pageSize, filters]);

  useEffect(() => {
    fetchProviders();
  }, [fetchProviders]);

  useEffect(() => {
    fetchDetails();
  }, [fetchDetails]);

  const handleViewDetail = (detail: RequestDetailItem) => {
    setSelectedDetail(detail);
    setIsDrawerOpen(true);
  };

  const handlePageChange = (newPage: number) => {
    setPagination((prev) => ({ ...prev, page: newPage }));
  };

  const handlePageSizeChange = (newPageSize: number) => {
    setPagination((prev) => ({ ...prev, pageSize: newPageSize, page: 1 }));
  };

  const handleClearFilters = () => {
    setFilters({ provider: "", startDate: "", endDate: "" });
  };

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <section className="border-y border-border py-5">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div className="flex min-w-0 flex-col gap-2">
            <label
              htmlFor="provider-filter"
              className="text-sm font-medium text-text-main"
            >
              Provider
            </label>
            <select
              id="provider-filter"
              value={filters.provider}
              onChange={(e) =>
                setFilters({ ...filters, provider: e.target.value })
              }
              className={cn(
                "h-9 px-3 rounded-lg border border-black/10 dark:border-white/10 bg-surface",
                "text-sm text-text-main focus:outline-none focus:ring-2 focus:ring-primary/20",
                "w-full min-w-0 cursor-pointer",
              )}
              style={{ colorScheme: "auto" }}
            >
              <option value="">All Providers</option>
              {providers.map((provider) => (
                <option key={provider.id} value={provider.id}>
                  {provider.name}
                </option>
              ))}
            </select>
          </div>

          <div className="flex min-w-0 flex-col gap-2">
            <label
              htmlFor="start-date-filter"
              className="text-sm font-medium text-text-main"
            >
              Start Date
            </label>
            <input
              id="start-date-filter"
              type="datetime-local"
              value={filters.startDate}
              onChange={(e) =>
                setFilters({ ...filters, startDate: e.target.value })
              }
              className={cn(
                "h-9 px-3 rounded-lg border border-black/10 dark:border-white/10 bg-surface",
                "w-full min-w-0 text-sm text-text-main focus:outline-none focus:ring-2 focus:ring-primary/20",
              )}
            />
          </div>

          <div className="flex min-w-0 flex-col gap-2">
            <label
              htmlFor="end-date-filter"
              className="text-sm font-medium text-text-main"
            >
              End Date
            </label>
            <input
              id="end-date-filter"
              type="datetime-local"
              value={filters.endDate}
              onChange={(e) =>
                setFilters({ ...filters, endDate: e.target.value })
              }
              className={cn(
                "h-9 px-3 rounded-lg border border-black/10 dark:border-white/10 bg-surface",
                "w-full min-w-0 text-sm text-text-main focus:outline-none focus:ring-2 focus:ring-primary/20",
              )}
            />
          </div>

          <div className="flex min-w-0 flex-col gap-2 sm:col-span-2 lg:col-span-1">
            <span
              className="hidden text-sm font-medium text-text-main opacity-0 lg:block"
              aria-hidden="true"
            >
              Clear
            </span>
            <Button
              variant="ghost"
              onClick={handleClearFilters}
              disabled={
                !filters.provider && !filters.startDate && !filters.endDate
              }
              className="w-full"
            >
              Clear Filters
            </Button>
          </div>
        </div>
      </section>

      <section className="min-w-0 overflow-hidden border-y border-border">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[880px]">
            <thead>
              <tr className="border-b border-black/5 dark:border-white/5">
                <th className="text-left p-4 text-sm font-semibold text-text-main">
                  Timestamp
                </th>
                <th className="text-left p-4 text-sm font-semibold text-text-main">
                  Model
                </th>
                <th className="text-left p-4 text-sm font-semibold text-text-main">
                  Provider
                </th>
                <th className="text-right p-4 text-sm font-semibold text-text-main">
                  Input Tokens
                </th>
                <th className="text-right p-4 text-sm font-semibold text-text-main">
                  Cached
                </th>
                <th className="text-right p-4 text-sm font-semibold text-text-main">
                  Cache Creation
                </th>
                <th className="text-right p-4 text-sm font-semibold text-text-main">
                  Output Tokens
                </th>
                <th className="text-left p-4 text-sm font-semibold text-text-main">
                  Latency
                </th>
                <th className="text-center p-4 text-sm font-semibold text-text-main">
                  Action
                </th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                Array.from({ length: 5 }, (_, row) => (
                  <tr
                    key={row}
                    className="border-b border-border last:border-b-0"
                  >
                    {Array.from({ length: 9 }, (_, cell) => (
                      <td key={cell} className="p-4">
                        <span className="block h-3 animate-pulse rounded bg-bg-subtle" />
                      </td>
                    ))}
                  </tr>
                ))
              ) : error ? (
                <tr>
                  <td colSpan={9} className="p-8 text-center">
                    <p className="text-sm text-error">{error}</p>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={fetchDetails}
                      className="mt-3"
                    >
                      Retry
                    </Button>
                  </td>
                </tr>
              ) : details.length === 0 ? (
                <tr>
                  <td colSpan={9} className="p-8 text-center text-text-muted">
                    No request details found
                  </td>
                </tr>
              ) : (
                details.map((detail, index) => (
                  <tr
                    key={`${detail.id}-${index}`}
                    className="border-b border-black/5 dark:border-white/5 last:border-b-0 hover:bg-black/[0.02] dark:hover:bg-white/[0.02] transition-colors"
                  >
                    <td className="whitespace-nowrap p-4 text-sm text-text-main">
                      {new Date(detail.timestamp).toLocaleString()}
                    </td>
                    <td className="max-w-[260px] truncate p-4 font-mono text-sm text-text-main">
                      {detail.model}
                    </td>
                    <td className="max-w-[180px] truncate p-4 text-sm text-text-main">
                      <span className="font-medium">
                        {getProviderName(detail.provider, providerNameCache)}
                      </span>
                    </td>
                    <td className="p-4 text-sm text-text-main text-right font-mono">
                      {getInputTokens(detail.tokens).toLocaleString()}
                    </td>
                    <td className="p-4 text-sm text-text-main text-right font-mono">
                      {getCachedTokens(detail.tokens) > 0
                        ? getCachedTokens(detail.tokens).toLocaleString()
                        : "—"}
                    </td>
                    <td className="p-4 text-sm text-text-main text-right font-mono">
                      {getCacheCreationTokens(detail.tokens) > 0
                        ? getCacheCreationTokens(detail.tokens).toLocaleString()
                        : "—"}
                    </td>
                    <td className="p-4 text-sm text-text-main text-right font-mono">
                      {detail.tokens?.completion_tokens?.toLocaleString() || 0}
                    </td>
                    <td className="p-4 text-sm text-text-muted">
                      <div className="flex flex-col gap-0.5">
                        <div>
                          TTFT:{" "}
                          <span className="font-mono">
                            {detail.latency?.ttft || 0}ms
                          </span>
                        </div>
                        <div>
                          Total:{" "}
                          <span className="font-mono">
                            {detail.latency?.total || 0}ms
                          </span>
                        </div>
                      </div>
                    </td>
                    <td className="p-4 text-center">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleViewDetail(detail)}
                      >
                        Detail
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {!loading && details.length > 0 && (
          <div className="border-t border-black/5 dark:border-white/5">
            <Pagination
              currentPage={pagination.page}
              pageSize={pagination.pageSize}
              totalItems={pagination.totalItems}
              onPageChange={handlePageChange}
              onPageSizeChange={handlePageSizeChange}
            />
          </div>
        )}
      </section>

      <Drawer
        isOpen={isDrawerOpen}
        onClose={() => setIsDrawerOpen(false)}
        title="Request Details"
        width="lg"
      >
        {selectedDetail && (
          <div className="space-y-6">
            <div className="grid min-w-0 grid-cols-1 gap-4 text-sm sm:grid-cols-2">
              <div>
                <span className="text-text-muted">ID:</span>{" "}
                <span className="break-all font-mono text-text-main">
                  {selectedDetail.id}
                </span>
              </div>
              <div>
                <span className="text-text-muted">Timestamp:</span>{" "}
                <span className="text-text-main">
                  {new Date(selectedDetail.timestamp).toLocaleString()}
                </span>
              </div>
              <div>
                <span className="text-text-muted">Provider:</span>{" "}
                <span className="text-text-main font-medium">
                  {getProviderName(selectedDetail.provider, providerNameCache)}
                </span>
              </div>
              <div>
                <span className="text-text-muted">Model:</span>{" "}
                <span className="text-text-main font-mono">
                  {selectedDetail.model}
                </span>
              </div>
              <div>
                <span className="text-text-muted">Status:</span>{" "}
                <span
                  className={cn(
                    "font-medium",
                    selectedDetail.status === "success"
                      ? "text-green-600"
                      : "text-red-600",
                  )}
                >
                  {selectedDetail.status}
                </span>
              </div>
              <div>
                <span className="text-text-muted">Latency:</span>{" "}
                <span className="text-text-main font-mono">
                  TTFT {selectedDetail.latency?.ttft || 0}ms / Total{" "}
                  {selectedDetail.latency?.total || 0}ms
                </span>
              </div>
              <div>
                <span className="text-text-muted">Input Tokens:</span>{" "}
                <span className="text-text-main font-mono">
                  {getInputTokens(selectedDetail.tokens).toLocaleString()}
                </span>
              </div>
              {getCachedTokens(selectedDetail.tokens) > 0 && (
                <div>
                  <span className="text-text-muted">Cached Tokens:</span>{" "}
                  <span className="text-text-main font-mono">
                    {getCachedTokens(selectedDetail.tokens).toLocaleString()}
                  </span>
                </div>
              )}
              {getCacheCreationTokens(selectedDetail.tokens) > 0 && (
                <div>
                  <span className="text-text-muted">Cache Creation:</span>{" "}
                  <span className="text-text-main font-mono">
                    {getCacheCreationTokens(
                      selectedDetail.tokens,
                    ).toLocaleString()}
                  </span>
                </div>
              )}
              <div>
                <span className="text-text-muted">Output Tokens:</span>{" "}
                <span className="text-text-main font-mono">
                  {selectedDetail.tokens?.completion_tokens?.toLocaleString() ||
                    0}
                </span>
              </div>
            </div>

            {selectedDetail.pxpipe && (
              <div className="rounded-lg border border-black/5 dark:border-white/5 p-4">
                <div className="flex items-center gap-2 mb-2">
                  <ImageIcon className="size-[18px] text-text-muted" />
                  <span className="font-semibold text-sm text-text-main">
                    PXPIPE
                  </span>
                  <span
                    className={cn(
                      "text-xs px-2 py-0.5 rounded",
                      selectedDetail.pxpipe.applied
                        ? "bg-green-500/15 text-green-600"
                        : "bg-amber-500/15 text-amber-600",
                    )}
                  >
                    {selectedDetail.pxpipe.applied ? "Activated" : "Skipped"}
                  </span>
                </div>
                {selectedDetail.pxpipe.applied ? (
                  <div className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
                    <div>
                      <span className="text-text-muted block text-xs">
                        Original (est.)
                      </span>
                      <span className="font-mono">
                        {(
                          selectedDetail.pxpipe.tokensBeforeEst || 0
                        ).toLocaleString()}{" "}
                        tokens
                      </span>
                    </div>
                    <div>
                      <span className="text-text-muted block text-xs">
                        Compressed (est.)
                      </span>
                      <span className="font-mono">
                        {(
                          selectedDetail.pxpipe.tokensAfterEst || 0
                        ).toLocaleString()}{" "}
                        tokens
                      </span>
                    </div>
                    <div>
                      <span className="text-text-muted block text-xs">
                        Saved
                      </span>
                      <span className="font-mono text-green-600">
                        {selectedDetail.pxpipe.savedPct || 0}%
                      </span>
                    </div>
                    <div>
                      <span className="text-text-muted block text-xs">
                        Images
                      </span>
                      <span className="font-mono">
                        {selectedDetail.pxpipe.imageCount || 0} (
                        {selectedDetail.pxpipe.durationMs || 0}ms)
                      </span>
                    </div>
                  </div>
                ) : (
                  <p className="text-sm text-text-muted">
                    Reason:{" "}
                    <span className="font-mono">
                      {selectedDetail.pxpipe.reason}
                    </span>
                    {selectedDetail.pxpipe.detail
                      ? ` — ${selectedDetail.pxpipe.detail}`
                      : ""}
                  </p>
                )}
              </div>
            )}

            <div className="space-y-4">
              <CollapsibleSection
                title="1. Client Request (Input)"
                defaultOpen={true}
                icon={<LogIn className="size-[18px] text-text-muted" />}
              >
                <pre className="max-h-[300px] max-w-full overflow-auto rounded-lg border border-black/5 bg-black/5 p-3 font-mono text-xs text-text-main dark:border-white/5 dark:bg-white/5 sm:p-4">
                  {JSON.stringify(selectedDetail.request, null, 2)}
                </pre>
              </CollapsibleSection>

              {selectedDetail.providerRequest !== undefined && (
                <CollapsibleSection
                  title="2. Provider Request (Translated)"
                  icon={<Languages className="size-[18px] text-text-muted" />}
                >
                  <pre className="max-h-[300px] max-w-full overflow-auto rounded-lg border border-black/5 bg-black/5 p-3 font-mono text-xs text-text-main dark:border-white/5 dark:bg-white/5 sm:p-4">
                    {JSON.stringify(selectedDetail.providerRequest, null, 2)}
                  </pre>
                </CollapsibleSection>
              )}

              {selectedDetail.providerResponse !== undefined && (
                <CollapsibleSection
                  title="3. Provider Response (Raw)"
                  icon={<Braces className="size-[18px] text-text-muted" />}
                >
                  <pre className="max-h-[300px] max-w-full overflow-auto rounded-lg border border-black/5 bg-black/5 p-3 font-mono text-xs text-text-main dark:border-white/5 dark:bg-white/5 sm:p-4">
                    {typeof selectedDetail.providerResponse === "object"
                      ? JSON.stringify(selectedDetail.providerResponse, null, 2)
                      : String(selectedDetail.providerResponse)}
                  </pre>
                </CollapsibleSection>
              )}

              <CollapsibleSection
                title="4. Client Response (Final)"
                defaultOpen={true}
                icon={<LogOut className="size-[18px] text-text-muted" />}
              >
                {selectedDetail.response?.thinking && (
                  <div className="mb-4">
                    <h4 className="font-semibold text-text-main mb-2 flex items-center gap-2 text-xs uppercase tracking-wide opacity-70">
                      <Brain className="size-4" />
                      Thinking Process
                    </h4>
                    <pre className="max-h-[200px] max-w-full overflow-auto rounded-lg border border-amber-200 bg-amber-50 p-3 font-mono text-xs text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-100 sm:p-4">
                      {selectedDetail.response.thinking}
                    </pre>
                  </div>
                )}

                <h4 className="font-semibold text-text-main mb-2 text-xs uppercase tracking-wide opacity-70">
                  Content
                </h4>
                <pre className="max-h-[300px] max-w-full overflow-auto rounded-lg border border-black/5 bg-black/5 p-3 font-mono text-xs text-text-main dark:border-white/5 dark:bg-white/5 sm:p-4">
                  {selectedDetail.response?.content || "[No content]"}
                </pre>
              </CollapsibleSection>
            </div>
          </div>
        )}
      </Drawer>
    </div>
  );
}
