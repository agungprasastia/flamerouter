"use client";

import PropTypes from "prop-types";
const fmt = (n) => new Intl.NumberFormat().format(n || 0);
const fmtCost = (n) => `$${(n || 0).toFixed(2)}`;

export default function OverviewCards({ stats }) {
  const metrics = [
    ["Total Requests", fmt(stats.totalRequests), ""],
    ["Input Tokens", fmt(stats.totalPromptTokens), "text-primary"],
    ["Cached Tokens", fmt(stats.totalCachedTokens), "text-info"],
    ["Output Tokens", fmt(stats.totalCompletionTokens), "text-success"],
    ["Est. Cost", `~${fmtCost(stats.totalCost)}`, "text-warning"],
  ];

  return (
    <div className="grid min-w-0 grid-cols-2 border-y border-border md:grid-cols-5">
      {metrics.map(([label, value, color], index) => (
        <div
          key={label}
          className={`min-w-0 py-4 pr-4 ${index === 4 ? "col-span-2 md:col-span-1" : ""} ${index % 2 === 1 ? "border-l border-border pl-4" : ""} ${index > 1 ? "border-t border-border md:border-t-0" : ""} ${index > 0 ? "md:border-l md:border-border md:px-4" : ""}`}
        >
          <span className="block text-[10px] font-mono uppercase tracking-[0.12em] text-text-muted">
            {label}
          </span>
          <span
            className={`mt-1 block truncate font-mono text-xl font-semibold tracking-tight tabular-nums ${color}`}
          >
            {value}
          </span>
          {index === 4 && (
            <span className="text-[9px] text-text-muted">
              Estimated billing
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

OverviewCards.propTypes = {
  stats: PropTypes.object.isRequired,
};
