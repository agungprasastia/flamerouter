"use client";
/* eslint-disable react-hooks/immutability */

import { useState, useEffect } from "react";
import { MITM_TOOLS } from "@/shared/constants/cliTools";
import { getModelsByProviderId } from "@/shared/constants/models";
import {
  isOpenAICompatibleProvider,
  isAnthropicCompatibleProvider,
} from "@/shared/constants/providers";
import {
  MitmServerCard,
  MitmToolCard,
} from "@/app/(dashboard)/dashboard/cli-tools/components";
import type { MitmServerStatus } from "@/app/(dashboard)/dashboard/cli-tools/components/MitmServerCard";
import type { ApiKeyItem } from "@/app/(dashboard)/dashboard/cli-tools/components/ApiKeySelect";

interface ConnectionItem {
  id?: string;
  provider: string;
  isActive?: boolean;
  [key: string]: unknown;
}

export default function MitmPageClient() {
  const [connections, setConnections] = useState<ConnectionItem[]>([]);
  const [apiKeys, setApiKeys] = useState<ApiKeyItem[]>([]);
  const [modelAliases, setModelAliases] = useState<Record<string, string>>({});
  const [cloudEnabled, setCloudEnabled] = useState(false);
  const [expandedTool, setExpandedTool] = useState<string | null>(null);
  const [mitmStatus, setMitmStatus] = useState<MitmServerStatus>({
    running: false,
    certExists: false,
    dnsStatus: {},
    hasCachedPassword: false,
  });

  useEffect(() => {
    fetchConnections();
    fetchApiKeys();
    fetchAliases();
    fetchCloudSettings();
  }, []);

  const fetchConnections = async () => {
    try {
      const res = await fetch("/api/providers");
      if (res.ok) {
        const data = await res.json();
        setConnections(data.connections || []);
      }
    } catch {
      /* ignore */
    }
  };

  const fetchApiKeys = async () => {
    try {
      const res = await fetch("/api/keys");
      if (res.ok) {
        const data = await res.json();
        setApiKeys(data.keys || []);
      }
    } catch {
      /* ignore */
    }
  };

  const fetchAliases = async () => {
    try {
      const res = await fetch("/api/models/alias");
      if (res.ok) {
        const data = await res.json();
        setModelAliases(data.aliases || {});
      }
    } catch {
      /* ignore */
    }
  };

  const fetchCloudSettings = async () => {
    try {
      const res = await fetch("/api/settings");
      if (res.ok) {
        const data = await res.json();
        setCloudEnabled(data.cloudEnabled || false);
      }
    } catch {
      /* ignore */
    }
  };

  const getActiveProviders = () =>
    connections.filter((c) => c.isActive !== false);

  const hasActiveProviders = () => {
    const active = getActiveProviders();
    return active.some(
      (conn) =>
        getModelsByProviderId(conn.provider).length > 0 ||
        isOpenAICompatibleProvider(conn.provider) ||
        isAnthropicCompatibleProvider(conn.provider),
    );
  };

  const mitmTools = Object.entries(MITM_TOOLS);

  return (
    <div className="flex w-full flex-col gap-6">
      <div className="flex items-start gap-2 px-3 py-2 rounded-lg bg-yellow-500/10 border border-yellow-500/30">
        <span className="material-symbols-outlined text-[16px] text-yellow-500 mt-0.5 shrink-0">
          warning
        </span>
        <p className="text-xs text-red-600 dark:text-yellow-400 leading-relaxed">
          ⚠️ MITM intercepts HTTPS traffic of IDE tools (Antigravity, GitHub
          Copilot, Kiro) via local CA to redirect requests to your providers.
          May violate ToS → account ban. Use at your own risk.
        </p>
      </div>

      {/* MITM Server Card */}
      <MitmServerCard
        apiKeys={apiKeys}
        cloudEnabled={cloudEnabled}
        onStatusChange={setMitmStatus}
      />

      {/* Tool Cards */}
      <div className="grid gap-3 sm:gap-4">
        {mitmTools.map(([toolId, tool]) => (
          <MitmToolCard
            key={toolId}
            tool={tool}
            isExpanded={expandedTool === toolId}
            onToggle={() =>
              setExpandedTool(expandedTool === toolId ? null : toolId)
            }
            serverRunning={mitmStatus.running}
            dnsActive={Boolean(mitmStatus.dnsStatus?.[toolId])}
            hasCachedPassword={mitmStatus.hasCachedPassword || false}
            needsSudoPassword={mitmStatus.needsSudoPassword !== false}
            isWin={mitmStatus.isWin === true}
            apiKeys={apiKeys}
            activeProviders={getActiveProviders()}
            hasActiveProviders={hasActiveProviders()}
            modelAliases={modelAliases}
            cloudEnabled={cloudEnabled}
            onDnsChange={(data: unknown) => {
              const d = data as { dnsStatus?: Record<string, unknown> };
              setMitmStatus((prev) => ({
                ...prev,
                dnsStatus: d?.dnsStatus ?? prev.dnsStatus,
              }));
            }}
          />
        ))}
      </div>
    </div>
  );
}
