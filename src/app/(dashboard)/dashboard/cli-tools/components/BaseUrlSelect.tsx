"use client";

import { useEffect, useMemo, useRef, useState, type ChangeEvent } from "react";
import { UPDATER_CONFIG } from "@/shared/constants/config";

const STORAGE_KEY = "flamerouter.cliToolEndpointPresets";
const CUSTOM_VALUE = "__custom__";
const SAVE_VALUE = "__save__";

export interface EndpointPreset {
  name: string;
  baseUrl: string;
}

export interface BaseUrlOption {
  value: string;
  label: string;
  url: string;
  saved?: boolean;
}

const ensureV1 = (url?: string | null): string => {
  const trimmed = (url || "").replace(/\/+$/, "");
  if (!trimmed) return "";
  return /\/v1$/.test(trimmed) ? trimmed : `${trimmed}/v1`;
};

const readSavedPresets = (): EndpointPreset[] => {
  if (typeof window === "undefined") return [];
  try {
    const raw = JSON.parse(window.localStorage.getItem(STORAGE_KEY) || "[]");
    if (!Array.isArray(raw)) return [];
    return raw.filter((p): p is EndpointPreset => Boolean(p?.name && p?.baseUrl));
  } catch {
    return [];
  }
};

const writeSavedPresets = (presets: EndpointPreset[]) => {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(presets));
};

export interface BuildOptionsParams {
  requiresExternalUrl?: boolean;
  tunnelEnabled?: boolean;
  tunnelPublicUrl?: string | null;
  tailscaleEnabled?: boolean;
  tailscaleUrl?: string | null;
  cloudEnabled?: boolean;
  cloudUrl?: string | null;
  savedPresets: EndpointPreset[];
  withV1?: boolean;
}

const buildOptions = ({
  requiresExternalUrl,
  tunnelEnabled,
  tunnelPublicUrl,
  tailscaleEnabled,
  tailscaleUrl,
  cloudEnabled,
  cloudUrl,
  savedPresets,
  withV1,
}: BuildOptionsParams): BaseUrlOption[] => {
  const opts: BaseUrlOption[] = [];
  const wrap = (url?: string | null) =>
    withV1 ? ensureV1(url) : (url || "").replace(/\/+$/, "");
  if (!requiresExternalUrl) {
    const localUrl = wrap(`http://127.0.0.1:${UPDATER_CONFIG.appPort}`);
    opts.push({ value: "local", label: localUrl, url: localUrl });
  }
  if (tunnelEnabled && tunnelPublicUrl) {
    const u = wrap(tunnelPublicUrl);
    opts.push({ value: "tunnel", label: u, url: u });
  }
  if (tailscaleEnabled && tailscaleUrl) {
    const u = wrap(tailscaleUrl);
    opts.push({ value: "tailscale", label: u, url: u });
  }
  if (cloudEnabled && cloudUrl) {
    const u = wrap(cloudUrl);
    opts.push({ value: "cloud", label: u, url: u });
  }
  savedPresets.forEach((p) => {
    opts.push({
      value: `saved:${p.name}`,
      label: p.baseUrl,
      url: p.baseUrl,
      saved: true,
    });
  });
  opts.push({ value: CUSTOM_VALUE, label: "Custom URL...", url: "" });
  return opts;
};

export interface BaseUrlSelectProps {
  value?: string | null;
  onChange: (value: string) => void;
  requiresExternalUrl?: boolean;
  tunnelEnabled?: boolean;
  tunnelPublicUrl?: string | null;
  tailscaleEnabled?: boolean;
  tailscaleUrl?: string | null;
  cloudEnabled?: boolean;
  cloudUrl?: string | null;
  withV1?: boolean;
}

export default function BaseUrlSelect({
  value,
  onChange,
  requiresExternalUrl = false,
  tunnelEnabled = false,
  tunnelPublicUrl = "",
  tailscaleEnabled = false,
  tailscaleUrl = "",
  cloudEnabled = false,
  cloudUrl = "",
  withV1 = true,
}: BaseUrlSelectProps) {
  const [savedPresets, setSavedPresets] = useState<EndpointPreset[]>([]);
  const [mode, setMode] = useState<string>("");
  const [customInput, setCustomInput] = useState<string>("");
  const initializedRef = useRef<boolean>(false);

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    setSavedPresets(readSavedPresets());
  }, []);

  const options = useMemo(
    () =>
      buildOptions({
        requiresExternalUrl,
        tunnelEnabled,
        tunnelPublicUrl,
        tailscaleEnabled,
        tailscaleUrl,
        cloudEnabled,
        cloudUrl,
        savedPresets,
        withV1,
      }),
    [
      requiresExternalUrl,
      tunnelEnabled,
      tunnelPublicUrl,
      tailscaleEnabled,
      tailscaleUrl,
      cloudEnabled,
      cloudUrl,
      savedPresets,
      withV1,
    ],
  );

  // Always default to first option (127.0.0.1) on mount, ignore persisted value
  useEffect(() => {
    if (initializedRef.current) return;
    if (options.length === 0) return;
    initializedRef.current = true;
    const first = options.find((o) => o.value !== CUSTOM_VALUE);
    if (first) {
      setMode(first.value);
      onChange(first.url);
    } else {
      setMode(CUSTOM_VALUE);
    }
  }, [options, onChange]);

  /* eslint-enable react-hooks/set-state-in-effect */
  const handleSelect = (e: ChangeEvent<HTMLSelectElement>) => {
    const next = e.target.value;
    if (next === SAVE_VALUE) {
      const trimmed = (value || "").trim();
      if (!trimmed) return;
      let defaultName = trimmed;
      try {
        defaultName = new URL(trimmed).host;
      } catch {}
      const name = window.prompt("Save endpoint as:", defaultName);
      if (!name?.trim()) return;
      const updated: EndpointPreset[] = [
        ...savedPresets.filter((p) => p.name !== name.trim()),
        { name: name.trim(), baseUrl: trimmed },
      ].sort((a, b) => a.name.localeCompare(b.name));
      setSavedPresets(updated);
      writeSavedPresets(updated);
      return;
    }
    setMode(next);
    if (next === CUSTOM_VALUE) {
      setCustomInput("");
      onChange("");
      return;
    }
    const opt = options.find((o) => o.value === next);
    if (opt) onChange(opt.url);
  };

  const handleCustomInput = (e: ChangeEvent<HTMLInputElement>) => {
    const v = e.target.value;
    setCustomInput(v);
    onChange(v);
  };

  const handleDeleteSaved = () => {
    if (!mode.startsWith("saved:")) return;
    const name = mode.slice(6);
    const updated = savedPresets.filter((p) => p.name !== name);
    setSavedPresets(updated);
    writeSavedPresets(updated);
    setMode(CUSTOM_VALUE);
    setCustomInput("");
    onChange("");
  };

  const isSaved = mode.startsWith("saved:");
  const isCustom = mode === CUSTOM_VALUE;
  const canSave = isCustom && (customInput || "").trim().length > 0;

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <select
          value={mode}
          onChange={handleSelect}
          className="flex-1 min-w-0 px-2 py-2 bg-surface rounded text-xs border border-border focus:outline-none focus:ring-1 focus:ring-primary/50 sm:py-1.5"
        >
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
          {canSave && <option value={SAVE_VALUE}>+ Save current as...</option>}
        </select>
        {isSaved && (
          <button
            type="button"
            onClick={handleDeleteSaved}
            className="p-1 text-text-muted hover:text-red-500 rounded transition-colors shrink-0"
            title="Delete saved endpoint"
          >
            <span className="material-symbols-outlined text-[14px]">
              delete
            </span>
          </button>
        )}
      </div>
      {isCustom && (
        <input
          type="text"
          value={customInput}
          onChange={handleCustomInput}
          placeholder={
            withV1 ? "https://example.com/v1" : "https://example.com"
          }
          className="w-full min-w-0 px-2 py-2 bg-surface rounded border border-border text-xs focus:outline-none focus:ring-1 focus:ring-primary/50 sm:py-1.5"
        />
      )}
    </div>
  );
}
