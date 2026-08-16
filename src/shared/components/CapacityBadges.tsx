"use client";

import { CAPACITY_META } from "@/shared/constants/models";
import Tooltip from "./Tooltip";

// Render small icon badges for a model's capabilities (only those set true).
// colorOverride: force a single color class for all badges (default: per-cap color).
// size: icon font-size in px (default 16).
export interface CapacityBadgesProps {
  caps?: Record<string, unknown> | null;
  className?: string;
  colorOverride?: string;
  size?: number;
}

type CapacityMetaKey = keyof typeof CAPACITY_META;

export default function CapacityBadges({
  caps,
  className = "",
  colorOverride,
  size = 16,
}: CapacityBadgesProps) {
  if (!caps) return null;
  const active = (Object.keys(CAPACITY_META) as CapacityMetaKey[]).filter(
    (k) => Boolean(caps[k]),
  );
  if (active.length === 0) return null;

  return (
    <span className={`inline-flex items-center gap-0.5 ${className}`}>
      {active.map((k) => {
        const meta = CAPACITY_META[k];
        return (
          <Tooltip
            key={k}
            text={`${meta.label} — ${meta.desc}`}
          >
            <span
              className={`material-symbols-outlined leading-none cursor-help ${colorOverride || meta.color}`}
              style={{ fontSize: `${size}px` }}
            >
              {meta.icon}
            </span>
          </Tooltip>
        );
      })}
    </span>
  );
}
