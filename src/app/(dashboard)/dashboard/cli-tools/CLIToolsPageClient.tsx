"use client";

import { useState, useEffect } from "react";
import { CardSkeleton } from "@/shared/components";
import { CLI_TOOLS, MITM_TOOLS } from "@/shared/constants/cliTools";
import { MitmLinkCard } from "./components";
import ToolSummaryCard, { type ToolStatusData } from "./components/ToolSummaryCard";

const ALL_STATUSES_URL = "/api/cli-tools/all-statuses";

export interface CLIToolsPageClientProps {
  machineId?: string;
}

export default function CLIToolsPageClient({ machineId }: CLIToolsPageClientProps) {
  const [loading, setLoading] = useState(true);
  const [toolStatuses, setToolStatuses] = useState<Record<string, ToolStatusData>>({});

  useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        const res = await fetch(ALL_STATUSES_URL);
        if (res.ok && mounted) setToolStatuses(await res.json());
      } catch (error) {
        console.log("Error fetching tool statuses:", error);
      } finally {
        if (mounted) setLoading(false);
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  if (loading) {
    return (
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 sm:gap-4">
        <CardSkeleton />
        <CardSkeleton />
        <CardSkeleton />
        <CardSkeleton />
        <CardSkeleton />
        <CardSkeleton />
      </div>
    );
  }

  const regularTools = Object.entries(CLI_TOOLS);
  const mitmTools = Object.entries(MITM_TOOLS);

  return (
    <div className="flex w-full flex-col gap-8 px-1 sm:px-0 workbench-reveal">
      <div className="workbench-intro">
        <div>
          <p className="workbench-kicker">Toolchain / Integrations</p>
          <h2 className="mt-3 text-3xl font-semibold tracking-[-0.04em]">
            Connect your working set.
          </h2>
        </div>
        <p className="text-sm leading-relaxed text-text-muted">
          Configure local AI tools against one governed FlameRouter endpoint.
        </p>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 sm:gap-4">
        {regularTools.map(([toolId, tool]) => (
          <ToolSummaryCard
            key={toolId}
            toolId={toolId}
            tool={tool}
            status={toolStatuses[toolId]}
          />
        ))}
      </div>
      <div className="flex flex-col gap-3 sm:gap-4">
        <div className="flex items-center gap-2 px-1">
          <span className="material-symbols-outlined text-[18px] text-primary">
            security
          </span>
          <h2 className="text-sm font-semibold text-text-main">MITM Tools</h2>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3 sm:gap-4">
          {mitmTools.map(([toolId, tool]) => (
            <MitmLinkCard key={toolId} tool={tool} />
          ))}
        </div>
      </div>
    </div>
  );
}
