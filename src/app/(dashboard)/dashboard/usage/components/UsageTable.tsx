"use client";
/* eslint-disable react-hooks/set-state-in-effect */

import { useState, useEffect, useCallback, useMemo, Fragment, type ReactNode } from "react";
import Badge from "@/shared/components/Badge";
import { ArrowDown, ArrowUp, ChevronRight, ChevronsUpDown } from "lucide-react";

const fmt = (n: number | string | undefined | null) => new Intl.NumberFormat().format(Number(n) || 0);
const fmtCost = (n: number | string | undefined | null) => `$${(Number(n) || 0).toFixed(2)}`;

function fmtTime(iso: string | number | null | undefined): string {
  if (!iso) return "Never";
  const diffMins = Math.floor((Date.now() - new Date(iso).getTime()) / 60000);
  if (diffMins < 1) return "Just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffMins < 1440) return `${Math.floor(diffMins / 60)}h ago`;
  return new Date(iso).toLocaleDateString();
}

interface SortIconProps {
  field: string;
  currentSort: string;
  currentOrder: string;
}

function SortIcon({ field, currentSort, currentOrder }: SortIconProps) {
  if (currentSort !== field)
    return <ChevronsUpDown className="size-3.5 opacity-30" />;
  return currentOrder === "asc" ? (
    <ArrowUp className="size-3.5" />
  ) : (
    <ArrowDown className="size-3.5" />
  );
}

export interface UsageItemData {
  key?: string;
  promptTokens?: number;
  cachedTokens?: number;
  completionTokens?: number;
  totalTokens?: number;
  inputCost?: number;
  cachedCost?: number;
  outputCost?: number;
  totalCost?: number;
  cost?: number;
  pending?: number;
  [key: string]: unknown;
}

interface ValueCellsProps {
  item: UsageItemData;
  viewMode: string;
  isSummary?: boolean;
}

/**
 * Render 3 token or cost cells based on viewMode
 */
function ValueCells({ item, viewMode, isSummary = false }: ValueCellsProps) {
  if (viewMode === "tokens") {
    return (
      <>
        <td className="px-6 py-3 text-right text-text-muted">
          {isSummary && item.promptTokens === undefined
            ? "—"
            : fmt(item.promptTokens)}
        </td>
        <td className="px-6 py-3 text-right text-text-muted">
          {item.cachedTokens ? fmt(item.cachedTokens) : "—"}
        </td>
        <td className="px-6 py-3 text-right text-text-muted">
          {isSummary && item.completionTokens === undefined
            ? "—"
            : fmt(item.completionTokens)}
        </td>
        <td className="px-6 py-3 text-right font-medium">
          {fmt(item.totalTokens)}
        </td>
      </>
    );
  }
  return (
    <>
      <td className="px-6 py-3 text-right text-text-muted">
        {isSummary && item.inputCost === undefined
          ? "—"
          : fmtCost(item.inputCost)}
      </td>
      <td className="px-6 py-3 text-right text-text-muted">
        {item.cachedCost ? fmtCost(item.cachedCost) : "—"}
      </td>
      <td className="px-6 py-3 text-right text-text-muted">
        {isSummary && item.outputCost === undefined
          ? "—"
          : fmtCost(item.outputCost)}
      </td>
      <td className="px-6 py-3 text-right font-medium text-warning">
        {fmtCost(item.totalCost || item.cost)}
      </td>
    </>
  );
}

export interface UsageTableColumn {
  field: string;
  label: string;
  align?: "left" | "right";
}

export interface UsageGroupedData<T = UsageItemData> {
  groupKey: string;
  summary: UsageItemData;
  items: T[];
}

export interface UsageTableProps<T = UsageItemData> {
  title?: string;
  columns: UsageTableColumn[];
  groupedData: UsageGroupedData<T>[];
  tableType: string;
  sortBy: string;
  sortOrder: string;
  onToggleSort: (tableType: string, field: string) => void;
  viewMode: string;
  storageKey: string;
  renderDetailCells: (item: T) => ReactNode;
  renderSummaryCells: (group: UsageGroupedData<T>) => ReactNode;
  emptyMessage: string;
}

export default function UsageTable<T extends UsageItemData = UsageItemData>({
  title,
  columns,
  groupedData,
  tableType,
  sortBy,
  sortOrder,
  onToggleSort,
  viewMode,
  storageKey,
  renderDetailCells,
  renderSummaryCells,
  emptyMessage,
}: UsageTableProps<T>) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  // Load expanded state from localStorage
  useEffect(() => {
    try {
      const saved = localStorage.getItem(storageKey);
      if (saved) setExpanded(new Set(JSON.parse(saved)));
    } catch (e) {
      console.error(`Failed to load ${storageKey}:`, e);
    }
  }, [storageKey]);

  // Save expanded state to localStorage
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify([...expanded]));
    } catch (e) {
      console.error(`Failed to save ${storageKey}:`, e);
    }
  }, [expanded, storageKey]);

  const toggleGroup = useCallback((groupKey: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(groupKey)) {
        next.delete(groupKey);
      } else {
        next.add(groupKey);
      }
      return next;
    });
  }, []);

  const valueColumns = useMemo(() => {
    if (viewMode === "tokens") {
      return [
        { field: "promptTokens", label: "Input Tokens" },
        { field: "cachedTokens", label: "Cached" },
        { field: "completionTokens", label: "Output Tokens" },
        { field: "totalTokens", label: "Total Tokens" },
      ];
    }
    return [
      { field: "promptTokens", label: "Input Cost" },
      { field: "cachedCost", label: "Cached Cost" },
      { field: "completionTokens", label: "Output Cost" },
      { field: "cost", label: "Total Cost" },
    ];
  }, [viewMode]);

  const totalColSpan = columns.length + valueColumns.length;

  return (
    <section className="min-w-0 overflow-hidden border-y border-border">
      {title && (
        <div className="border-b border-border py-4">
          <h3 className="font-semibold">{title}</h3>
        </div>
      )}
      <div className="overflow-x-auto">
        <table className="w-full min-w-max text-left text-sm">
          <thead className="text-xs uppercase text-text-muted">
            <tr>
              {columns.map((col) => (
                <th
                  key={col.field}
                  className={`px-6 py-3 ${col.align === "right" ? "text-right" : ""}`}
                  aria-sort={
                    sortBy === col.field
                      ? sortOrder === "asc"
                        ? "ascending"
                        : "descending"
                      : "none"
                  }
                >
                  <button
                    type="button"
                    onClick={() => onToggleSort(tableType, col.field)}
                    aria-label={`Sort by ${col.label}`}
                    className={`inline-flex items-center gap-1 hover:text-text-main ${col.align === "right" ? "justify-end" : ""}`}
                  >
                    {col.label}
                    <SortIcon
                      field={col.field}
                      currentSort={sortBy}
                      currentOrder={sortOrder}
                    />
                  </button>
                </th>
              ))}
              {valueColumns.map((col) => (
                <th
                  key={col.field}
                  className="px-6 py-3 text-right"
                  aria-sort={
                    sortBy === col.field
                      ? sortOrder === "asc"
                        ? "ascending"
                        : "descending"
                      : "none"
                  }
                >
                  <button
                    type="button"
                    onClick={() => onToggleSort(tableType, col.field)}
                    aria-label={`Sort by ${col.label}`}
                    className="inline-flex items-center justify-end gap-1 hover:text-text-main"
                  >
                    {col.label}
                    <SortIcon
                      field={col.field}
                      currentSort={sortBy}
                      currentOrder={sortOrder}
                    />
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {groupedData.map((group) => (
              <Fragment key={group.groupKey}>
                {/* Group summary row */}
                <tr className="group-summary hover:bg-bg-subtle/50 transition-colors">
                  <td className="px-6 py-3">
                    <button
                      type="button"
                      onClick={() => toggleGroup(group.groupKey)}
                      aria-expanded={expanded.has(group.groupKey)}
                      aria-label={`${expanded.has(group.groupKey) ? "Collapse" : "Expand"} ${group.groupKey}`}
                      className="flex w-full items-center gap-2 text-left"
                    >
                      <ChevronRight
                        className={`size-4 shrink-0 text-text-muted transition-transform ${expanded.has(group.groupKey) ? "rotate-90" : ""}`}
                      />
                      <span
                        className={`font-medium transition-colors ${(group.summary.pending ?? 0) > 0 ? "text-primary" : ""}`}
                      >
                        {group.groupKey}
                      </span>
                    </button>
                  </td>
                  {renderSummaryCells(group)}
                  <ValueCells
                    item={group.summary}
                    viewMode={viewMode}
                    isSummary
                  />
                </tr>
                {/* Detail rows */}
                {expanded.has(group.groupKey) &&
                  group.items.map((item) => (
                    <tr
                      key={`detail-${item.key}`}
                      className="group-detail hover:bg-bg-subtle/20 transition-colors"
                    >
                      {renderDetailCells(item)}
                      <ValueCells item={item} viewMode={viewMode} />
                    </tr>
                  ))}
              </Fragment>
            ))}
            {groupedData.length === 0 && (
              <tr>
                <td
                  colSpan={totalColSpan}
                  className="px-6 py-8 text-center text-text-muted"
                >
                  {emptyMessage}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

// Re-export utilities for use in UsageStats orchestrator
export { fmt, fmtCost, fmtTime };
