"use client";

import { useState, useEffect } from "react";
import PropTypes from "prop-types";
import { Badge, Button, Toggle } from "@/shared/components";
import {
  ChevronDown,
  ChevronRight,
  CircleAlert,
  CircleCheck,
  FlaskConical,
  LoaderCircle,
  PauseCircle,
  PlugZap,
  Plus,
  SearchX,
  X,
} from "lucide-react";
import ProviderIcon from "@/shared/components/ProviderIcon";
import { getProviderIconSrc } from "@/shared/utils/providerIcon";
import { OAUTH_PROVIDERS, APIKEY_PROVIDERS } from "@/shared/constants/config";
import {
  FREE_PROVIDERS,
  FREE_TIER_PROVIDERS,
  WEB_COOKIE_PROVIDERS,
  OPENAI_COMPATIBLE_PREFIX,
  ANTHROPIC_COMPATIBLE_PREFIX,
} from "@/shared/constants/providers";
import Link from "next/link";
import { getErrorCode, getRelativeTime } from "@/shared/utils";
import { useNotificationStore } from "@/store/notificationStore";
import { useHeaderSearchStore } from "@/store/headerSearchStore";
import ModelAvailabilityBadge from "./components/ModelAvailabilityBadge";
import AddCompatibleModal from "./components/AddCompatibleModal";

interface PageConnection {
  id: string;
  name?: string;
  provider?: string;
  authType?: string;
  isActive?: boolean;
  testStatus?: string;
  errorCode?: string | number;
  lastError?: string;
  lastErrorType?: string;
  lastErrorAt?: string | null;
  priority?: number;
  [key: string]: unknown;
}

interface PageProviderNode {
  id: string;
  name?: string;
  type?: string;
  apiType?: string;
  baseUrl?: string;
  priority?: number;
  [key: string]: unknown;
}

interface ProviderMeta {
  id: string;
  name: string;
  color?: string;
  textIcon?: string;
  apiType?: string;
  noAuth?: boolean;
  hidden?: boolean;
  priority?: number;
  serviceKinds?: string[];
  authModes?: string[];
  [key: string]: unknown;
}

interface ProviderStats {
  connected: number;
  error: number;
  total: number;
  errorCode: string | null;
  errorTime: string | null;
  allDisabled: boolean;
}

function getStatusDisplay(
  connected: number,
  error: number,
  errorCode?: string | null,
) {
  const parts = [];
  if (connected > 0) {
    parts.push(
      <Badge key="connected" variant="success" size="sm" dot>
        {connected} Connected
      </Badge>,
    );
  }
  if (error > 0) {
    const errText = errorCode
      ? `${error} Error (${errorCode})`
      : `${error} Error`;
    parts.push(
      <Badge key="error" variant="error" size="sm" dot>
        {errText}
      </Badge>,
    );
  }
  if (parts.length === 0) {
    return <span className="text-text-muted">No connections</span>;
  }
  return parts;
}

function getConnectionErrorTag(connection?: PageConnection | null) {
  if (!connection) return null;

  const explicitType = connection.lastErrorType;
  if (explicitType === "runtime_error") return "RUNTIME";
  if (
    explicitType === "upstream_auth_error" ||
    explicitType === "auth_missing" ||
    explicitType === "token_refresh_failed" ||
    explicitType === "token_expired"
  )
    return "AUTH";
  if (explicitType === "upstream_rate_limited") return "429";
  if (explicitType === "upstream_unavailable") return "5XX";
  if (explicitType === "network_error") return "NET";

  const numericCode = Number(connection.errorCode);
  if (Number.isFinite(numericCode) && numericCode >= 400)
    return String(numericCode);

  const fromMessage = getErrorCode(connection.lastError);
  if (fromMessage === "401" || fromMessage === "403") return "AUTH";
  if (fromMessage && fromMessage !== "ERR") return fromMessage;

  const msg = (connection.lastError || "").toLowerCase();
  if (
    msg.includes("runtime") ||
    msg.includes("not runnable") ||
    msg.includes("not installed")
  )
    return "RUNTIME";
  if (
    msg.includes("invalid api key") ||
    msg.includes("token invalid") ||
    msg.includes("revoked") ||
    msg.includes("unauthorized")
  )
    return "AUTH";

  return "ERR";
}

const APIKEY_INITIAL_VISIBLE = 20;

export default function ProvidersPage() {
  const [connections, setConnections] = useState<PageConnection[]>([]);
  const [providerNodes, setProviderNodes] = useState<PageProviderNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [showAllApikey, setShowAllApikey] = useState(false);
  const [quickFilter, setQuickFilter] = useState("all");
  const [providerView, setProviderView] = useState("all");
  const [showAddCompatibleModal, setShowAddCompatibleModal] = useState(false);
  const [showAddAnthropicCompatibleModal, setShowAddAnthropicCompatibleModal] =
    useState(false);
  const [testingMode, setTestingMode] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<{
    summary?: { passed: number; failed: number; total: number };
    results?: Array<{
      connectionId?: string;
      valid?: boolean;
      connectionName?: string;
      provider?: string;
      latencyMs?: number;
      diagnosis?: { type?: string };
      [key: string]: unknown;
    }>;
    error?: string;
    mode?: string;
    [key: string]: unknown;
  } | null>(null);
  const notify = useNotificationStore();
  const searchQuery = useHeaderSearchStore((s) => s.query);
  const registerSearch = useHeaderSearchStore((s) => s.register);
  const unregisterSearch = useHeaderSearchStore((s) => s.unregister);

  useEffect(() => {
    registerSearch("Search providers...");
    return () => unregisterSearch();
  }, [registerSearch, unregisterSearch]);

  const matchSearch = (name: string) =>
    !searchQuery.trim() ||
    name.toLowerCase().includes(searchQuery.trim().toLowerCase());

  const sortByPriority = (
    entries: Array<[string, ProviderMeta]>,
    authType: string | string[],
  ) =>
    [...entries].sort(([ka, a], [kb, b]) => {
      const pa = a.priority ?? 999;
      const pb = b.priority ?? 999;
      if (pa !== pb) return pa - pb;
      const sa = getProviderStats(ka, authType);
      const sb = getProviderStats(kb, authType);
      const ca = sa.connected > 0 ? 1 : 0;
      const cb = sb.connected > 0 ? 1 : 0;
      if (ca !== cb) return cb - ca;
      return (a.name || "").localeCompare(b.name || "");
    });

  const sortItemsByPriority = (
    items: ProviderMeta[],
    authType: string | string[],
  ) =>
    [...items].sort((a, b) => {
      const pa = a.priority ?? 999;
      const pb = b.priority ?? 999;
      if (pa !== pb) return pa - pb;
      const sa = getProviderStats(a.id, authType);
      const sb = getProviderStats(b.id, authType);
      const ca = sa.connected > 0 ? 1 : 0;
      const cb = sb.connected > 0 ? 1 : 0;
      if (ca !== cb) return cb - ca;
      return (a.name || "").localeCompare(b.name || "");
    });

  useEffect(() => {
    const fetchData = async () => {
      setLoadError("");
      try {
        const [connectionsRes, nodesRes] = await Promise.all([
          fetch("/api/providers"),
          fetch("/api/provider-nodes"),
        ]);
        if (!connectionsRes.ok || !nodesRes.ok) throw new Error("Fetch failed");
        const connectionsData = await connectionsRes.json();
        const nodesData = await nodesRes.json();
        setConnections(connectionsData.connections || []);
        setProviderNodes(nodesData.nodes || []);
      } catch (error) {
        console.log("Error fetching data:", error);
        setLoadError("Provider registry could not be loaded.");
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, []);

  const getProviderStats = (
    providerId: string,
    authType: string | string[],
  ): ProviderStats => {
    const authTypes = Array.isArray(authType) ? authType : [authType];
    const providerConnections = connections.filter(
      (c) => c.provider === providerId && authTypes.includes(c.authType || ""),
    );

    const getEffectiveStatus = (conn: PageConnection) => {
      const isCooldown = Object.entries(conn).some(
        ([k, v]) =>
          k.startsWith("modelLock_") &&
          typeof v === "string" &&
          new Date(v).getTime() > Date.now(),
      );
      return conn.testStatus === "unavailable" && !isCooldown
        ? "active"
        : conn.testStatus;
    };

    const connected = providerConnections.filter((c) => {
      const status = getEffectiveStatus(c);
      return status === "active" || status === "success";
    }).length;

    const errorConns = providerConnections.filter((c) => {
      const status = getEffectiveStatus(c);
      return (
        status === "error" || status === "expired" || status === "unavailable"
      );
    });

    const error = errorConns.length;
    const total = providerConnections.length;
    const allDisabled =
      total > 0 && providerConnections.every((c) => c.isActive === false);

    const latestError = errorConns.sort(
      (a, b) => new Date(typeof b.lastErrorAt === "string" ? b.lastErrorAt : 0).getTime() - new Date(typeof a.lastErrorAt === "string" ? a.lastErrorAt : 0).getTime(),
    )[0];
    const errorCode = latestError ? getConnectionErrorTag(latestError) : null;
    const errorTime = latestError?.lastErrorAt
      ? getRelativeTime(latestError.lastErrorAt)
      : null;

    return { connected, error, total, errorCode, errorTime, allDisabled };
  };

  // Toggle all connections for a provider on/off. authType may be a single
  // string or an array (kiro counts oauth + api_key/apikey together).
  const handleToggleProvider = async (
    providerId: string,
    authType: string | string[],
    newActive: boolean,
  ) => {
    const authTypes = Array.isArray(authType) ? authType : [authType];
    const matches = (c: PageConnection) =>
      c.provider === providerId && authTypes.includes(c.authType || "");
    const providerConns = connections.filter(matches);
    setConnections((prev) =>
      prev.map((c) => (matches(c) ? { ...c, isActive: newActive } : c)),
    );
    await Promise.allSettled(
      providerConns.map((c) =>
        fetch(`/api/providers/${c.id}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ isActive: newActive }),
        }),
      ),
    );
  };

  const handleBatchTest = async (mode: string, providerId: string | null = null) => {
    if (testingMode) return;
    setTestingMode(mode === "provider" ? providerId : mode);
    setTestResults(null);
    try {
      const res = await fetch("/api/providers/test-batch", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode, providerId }),
      });
      const data = await res.json();
      setTestResults(data);
      if (data.summary) {
        const { passed, failed, total } = data.summary;
        if (failed === 0) notify.success(`All ${total} tests passed`);
        else notify.warning(`${passed}/${total} passed, ${failed} failed`);
      }
    } catch (error) {
      setTestResults({ error: "Test request failed" });
      notify.error("Provider test failed");
    } finally {
      setTestingMode(null);
    }
  };

  const compatibleProviders = providerNodes
    .filter((node) => node.type === "openai-compatible")
    .map((node) => ({
      id: node.id,
      name: node.name || "OpenAI Compatible",
      color: "#10A37F",
      textIcon: "OC",
      apiType: node.apiType,
    }))
    .filter((p) => matchSearch(p.name));

  const anthropicCompatibleProviders = providerNodes
    .filter((node) => node.type === "anthropic-compatible")
    .map((node) => ({
      id: node.id,
      name: node.name || "Anthropic Compatible",
      color: "#D97757",
      textIcon: "AC",
    }))
    .filter((p) => matchSearch(p.name));

  // Dual-auth providers (oauth + apikey) store API keys as authType "apikey"
  // (and sometimes "api_key"). Card stats must count both so totals match detail.
  // kiro has no authModes in registry but accepts both (headless uses "api_key").
  const dualAuthTypes = (info: ProviderMeta, key: string): string | string[] => {
    if (key === "kiro") return ["oauth", "apikey", "api_key"];
    const modes = info?.authModes;
    // Free-tier and API-key providers default to supporting apikey even when the
    // registry entry omits authModes (e.g. cloudflare-ai, byteplus, ollama,
    // vertex) — otherwise their apikey connections are invisible on the grid card.
    if (!Array.isArray(modes)) {
      return key in FREE_TIER_PROVIDERS || key in APIKEY_PROVIDERS
        ? ["oauth", "apikey", "api_key"]
        : "oauth";
    }
    if (!modes.includes("apikey")) return "oauth";
    return ["oauth", "apikey", "api_key"];
  };

  const oauthEntries = sortByPriority(
    (Object.entries(OAUTH_PROVIDERS) as Array<[string, ProviderMeta]>).filter(
      ([, info]) => !info.hidden && matchSearch(info.name),
    ),
    "oauth",
  );
  const freeEntries = (Object.entries(FREE_PROVIDERS) as Array<[string, ProviderMeta]>)
    .filter(([, info]) => !info.hidden && matchSearch(info.name))
    .sort(([, a], [, b]) => (b.noAuth ? 1 : 0) - (a.noAuth ? 1 : 0));
  // Free Tier cards may be oauth-only (e.g. kimchi) or dual-auth, so count via
  // dualAuthTypes per provider instead of a fixed "apikey" — otherwise oauth
  // connections are invisible here (mismatch with the detail page).
  const freeTierEntries = (Object.entries(FREE_TIER_PROVIDERS) as Array<[string, ProviderMeta]>)
    .filter(
      ([, info]) =>
        !info.hidden &&
        matchSearch(info.name) &&
        (info.serviceKinds ?? ["llm"]).includes("llm"),
    )
    .sort(([ka, a], [kb, b]) => {
      const pa = a.priority ?? 999;
      const pb = b.priority ?? 999;
      if (pa !== pb) return pa - pb;
      const noAuthDiff = (b.noAuth ? 1 : 0) - (a.noAuth ? 1 : 0);
      if (noAuthDiff !== 0) return noAuthDiff;
      const ca =
        getProviderStats(ka, dualAuthTypes(a, ka)).connected > 0 ? 0 : 1;
      const cb =
        getProviderStats(kb, dualAuthTypes(b, kb)).connected > 0 ? 0 : 1;
      if (ca !== cb) return ca - cb;
      return (a.name || "").localeCompare(b.name || "");
    });
  // API Key: connected providers first, then alphabetical by name
  const apikeyEntries = (Object.entries(APIKEY_PROVIDERS) as Array<[string, ProviderMeta]>)
    .filter(
      ([, info]) =>
        !info.hidden &&
        (info.serviceKinds ?? ["llm"]).includes("llm") &&
        matchSearch(info.name || ""),
    )
    .sort(([ka, a], [kb, b]) => {
      const ca = getProviderStats(ka, "apikey").total > 0 ? 0 : 1;
      const cb = getProviderStats(kb, "apikey").total > 0 ? 0 : 1;
      if (ca !== cb) return ca - cb;
      return (a.name || "").localeCompare(b.name || "");
    });
  const isApikeySearching = !!searchQuery.trim();
  const visibleApikeyEntries =
    isApikeySearching || showAllApikey
      ? apikeyEntries
      : apikeyEntries.slice(0, APIKEY_INITIAL_VISIBLE);
  const hiddenApikeyCount = apikeyEntries.length - APIKEY_INITIAL_VISIBLE;
  const isQuickFind = Boolean(searchQuery.trim());
  const quickResults = [
    ...compatibleProviders.map((provider) => ({
      key: `compatible-${provider.id}`,
      id: provider.id,
      provider: provider as ProviderMeta,
      stats: getProviderStats(provider.id, "apikey"),
      authType: "compatible",
      authTypes: "apikey",
      card: "api",
    })),
    ...anthropicCompatibleProviders.map((provider) => ({
      key: `anthropic-${provider.id}`,
      id: provider.id,
      provider: provider as ProviderMeta,
      stats: getProviderStats(provider.id, "apikey"),
      authType: "compatible",
      authTypes: "apikey",
      card: "api",
    })),
    ...oauthEntries.map(([id, provider]) => ({
      key: `oauth-${id}`,
      id,
      provider: { ...provider, id },
      stats: getProviderStats(id, dualAuthTypes(provider, id)),
      authType: "oauth",
      authTypes: dualAuthTypes(provider, id),
      card: "provider",
    })),
    ...freeEntries.map(([id, provider]) => ({
      key: `free-${id}`,
      id,
      provider: { ...provider, id },
      stats: getProviderStats(id, dualAuthTypes(provider, id)),
      authType: "free",
      authTypes: dualAuthTypes(provider, id),
      card: "provider",
    })),
    ...freeTierEntries.map(([id, provider]) => ({
      key: `freetier-${id}`,
      id,
      provider: { ...provider, id },
      stats: getProviderStats(id, dualAuthTypes(provider, id)),
      authType: "free",
      authTypes: dualAuthTypes(provider, id),
      card: "api",
    })),
    ...apikeyEntries.map(([id, provider]) => ({
      key: `apikey-${id}`,
      id,
      provider: { ...provider, id },
      stats: getProviderStats(id, "apikey"),
      authType: "apikey",
      authTypes: "apikey",
      card: "api",
    })),
  ].filter(({ stats, provider }) => {
    if (quickFilter === "connected") return stats.connected > 0;
    if (quickFilter === "errors") return stats.error > 0;
    if (quickFilter === "no-account") return Boolean(provider.noAuth);
    return true;
  });

  if (loading) {
    return (
      <div className="divide-y divide-border border-y border-border">
        {Array.from({ length: 6 }).map((_, index) => (
          <div
            key={index}
            className="grid gap-4 py-5 sm:grid-cols-[minmax(0,1.4fr)_minmax(8rem,.75fr)_minmax(0,1fr)_auto] sm:items-center"
          >
            <div className="h-8 w-48 animate-pulse rounded bg-surface-2" />
            <div className="h-4 w-28 animate-pulse rounded bg-surface-2" />
            <div className="h-5 w-32 animate-pulse rounded bg-surface-2" />
            <div className="h-6 w-12 animate-pulse rounded bg-surface-2" />
          </div>
        ))}
      </div>
    );
  }

  const hasAnyResult =
    oauthEntries.length > 0 ||
    freeEntries.length > 0 ||
    freeTierEntries.length > 0 ||
    apikeyEntries.length > 0 ||
    compatibleProviders.length > 0 ||
    anthropicCompatibleProviders.length > 0;

  return (
    <div className="flex min-w-0 flex-col gap-10 px-1 sm:px-0 workbench-reveal">
      <div className="editorial-intro">
        <div>
          <p className="workbench-kicker">Provider registry / Routing supply</p>
          <h2 className="mt-3 text-3xl font-semibold tracking-[-0.04em]">
            Build your model bench.
          </h2>
        </div>
        <p className="text-sm leading-relaxed text-text-muted">
          Connect upstream accounts, test availability, then compose resilient
          routes.
        </p>
      </div>
      {!isQuickFind && (
        <div className="-mx-1 flex flex-wrap items-center gap-1 border-b border-border bg-bg py-3 sm:gap-2 sm:px-1">
          {[['all', 'All providers'], ['oauth', 'OAuth'], ['apikey', 'API key'], ['free', 'Free tier'], ['custom', 'Custom']].map(([value, label]) => (
            <button
              key={value}
              type="button"
              onClick={() => setProviderView(value)}
              aria-pressed={providerView === value}
              className={`rounded-[4px] border px-3 py-2 text-xs font-medium transition-colors ${providerView === value ? "border-primary/50 bg-primary/10 text-primary" : "border-border text-text-muted hover:text-text-main"}`}
            >
              {label}
            </button>
          ))}
        </div>
      )}
      {loadError && (
        <div className="flex flex-col gap-4 border-y border-red-500/30 py-5 text-sm sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-3 text-red-500">
            <CircleAlert size={18} strokeWidth={1.75} />
            <span>{loadError}</span>
          </div>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => globalThis.location.reload()}
          >
            Retry
          </Button>
        </div>
      )}
      {!hasAnyResult && searchQuery.trim() && (
        <div className="flex flex-col items-center border-y border-border py-10 text-center">
          <SearchX
            size={32}
            strokeWidth={1.75}
            className="mb-3 text-text-muted"
          />
          <p className="text-text-muted text-sm">
            No providers match &ldquo;{searchQuery.trim()}&rdquo;
          </p>
        </div>
      )}

      {isQuickFind && hasAnyResult && (
        <section className="flex flex-col gap-4">
          <div className="flex flex-col gap-3 border-b border-border pb-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <p className="workbench-kicker">Quick find</p>
              <h2 className="mt-1 text-xl font-semibold">
                {quickResults.length} provider{quickResults.length === 1 ? "" : "s"} found
              </h2>
            </div>
            <div className="flex flex-wrap gap-1" role="group" aria-label="Filter provider results">
              {[
                ["all", "All"],
                ["connected", "Connected"],
                ["errors", "Errors"],
                ["no-account", "No account"],
              ].map(([value, label]) => (
                <button
                  key={value}
                  type="button"
                  onClick={() => setQuickFilter(value)}
                  aria-pressed={quickFilter === value}
                  className={`rounded-[4px] border px-2.5 py-1.5 text-xs transition-colors ${quickFilter === value ? "border-primary/50 bg-primary/10 text-primary" : "border-border text-text-muted hover:text-text-main"}`}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>
          {quickResults.length === 0 ? (
            <div className="border-y border-border py-8 text-center text-sm text-text-muted">
              No providers match this filter.
            </div>
          ) : (
            <div className="divide-y divide-border border-y border-border">
              {quickResults.map((result) =>
                result.card === "api" ? (
                  <ApiKeyProviderCard
                    key={`${result.id}-${result.authType}`}
                    providerId={result.id}
                    provider={result.provider}
                    stats={result.stats}
                    authType={result.authType}
                    onToggle={(active) =>
                      handleToggleProvider(result.id, result.authTypes, active)
                    }
                  />
                ) : (
                  <ProviderCard
                    key={`${result.id}-${result.authType}`}
                    providerId={result.id}
                    provider={result.provider}
                    stats={result.stats}
                    authType={result.authType}
                    onToggle={(active) =>
                      handleToggleProvider(result.id, result.authTypes, active)
                    }
                  />
                ),
              )}
            </div>
          )}
        </section>
      )}

      {/* Custom Providers (OpenAI/Anthropic Compatible) — dynamic */}
      {!isQuickFind && (providerView === "all" || providerView === "custom") && <section className="flex flex-col gap-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <h2 className="text-lg sm:text-xl font-semibold flex items-center gap-2 leading-tight">
            Custom Providers (OpenAI/Anthropic Compatible){" "}
          </h2>
          <div className="grid grid-cols-1 gap-2 sm:flex sm:w-auto">
            <Button
              size="sm"
              icon={<Plus size={18} strokeWidth={1.75} />}
              onClick={() => setShowAddAnthropicCompatibleModal(true)}
              className="w-full sm:w-auto"
            >
              Add Anthropic Compatible
            </Button>
            <Button
              size="sm"
              variant="secondary"
              icon={<Plus size={18} strokeWidth={1.75} />}
              onClick={() => setShowAddCompatibleModal(true)}
              className="w-full !bg-white !text-black hover:!bg-gray-100 sm:w-auto"
            >
              Add OpenAI Compatible
            </Button>
          </div>
        </div>
        {compatibleProviders.length === 0 &&
        anthropicCompatibleProviders.length === 0 ? (
          <div className="flex items-center justify-center gap-2 border-y border-border py-5 text-sm text-text-muted">
            <PlugZap size={18} strokeWidth={1.75} />
            <span>
              No custom providers — use buttons above to add OpenAI/Anthropic
              compatible endpoints
            </span>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 border-y border-border py-3 xl:grid-cols-2">
            {[...compatibleProviders, ...anthropicCompatibleProviders].map(
              (info) => (
                <ApiKeyProviderCard
                  key={info.id}
                  providerId={info.id}
                  provider={info}
                  stats={getProviderStats(info.id, "apikey")}
                  authType="compatible"
                  onToggle={(active) =>
                    handleToggleProvider(info.id, "apikey", active)
                  }
                />
              ),
            )}
          </div>
        )}
      </section>}

      {/* OAuth Providers */}
      {!isQuickFind && (providerView === "all" || providerView === "oauth") && oauthEntries.length > 0 && (
        <section className="flex flex-col gap-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="text-lg sm:text-xl font-semibold flex items-center gap-2 leading-tight">
              OAuth Providers
            </h2>
            <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
              <ModelAvailabilityBadge />
              <button
                onClick={() => handleBatchTest("oauth")}
                disabled={!!testingMode}
                className={`flex w-full items-center justify-center gap-1.5 rounded-lg border px-3 py-2 text-xs font-medium transition-colors sm:w-auto sm:py-1.5 ${
                  testingMode === "oauth"
                    ? "bg-primary/20 border-primary/40 text-primary animate-pulse"
                    : "bg-bg border-border text-text-muted hover:text-text-main hover:border-primary/40"
                }`}
                title="Test all OAuth connections"
                aria-label="Test all OAuth connections"
              >
                {testingMode === "oauth" ? (
                  <LoaderCircle size={18} strokeWidth={1.75} className="animate-spin" />
                ) : (
                  <FlaskConical size={18} strokeWidth={1.75} />
                )}
                {testingMode === "oauth" ? "Testing..." : "Test All"}
              </button>
            </div>
          </div>
          <div className="grid grid-cols-1 gap-3 border-y border-border py-3 xl:grid-cols-2">
            {oauthEntries.map(([key, info]) => {
              const authTypes = dualAuthTypes(info, key);
              return (
                <ProviderCard
                  key={key}
                  providerId={key}
                  provider={info}
                  stats={getProviderStats(key, authTypes)}
                  authType="oauth"
                  onToggle={(active) =>
                    handleToggleProvider(key, authTypes, active)
                  }
                />
              );
            })}
          </div>
        </section>
      )}

      {/* Free Tier Providers */}
      {!isQuickFind && (providerView === "all" || providerView === "free") && (freeEntries.length > 0 || freeTierEntries.length > 0) && (
        <section className="flex flex-col gap-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="text-lg sm:text-xl font-semibold flex items-center gap-2 leading-tight">
              Free Tier Providers
            </h2>
            <button
              onClick={() => handleBatchTest("free")}
              disabled={!!testingMode}
              className={`flex w-full items-center justify-center gap-1.5 rounded-lg border px-3 py-2 text-xs font-medium transition-colors sm:w-auto sm:py-1.5 ${
                testingMode === "free"
                  ? "bg-primary/20 border-primary/40 text-primary animate-pulse"
                  : "bg-bg border-border text-text-muted hover:text-text-main hover:border-primary/40"
              }`}
              title="Test all Free connections"
              aria-label="Test all Free provider connections"
            >
              {testingMode === "free" ? (
                <LoaderCircle size={18} strokeWidth={1.75} className="animate-spin" />
              ) : (
                <FlaskConical size={18} strokeWidth={1.75} />
              )}
              {testingMode === "free" ? "Testing..." : "Test All"}
            </button>
          </div>
          <div className="grid grid-cols-1 gap-3 border-y border-border py-3 xl:grid-cols-2">
            {freeEntries.map(([key, info]) => {
              // Dual-auth (e.g. kiro): count/toggle oauth + apikey/api_key so the
              // card total matches the provider detail page.
              const freeAuthTypes = dualAuthTypes(info, key);
              return (
                <ProviderCard
                  key={key}
                  providerId={key}
                  provider={info}
                  stats={getProviderStats(key, freeAuthTypes)}
                  authType="free"
                  onToggle={(active) =>
                    handleToggleProvider(key, freeAuthTypes, active)
                  }
                />
              );
            })}
            {freeTierEntries.map(([key, info]) => {
              const freeAuthTypes = dualAuthTypes(info, key);
              return (
                <ApiKeyProviderCard
                  key={key}
                  providerId={key}
                  provider={info}
                  stats={getProviderStats(key, freeAuthTypes)}
                  authType={
                    Array.isArray(freeAuthTypes)
                      ? (freeAuthTypes[0] ?? "apikey")
                      : freeAuthTypes
                  }
                  onToggle={(active) =>
                    handleToggleProvider(key, freeAuthTypes, active)
                  }
                />
              );
            })}
          </div>
        </section>
      )}

      {/* API Key Providers — fixed list */}
      {!isQuickFind && (providerView === "all" || providerView === "apikey") && apikeyEntries.length > 0 && (
        <section className="flex flex-col gap-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="text-lg sm:text-xl font-semibold flex items-center gap-2 leading-tight">
              API Key Providers{" "}
            </h2>
            <button
              onClick={() => handleBatchTest("apikey")}
              disabled={!!testingMode}
              className={`flex w-full items-center justify-center gap-1.5 rounded-lg border px-3 py-2 text-xs font-medium transition-colors sm:w-auto sm:py-1.5 ${
                testingMode === "apikey"
                  ? "bg-primary/20 border-primary/40 text-primary animate-pulse"
                  : "bg-bg border-border text-text-muted hover:text-text-main hover:border-primary/40"
              }`}
              title="Test all API Key connections"
              aria-label="Test all API Key connections"
            >
              {testingMode === "apikey" ? (
                <LoaderCircle size={18} strokeWidth={1.75} className="animate-spin" />
              ) : (
                <FlaskConical size={18} strokeWidth={1.75} />
              )}
              {testingMode === "apikey" ? "Testing..." : "Test All"}
            </button>
          </div>
          <div className="grid grid-cols-1 gap-3 border-y border-border py-3 xl:grid-cols-2">
            {visibleApikeyEntries.map(([key, info]) => (
              <ApiKeyProviderCard
                key={key}
                providerId={key}
                provider={{ ...info, id: key, name: info.name || key }}
                stats={getProviderStats(key, "apikey")}
                authType="apikey"
                onToggle={(active) =>
                  handleToggleProvider(key, "apikey", active)
                }
              />
            ))}
          </div>
          {!isApikeySearching && !showAllApikey && hiddenApikeyCount > 0 && (
            <button
              onClick={() => setShowAllApikey(true)}
              className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-dashed border-primary/40 px-3 py-2.5 text-sm font-medium text-primary transition-colors hover:border-primary hover:bg-primary/5"
            >
              <ChevronDown size={18} strokeWidth={1.75} />
              Show all {apikeyEntries.length} providers
            </button>
          )}
        </section>
      )}

      {/* Web Cookie Providers — use browser subscription cookie instead of API key */}
      {/* <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <h2 className="text-xl font-semibold flex items-center gap-2">
            Web Cookie Providers{" "}
          </h2>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {Object.entries(WEB_COOKIE_PROVIDERS).map(([key, info]) => (
            <ApiKeyProviderCard
              key={key}
              providerId={key}
              provider={info}
              stats={getProviderStats(key, "apikey")}
              authType="apikey"
              onToggle={(active) => handleToggleProvider(key, "apikey", active)}
            />
          ))}
        </div>
      </div> */}

      <AddCompatibleModal
        variant="openai"
        isOpen={showAddCompatibleModal}
        onClose={() => setShowAddCompatibleModal(false)}
        onCreated={(node: unknown) => {
          setProviderNodes((prev) => [...prev, node as PageProviderNode]);
          setShowAddCompatibleModal(false);
        }}
      />
      <AddCompatibleModal
        variant="anthropic"
        isOpen={showAddAnthropicCompatibleModal}
        onClose={() => setShowAddAnthropicCompatibleModal(false)}
        onCreated={(node: unknown) => {
          setProviderNodes((prev) => [...prev, node as PageProviderNode]);
          setShowAddAnthropicCompatibleModal(false);
        }}
      />

      {/* Test Results Modal */}
      {testResults && (
        <div
          className="fixed inset-0 z-50 flex items-start justify-center px-3 pt-[6vh] sm:pt-[10vh]"
          onClick={() => setTestResults(null)}
        >
          <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" />
          <div
            className="relative bg-surface border border-border rounded-xl w-full max-w-[600px] max-h-[86vh] sm:max-h-[80vh] overflow-y-auto shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="sticky top-0 z-10 flex items-center justify-between px-5 py-3 border-b border-border bg-surface/95 backdrop-blur-sm rounded-t-xl">
              <h3 className="font-semibold">Test Results</h3>
              <button
                onClick={() => setTestResults(null)}
                className="p-1 rounded-lg hover:bg-bg text-text-muted hover:text-text-main transition-colors"
                aria-label="Close test results"
              >
                <X size={18} strokeWidth={1.75} />
              </button>
            </div>
            <div className="p-5">
              <ProviderTestResultsView results={testResults} />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

interface ProviderCardProps {
  providerId: string;
  provider: ProviderMeta;
  stats: ProviderStats;
  authType?: string;
  onToggle: (active: boolean) => void;
}

function ProviderCard({
  providerId,
  provider,
  stats,
  authType,
  onToggle,
}: ProviderCardProps) {
  const { connected, error, errorCode, errorTime, allDisabled } = stats;
  const isNoAuth = !!provider.noAuth;

  return (
    <div
      className={`group grid min-w-0 gap-4 rounded-[8px] border border-border-subtle bg-surface/30 p-4 transition-colors hover:border-primary/30 hover:bg-surface-2/50 sm:grid-cols-[minmax(0,1.4fr)_minmax(8rem,.75fr)_minmax(0,1fr)_auto] sm:items-center ${allDisabled ? "opacity-50" : ""}`}
    >
      <Link
        href={`/dashboard/providers/${providerId}`}
        className="grid min-w-0 gap-4 sm:col-span-3 sm:grid-cols-[minmax(0,1.4fr)_minmax(8rem,.75fr)_minmax(0,1fr)] sm:items-center"
      >
        <div className="flex min-w-0 items-center gap-3">
            <div
              className="size-8 shrink-0 rounded-lg flex items-center justify-center"
              style={{
                backgroundColor: `${(provider.color && provider.color.length > 7) ? provider.color : (provider.color ? `${provider.color}15` : "transparent")}`,
              }}
            >
              <ProviderIcon
                src={`/providers/${provider.id}.png`}
                alt={provider.name}
                size={30}
                className="object-contain rounded-lg max-w-[32px] max-h-[32px]"
                fallbackText={
                  provider.textIcon || provider.id.slice(0, 2).toUpperCase()
                }
                fallbackColor={provider.color}
              />
            </div>
            <div className="min-w-0">
              <h3 className="truncate font-semibold">{provider.name}</h3>
              <span className="text-xs text-text-muted">{authType}</span>
            </div>
        </div>
        <div className="text-xs text-text-muted tabular-nums">
          {isNoAuth
            ? "No account required"
            : `${stats.total} account${stats.total === 1 ? "" : "s"}`}
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
          {allDisabled ? (
            <Badge variant="default" size="sm">
              <span className="flex items-center gap-1">
                <PauseCircle size={14} strokeWidth={1.75} /> Disabled
              </span>
            </Badge>
          ) : isNoAuth ? (
            <Badge variant="success" size="sm">
              <CircleCheck size={14} strokeWidth={1.75} /> Ready
            </Badge>
          ) : (
            <>
              {getStatusDisplay(connected, error, errorCode)}
              {errorTime && <span className="text-text-muted">{errorTime}</span>}
            </>
          )}
        </div>
      </Link>
      <div className="flex shrink-0 items-center justify-between gap-3 sm:justify-end">
        {stats.total > 0 && (
          <Toggle
            size="sm"
            checked={!allDisabled}
            onChange={(checked) => onToggle(checked)}
            aria-label={`${allDisabled ? "Enable" : "Disable"} ${provider.name}`}
          />
        )}
        <ChevronRight size={18} strokeWidth={1.75} className="text-text-muted" />
      </div>
    </div>
  );
}

ProviderCard.propTypes = {
  providerId: PropTypes.string.isRequired,
  provider: PropTypes.shape({
    id: PropTypes.string.isRequired,
    name: PropTypes.string.isRequired,
    color: PropTypes.string,
    textIcon: PropTypes.string,
  }).isRequired,
  stats: PropTypes.shape({
    connected: PropTypes.number,
    error: PropTypes.number,
    errorCode: PropTypes.string,
    errorTime: PropTypes.string,
    allDisabled: PropTypes.bool,
  }).isRequired,
  authType: PropTypes.string,
  onToggle: PropTypes.func,
};

interface ApiKeyProviderCardProps {
  providerId: string;
  provider: ProviderMeta;
  stats: ProviderStats;
  authType?: string;
  onToggle: (active: boolean) => void;
}

function ApiKeyProviderCard({
  providerId,
  provider,
  stats,
  authType,
  onToggle,
}: ApiKeyProviderCardProps) {
  const { connected, error, errorCode, errorTime, allDisabled } = stats;
  const isCompatible = providerId.startsWith(OPENAI_COMPATIBLE_PREFIX);
  const isAnthropicCompatible = providerId.startsWith(
    ANTHROPIC_COMPATIBLE_PREFIX,
  );
  const getIconPath = () => {
    if (isCompatible && provider.apiType)
      return provider.apiType === "responses"
        ? "/providers/oai-r.png"
        : "/providers/oai-cc.png";
    if (isAnthropicCompatible) return "/providers/anthropic-m.png";
    return getProviderIconSrc(provider.id);
  };

  return (
    <div
      className={`group grid min-w-0 gap-4 rounded-[8px] border border-border-subtle bg-surface/30 p-4 transition-colors hover:border-primary/30 hover:bg-surface-2/50 sm:grid-cols-[minmax(0,1.4fr)_minmax(8rem,.75fr)_minmax(0,1fr)_auto] sm:items-center ${allDisabled ? "opacity-50" : ""}`}
    >
      <Link
        href={`/dashboard/providers/${providerId}`}
        className="grid min-w-0 gap-4 sm:col-span-3 sm:grid-cols-[minmax(0,1.4fr)_minmax(8rem,.75fr)_minmax(0,1fr)] sm:items-center"
      >
        <div className="flex min-w-0 items-center gap-3">
            <div
              className="size-8 shrink-0 rounded-lg flex items-center justify-center"
              style={{
                backgroundColor: `${(provider.color && provider.color.length > 7) ? provider.color : (provider.color ? `${provider.color}15` : "transparent")}`,
              }}
            >
              <ProviderIcon
                src={getIconPath()}
                alt={provider.name}
                size={30}
                className="object-contain rounded-lg max-w-[30px] max-h-[30px]"
                fallbackText={
                  provider.textIcon || provider.id.slice(0, 2).toUpperCase()
                }
                fallbackColor={provider.color}
              />
            </div>
            <div className="min-w-0">
              <h3 className="truncate font-semibold">{provider.name}</h3>
              <span className="text-xs text-text-muted">{authType}</span>
            </div>
        </div>
        <div className="text-xs text-text-muted tabular-nums">
          {stats.total} account{stats.total === 1 ? "" : "s"}
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs">
          {allDisabled ? (
            <Badge variant="default" size="sm">
              <span className="flex items-center gap-1">
                <PauseCircle size={14} strokeWidth={1.75} /> Disabled
              </span>
            </Badge>
          ) : (
            <>
              {getStatusDisplay(connected, error, errorCode)}
              {isCompatible && (
                <Badge variant="default" size="sm">
                  {provider.apiType === "responses" ? "Responses" : "Chat"}
                </Badge>
              )}
              {isAnthropicCompatible && (
                <Badge variant="default" size="sm">
                  Messages
                </Badge>
              )}
              {errorTime && <span className="text-text-muted">{errorTime}</span>}
            </>
          )}
        </div>
      </Link>
      <div className="flex shrink-0 items-center justify-between gap-3 sm:justify-end">
        {stats.total > 0 && (
          <Toggle
            size="sm"
            checked={!allDisabled}
            onChange={(checked) => onToggle(checked)}
            aria-label={`${allDisabled ? "Enable" : "Disable"} ${provider.name}`}
          />
        )}
        <ChevronRight size={18} strokeWidth={1.75} className="text-text-muted" />
      </div>
    </div>
  );
}

ApiKeyProviderCard.propTypes = {
  providerId: PropTypes.string.isRequired,
  provider: PropTypes.shape({
    id: PropTypes.string.isRequired,
    name: PropTypes.string.isRequired,
    color: PropTypes.string,
    textIcon: PropTypes.string,
    apiType: PropTypes.string,
  }).isRequired,
  stats: PropTypes.shape({
    connected: PropTypes.number,
    error: PropTypes.number,
    errorCode: PropTypes.string,
    errorTime: PropTypes.string,
    allDisabled: PropTypes.bool,
  }).isRequired,
  authType: PropTypes.string,
  onToggle: PropTypes.func,
};

interface ProviderTestResultsViewProps {
  results: {
    mode?: string;
    results?: Array<{
      connectionId?: string;
      valid?: boolean;
      connectionName?: string;
      provider?: string;
      latencyMs?: number;
      diagnosis?: { type?: string };
      [key: string]: unknown;
    }>;
    summary?: {
      total: number;
      passed: number;
      failed: number;
    };
    error?: string;
    [key: string]: unknown;
  };
}

function ProviderTestResultsView({ results }: ProviderTestResultsViewProps) {
  if (results.error && !results.results) {
    return (
      <div className="text-center py-6">
        <CircleAlert size={32} strokeWidth={1.75} className="mx-auto mb-2 text-red-500" />
        <p className="text-sm text-red-400">{results.error}</p>
      </div>
    );
  }

  const { summary, mode } = results;
  const items = results.results || [];
  const modeLabel =
    (mode && {
      oauth: "OAuth",
      free: "Free",
      apikey: "API Key",
      provider: "Provider",
      all: "All",
    }[mode as "oauth" | "free" | "apikey" | "provider" | "all"]) || mode;

  return (
    <div className="flex min-w-0 flex-col gap-3">
      {summary && (
        <div className="flex flex-wrap items-center gap-2 text-xs mb-1 sm:gap-3">
          <span className="text-text-muted">{modeLabel} Test</span>
          <span className="px-2 py-0.5 rounded bg-emerald-500/15 text-emerald-400 font-medium">
            {summary.passed} passed
          </span>
          {summary.failed > 0 && (
            <span className="px-2 py-0.5 rounded bg-red-500/15 text-red-400 font-medium">
              {summary.failed} failed
            </span>
          )}
          <span className="text-text-muted sm:ml-auto">
            {summary.total} tested
          </span>
        </div>
      )}
      {items.map((r, i) => (
        <div
          key={r.connectionId || i}
          className="flex min-w-0 flex-wrap items-center gap-2 rounded-lg bg-black/[0.03] px-3 py-2 text-xs dark:bg-white/[0.03] sm:flex-nowrap"
        >
          {r.valid ? (
            <CircleCheck size={18} strokeWidth={1.75} className="text-emerald-500" />
          ) : (
            <CircleAlert size={18} strokeWidth={1.75} className="text-red-500" />
          )}
          <div className="min-w-0 flex-[1_1_160px]">
            <span className="block truncate font-medium sm:inline">
              {r.connectionName}
            </span>
            <span className="block truncate text-text-muted sm:ml-1.5 sm:inline">
              ({r.provider})
            </span>
          </div>
          {r.latencyMs !== undefined && (
            <span className="shrink-0 text-text-muted font-mono tabular-nums">
              {r.latencyMs}ms
            </span>
          )}
          <span
            className={`shrink-0 text-[10px] uppercase font-bold px-1.5 py-0.5 rounded ${
              r.valid
                ? "bg-emerald-500/15 text-emerald-400"
                : "bg-red-500/15 text-red-400"
            }`}
          >
            {r.valid ? "OK" : r.diagnosis?.type || "ERROR"}
          </span>
        </div>
      ))}
      {items.length === 0 && (
        <div className="text-center py-4 text-text-muted text-sm">
          No active connections found for this group.
        </div>
      )}
    </div>
  );
}

ProviderTestResultsView.propTypes = {
  results: PropTypes.shape({
    mode: PropTypes.string,
    results: PropTypes.array,
    summary: PropTypes.shape({
      total: PropTypes.number,
      passed: PropTypes.number,
      failed: PropTypes.number,
    }),
    error: PropTypes.string,
  }).isRequired,
};
