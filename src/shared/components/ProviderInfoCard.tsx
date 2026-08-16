"use client";

import Card from "./Card";

interface FieldSchemaItem {
  label: string;
  format: (v: unknown) => string;
  mono?: boolean;
  isLink?: boolean;
}

// Only show fields user actually cares about
const FIELD_SCHEMA: Record<string, FieldSchemaItem> = {
  mode: { label: "Mode", format: (v) => String(v) },
  defaultModel: { label: "Model", format: (v) => String(v), mono: true },
  baseUrl: { label: "Endpoint", format: (v) => String(v), isLink: true, mono: true },
  costPerQuery: {
    label: "Cost / call",
    format: (v) => {
      const num = Number(v);
      return num === 0 ? "Free" : `$${num.toFixed(4)}`;
    },
  },
  pricingUrl: { label: "Pricing", format: () => "View pricing", isLink: true },
  freeTier: { label: "Free tier", format: (v) => String(v) },
  freeMonthlyQuota: {
    label: "Free quota",
    format: (v) => {
      const num = Number(v);
      return num === 0 ? "—" : num >= 999999 ? "Unlimited" : `${num.toLocaleString()} / mo`;
    },
  },
  searchTypes: {
    label: "Types",
    format: (v) => (Array.isArray(v) ? v.join(", ") : String(v)),
  },
  formats: {
    label: "Formats",
    format: (v) => (Array.isArray(v) ? v.join(", ") : String(v)),
  },
  maxMaxResults: { label: "Max results", format: (v) => String(v) },
  maxCharacters: {
    label: "Max chars",
    format: (v) => Number(v).toLocaleString(),
  },
};

export interface ProviderNotice {
  apiKeyUrl?: string;
  text?: string;
}

export interface ProviderObj {
  notice?: ProviderNotice;
  website?: string;
}

export interface ProviderInfoCardProps {
  config?: Record<string, unknown> | null;
  provider?: ProviderObj | null;
  title?: string;
}

export default function ProviderInfoCard({
  config,
  provider,
  title = "Provider Info",
}: ProviderInfoCardProps) {
  if (!config) return null;

  const rows = Object.entries(FIELD_SCHEMA)
    .filter(
      ([key]) =>
        config[key] !== undefined && config[key] !== null && config[key] !== "",
    )
    .map(([key, schema]) => ({
      key,
      label: schema.label,
      value: schema.format(config[key]),
      isLink: schema.isLink,
      mono: schema.mono,
      raw: String(config[key]),
    }));

  const signupUrl = provider?.notice?.apiKeyUrl || provider?.website;
  const noticeText = provider?.notice?.text;

  return (
    <Card>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-lg font-semibold">{title}</h2>
        {signupUrl && (
          <a
            href={signupUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-primary hover:underline inline-flex items-center gap-1"
          >
            <span className="material-symbols-outlined text-sm">
              open_in_new
            </span>
            Get API Key
          </a>
        )}
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2">
        {rows.map((r) => (
          <div key={r.key} className="flex items-center gap-3 min-w-0">
            <span className="text-xs text-text-muted w-28 shrink-0">
              {r.label}
            </span>
            {r.isLink ? (
              <a
                href={r.raw}
                target="_blank"
                rel="noopener noreferrer"
                className={`text-sm text-primary hover:underline truncate ${r.mono ? "font-mono" : ""}`}
              >
                {r.value}
              </a>
            ) : (
              <span
                className={`text-sm text-text-main truncate ${r.mono ? "font-mono" : ""}`}
              >
                {r.value}
              </span>
            )}
          </div>
        ))}
        {noticeText && (
          <div className="flex items-start gap-3 min-w-0 sm:col-span-2">
            <span className="text-xs text-text-muted w-28 shrink-0 mt-0.5">
              Notice
            </span>
            <span className="text-sm text-text-main leading-relaxed">
              {noticeText}
            </span>
          </div>
        )}
      </div>
    </Card>
  );
}
