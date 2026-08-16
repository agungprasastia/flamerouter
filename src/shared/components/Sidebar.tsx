"use client";

import { useState, useEffect } from "react";
import PropTypes from "prop-types";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/shared/utils/cn";
import { APP_CONFIG, UPDATER_CONFIG } from "@/shared/constants/config";
import { MEDIA_PROVIDER_KINDS } from "@/shared/constants/providers";
import { useCopyToClipboard } from "@/shared/hooks/useCopyToClipboard";
import Button from "./Button";
import { ConfirmModal } from "./Modal";
import {
  Activity,
  ArrowUpCircle,
  Blocks,
  Flame,
  ChartNoAxesCombined,
  ChevronDown,
  CircleGauge,
  Clipboard,
  Database,
  Globe2,
  Image,
  Images,
  KeyRound,
  Languages,
  Layers3,
  Mic,
  Network,
  PanelLeftClose,
  PiggyBank,
  Power,
  Settings,
  Terminal,
  Video,
  Volume2,
  Waypoints,
} from "lucide-react";

// const VISIBLE_MEDIA_KINDS = ["embedding", "image", "imageToText", "tts", "stt", "webSearch", "webFetch", "video", "music"];
const VISIBLE_MEDIA_KINDS = ["embedding", "image", "video", "tts", "stt"];
// Combined entry: webSearch + webFetch share one page at /dashboard/media-providers/web
const COMBINED_WEB_ITEM = {
  id: "web",
  label: "Web Fetch & Search",
  icon: Globe2,
  href: "/dashboard/media-providers/web",
};

const mediaIcons = {
  embedding: Waypoints,
  image: Image,
  video: Video,
  tts: Volume2,
  stt: Mic,
};

const navItems = [
  { href: "/dashboard/endpoint", label: "Endpoint & Key", icon: KeyRound },
  { href: "/dashboard/providers", label: "Providers", icon: Database },
  // { href: "/dashboard/basic-chat", label: "Basic Chat", icon: "chat" }, // Hidden
  { href: "/dashboard/combos", label: "Combo & Vision Adapter", icon: Layers3 },
  { href: "/dashboard/usage", label: "Usage", icon: ChartNoAxesCombined },
  { href: "/dashboard/quota", label: "Quota Tracker", icon: CircleGauge },
  { href: "/dashboard/token-saver", label: "Token Saver", icon: PiggyBank },
  // { href: "/dashboard/pxpipe", label: "PXPIPE", icon: "image" },
  { href: "/dashboard/cli-tools", label: "CLI Tools", icon: Terminal },
];

const debugItems = [
  { href: "/dashboard/console-log", label: "Console Log", icon: Activity },
  { href: "/dashboard/translator", label: "Translator", icon: Languages },
];

const systemItems = [
  { href: "/dashboard/proxy-pools", label: "Proxy Pools", icon: Network },
  { href: "/dashboard/skills", label: "Skills", icon: Blocks },
];

export interface SidebarProps {
  onClose?: () => void;
}

export default function Sidebar({ onClose }: SidebarProps) {
  const pathname = usePathname();
  const [mediaOpen, setMediaOpen] = useState(false);
  const [isDisconnected, setIsDisconnected] = useState(false);
  const [updateInfo, setUpdateInfo] = useState(null);
  const [showUpdateModal, setShowUpdateModal] = useState(false);
  const [isUpdating, setIsUpdating] = useState(false);
  const [shutdownCountdown, setShutdownCountdown] = useState(0);
  const [enableTranslator, setEnableTranslator] = useState(false);
  const { copied, copy } = useCopyToClipboard(2000);

  const INSTALL_CMD = UPDATER_CONFIG.installCmdLatest;

  useEffect(() => {
    fetch("/api/settings")
      .then((res) => res.json())
      .then((data) => {
        if (data.enableTranslator) setEnableTranslator(true);
      })
      .catch(() => {});
  }, []);

  // Lazy check for new npm version on mount
  useEffect(() => {
    fetch("/api/version")
      .then((res) => res.json())
      .then((data) => {
        if (data.hasUpdate) setUpdateInfo(data);
      })
      .catch(() => {});
  }, []);

  const isActive = (href) => {
    if (href === "/dashboard/endpoint") {
      return (
        pathname === "/dashboard" || pathname.startsWith("/dashboard/endpoint")
      );
    }
    return pathname.startsWith(href);
  };

  // Open manual update panel (no countdown yet — user must click Copy to trigger shutdown)
  const handleUpdate = () => {
    setShowUpdateModal(false);
    setIsUpdating(true);
  };

  // Triggered by Copy button inside ManualUpdatePanel: copy + countdown + shutdown
  const handleCopyAndShutdown = async () => {
    try {
      await navigator.clipboard.writeText(INSTALL_CMD);
    } catch {
      /* clipboard blocked */
    }
    copy(INSTALL_CMD);
    let remaining = UPDATER_CONFIG.shutdownCountdownSec;
    setShutdownCountdown(remaining);
    const timer = setInterval(() => {
      remaining -= 1;
      setShutdownCountdown(remaining);
      if (remaining <= 0) {
        clearInterval(timer);
        fetch("/api/version/shutdown", { method: "POST" }).catch(() => {});
        setIsDisconnected(true);
      }
    }, 1000);
  };

  const handleCancelUpdate = () => {
    setIsUpdating(false);
    setShutdownCountdown(0);
  };

  // Note: legacy updater poll removed. New flow: copy install cmd + shutdown server,
  // user runs the command manually in another terminal.

  return (
    <>
      <aside className="flex w-[248px] flex-col border-r border-white/8 bg-sidebar text-[#eee9df] transition-colors duration-300 min-h-full">
        <div className="flex items-center justify-between px-6 pt-6 pb-3">
          <span className="text-[10px] font-mono font-semibold uppercase tracking-[0.24em] text-white/60">
            Routing workspace
          </span>
          <button
            type="button"
            onClick={onClose}
            className="grid size-8 place-items-center text-white/55 hover:text-white lg:hidden"
            aria-label="Close navigation"
          >
            <PanelLeftClose size={18} strokeWidth={1.75} aria-hidden="true" />
          </button>
          <span className="relative hidden size-2 rounded-full bg-primary before:absolute before:inset-[-3px] before:rounded-full before:border before:border-primary/35 lg:block" />
        </div>

        {/* Logo */}
        <div className="px-6 py-4 flex flex-col gap-2">
          <Link href="/dashboard" className="flex items-center gap-3">
            <div className="flex items-center justify-center size-10 rounded-[10px_2px_10px_2px] bg-primary">
              <Flame
                size={20}
                strokeWidth={1.75}
                className="text-white"
                aria-hidden="true"
              />
            </div>
            <div className="flex flex-col">
              <h1 className="text-lg font-semibold tracking-tight text-[#f6f1e7]">
                {APP_CONFIG.name}
              </h1>
              <span className="text-[10px] font-mono uppercase tracking-[0.12em] text-white/60">
                control plane / v{APP_CONFIG.version}
              </span>
            </div>
          </Link>
          {updateInfo && (
            <div className="flex flex-col gap-1.5 rounded p-1 -m-1">
              <span className="flex items-center gap-1 text-xs font-semibold text-green-600 dark:text-amber-500">
                <ArrowUpCircle
                  size={14}
                  strokeWidth={1.75}
                  aria-hidden="true"
                />
                New version available: v{updateInfo.latestVersion}
              </span>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setShowUpdateModal(true)}
                  className="px-2 py-1 rounded bg-green-600 hover:bg-green-700 dark:bg-amber-500 dark:hover:bg-amber-600 text-white text-[11px] font-semibold transition-colors cursor-pointer"
                >
                  Update now
                </button>
                <button
                  onClick={() => copy(INSTALL_CMD)}
                  title="Copy install command"
                  className="flex-1 text-left hover:opacity-80 transition-opacity cursor-pointer min-w-0"
                >
                  <code className="block text-[10px] text-green-600/80 dark:text-amber-400/70 font-mono truncate">
                    {copied ? "copied" : INSTALL_CMD}
                  </code>
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Navigation */}
        <nav className="flex-1 px-4 py-2 space-y-0.5 overflow-y-auto custom-scrollbar">
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                onClick={onClose}
                aria-current={isActive(item.href) ? "page" : undefined}
                className={cn(
                  "relative flex items-center gap-3 px-3 py-2 rounded-[3px] transition-colors group before:absolute before:left-0 before:h-5 before:w-0.5 before:bg-primary before:opacity-0",
                  isActive(item.href)
                    ? "bg-white/6 text-white before:opacity-100"
                    : "text-white/55 hover:bg-white/6 hover:text-white",
                )}
              >
                <Icon
                  size={18}
                  strokeWidth={1.75}
                  className={cn(
                    isActive(item.href)
                      ? "text-white"
                      : "text-white/45 group-hover:text-primary transition-colors",
                  )}
                  aria-hidden="true"
                />
                <span className="text-[13px] font-medium">{item.label}</span>
              </Link>
            );
          })}

          {/* System section */}
          <div className="pt-3 mt-2 space-y-0.5">
            <p className="px-3 text-[10px] font-mono font-semibold text-white/30 uppercase tracking-[0.16em] mb-2">
              System
            </p>

            {/* Media Providers accordion */}
            <button
              onClick={() => setMediaOpen((v) => !v)}
              aria-expanded={mediaOpen}
              aria-controls="media-provider-navigation"
              className={cn(
                "relative w-full flex items-center gap-3 px-3 py-2 rounded-[3px] transition-colors group before:absolute before:left-0 before:h-5 before:w-0.5 before:bg-primary before:opacity-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50",
                pathname.startsWith("/dashboard/media-providers")
                  ? "bg-white/6 text-white before:opacity-100"
                  : "text-white/55 hover:bg-white/6 hover:text-white",
              )}
            >
              <Images size={18} strokeWidth={1.75} aria-hidden="true" />
              <span className="text-[13px] font-medium flex-1 text-left">
                Media Providers
              </span>
              <ChevronDown
                size={14}
                strokeWidth={1.75}
                className="transition-transform"
                style={{
                  transform: mediaOpen ? "rotate(180deg)" : "rotate(0deg)",
                }}
                aria-hidden="true"
              />
            </button>
            {mediaOpen && (
              <div id="media-provider-navigation" className="pl-4">
                {MEDIA_PROVIDER_KINDS.filter((k) =>
                  VISIBLE_MEDIA_KINDS.includes(k.id),
                ).map((kind) => {
                  const Icon = mediaIcons[kind.id] || Images;
                  return (
                    <Link
                      key={kind.id}
                      href={`/dashboard/media-providers/${kind.id}`}
                      onClick={onClose}
                      aria-current={
                        pathname.startsWith(
                          `/dashboard/media-providers/${kind.id}`,
                        )
                          ? "page"
                          : undefined
                      }
                      className={cn(
                        "relative flex items-center gap-3 px-4 py-1.5 rounded-[3px] transition-colors group before:absolute before:left-0 before:h-4 before:w-0.5 before:bg-primary before:opacity-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50",
                        pathname.startsWith(
                          `/dashboard/media-providers/${kind.id}`,
                        )
                          ? "bg-white/6 text-white before:opacity-100"
                          : "text-white/45 hover:bg-white/6 hover:text-white",
                      )}
                    >
                      <Icon size={16} strokeWidth={1.75} aria-hidden="true" />
                      <span className="text-sm">{kind.label}</span>
                    </Link>
                  );
                })}
                <Link
                  key={COMBINED_WEB_ITEM.id}
                  href={COMBINED_WEB_ITEM.href}
                  onClick={onClose}
                  aria-current={
                    pathname.startsWith(COMBINED_WEB_ITEM.href)
                      ? "page"
                      : undefined
                  }
                  className={cn(
                    "relative flex items-center gap-3 px-4 py-1.5 rounded-[3px] transition-colors group before:absolute before:left-0 before:h-4 before:w-0.5 before:bg-primary before:opacity-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50",
                    pathname.startsWith(COMBINED_WEB_ITEM.href)
                      ? "bg-white/6 text-white before:opacity-100"
                      : "text-white/45 hover:bg-white/6 hover:text-white",
                  )}
                >
                  <COMBINED_WEB_ITEM.icon
                    size={16}
                    strokeWidth={1.75}
                    aria-hidden="true"
                  />
                  <span className="text-sm">{COMBINED_WEB_ITEM.label}</span>
                </Link>
              </div>
            )}

            {systemItems.map((item) => {
              const Icon = item.icon;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={onClose}
                  aria-current={isActive(item.href) ? "page" : undefined}
                  className={cn(
                    "relative flex items-center gap-3 px-3 py-2 rounded-[3px] transition-colors group before:absolute before:left-0 before:h-5 before:w-0.5 before:bg-primary before:opacity-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50",
                    isActive(item.href)
                      ? "bg-white/6 text-white before:opacity-100"
                      : "text-white/55 hover:bg-white/6 hover:text-white",
                  )}
                >
                  <Icon
                    size={18}
                    strokeWidth={1.75}
                    className={cn(
                      !isActive(item.href) &&
                        "group-hover:text-primary transition-colors",
                    )}
                    aria-hidden="true"
                  />
                  <span className="text-[13px] font-medium">{item.label}</span>
                </Link>
              );
            })}

            {/* Debug items (inside System section, before Settings) */}
            {debugItems.map((item) => {
              const show =
                item.href !== "/dashboard/translator" || enableTranslator;
              const Icon = item.icon;
              return show ? (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={onClose}
                  aria-current={isActive(item.href) ? "page" : undefined}
                  className={cn(
                    "relative flex items-center gap-3 px-3 py-2 rounded-[3px] transition-colors group before:absolute before:left-0 before:h-5 before:w-0.5 before:bg-primary before:opacity-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50",
                    isActive(item.href)
                      ? "bg-white/6 text-white before:opacity-100"
                      : "text-white/55 hover:bg-white/6 hover:text-white",
                  )}
                >
                  <Icon
                    size={18}
                    strokeWidth={1.75}
                    className={cn(
                      !isActive(item.href) &&
                        "group-hover:text-primary transition-colors",
                    )}
                    aria-hidden="true"
                  />
                  <span className="text-[13px] font-medium">{item.label}</span>
                </Link>
              ) : null;
            })}

            {/* Settings */}
            <Link
              href="/dashboard/profile"
              onClick={onClose}
              aria-current={
                isActive("/dashboard/profile") ? "page" : undefined
              }
              className={cn(
                "relative flex items-center gap-3 px-3 py-2 rounded-[3px] transition-colors group before:absolute before:left-0 before:h-5 before:w-0.5 before:bg-primary before:opacity-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50",
                isActive("/dashboard/profile")
                  ? "bg-white/6 text-white before:opacity-100"
                  : "text-white/55 hover:bg-white/6 hover:text-white",
              )}
            >
              <Settings
                size={18}
                strokeWidth={1.75}
                className={cn(
                  !isActive("/dashboard/profile") &&
                    "group-hover:text-primary transition-colors",
                )}
                aria-hidden="true"
              />
              <span className="text-[13px] font-medium">Settings</span>
            </Link>
          </div>
        </nav>
      </aside>

      {/* Update Confirmation Modal */}
      <ConfirmModal
        isOpen={showUpdateModal}
        onClose={() => setShowUpdateModal(false)}
        onConfirm={handleUpdate}
        title="Update FlameRouter"
        message={`Show install command for v${updateInfo?.latestVersion || ""}? You can copy it and shutdown to install manually.`}
        confirmText="Show Command"
        cancelText="Cancel"
        variant="primary"
      />

      {/* Disconnected / Updating Overlay */}
      {(isDisconnected || isUpdating) && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-6">
          {isUpdating ? (
            <ManualUpdatePanel
              latestVersion={updateInfo?.latestVersion}
              installCmd={INSTALL_CMD}
              copied={copied}
              onCopyAndShutdown={handleCopyAndShutdown}
              onCancel={handleCancelUpdate}
              countdown={shutdownCountdown}
              isDisconnected={isDisconnected}
            />
          ) : (
            <div className="text-center p-8">
              <div className="flex items-center justify-center size-16 rounded-full bg-red-500/20 text-red-500 mx-auto mb-4">
                <Power size={32} strokeWidth={1.75} aria-hidden="true" />
              </div>
              <h2 className="text-xl font-semibold text-white mb-2">
                Server Disconnected
              </h2>
              <p className="text-text-muted mb-6">
                The proxy server has been stopped.
              </p>
              <Button
                variant="secondary"
                onClick={() => globalThis.location.reload()}
              >
                Reload Page
              </Button>
            </div>
          )}
        </div>
      )}
    </>
  );
}

Sidebar.propTypes = {
  onClose: PropTypes.func,
};

interface ManualUpdatePanelProps {
  latestVersion?: string;
  installCmd: string;
  copied?: boolean;
  onCopyAndShutdown: () => void;
  onCancel: () => void;
  countdown?: number;
  isDisconnected?: boolean;
}

function ManualUpdatePanel({
  latestVersion,
  installCmd,
  copied,
  onCopyAndShutdown,
  onCancel,
  countdown,
  isDisconnected,
}: ManualUpdatePanelProps) {
  const isCountingDown = countdown > 0;
  return (
    <div className="w-full max-w-lg rounded-xl bg-neutral-900/95 border border-white/10 p-6 text-white">
      <div className="flex items-center gap-3 mb-4">
        <div className="flex items-center justify-center size-11 rounded-full bg-amber-500/20 text-amber-400">
          <Clipboard size={24} strokeWidth={1.75} aria-hidden="true" />
        </div>
        <div>
          <h2 className="text-lg font-semibold">
            Update FlameRouter{latestVersion ? ` to v${latestVersion}` : ""}
          </h2>
          <p className="text-xs text-white/60">
            {isDisconnected
              ? "Server stopped. Paste the command into a terminal to install."
              : isCountingDown
                ? `Command copied. Server will stop in ${countdown}s...`
                : "Click the button below to copy the install command and shutdown."}
          </p>
        </div>
      </div>

      <p className="text-sm text-white/80 mb-2">Install command:</p>
      <div className="w-full px-3 py-2 rounded bg-white/5 mb-4">
        <code className="text-xs font-mono text-amber-400 break-all">
          {installCmd}
        </code>
      </div>

      <ol className="text-xs text-white/70 space-y-1 list-decimal list-inside mb-4">
        <li>
          Click <strong>Copy & Shutdown</strong> below.
        </li>
        <li>Paste the command into your terminal and press Enter.</li>
        <li>
          Run{" "}
          <code className="px-1 rounded bg-white/10 text-green-400">
            flamerouter
          </code>{" "}
          again after install.
        </li>
      </ol>

      {isDisconnected ? (
        <Button
          variant="secondary"
          fullWidth
          onClick={() => globalThis.location.reload()}
        >
          Reload Page
        </Button>
      ) : (
        <div className="flex gap-2">
          <Button
            variant="secondary"
            onClick={onCancel}
            disabled={isCountingDown}
          >
            Cancel
          </Button>
          <Button
            variant="primary"
            fullWidth
            onClick={onCopyAndShutdown}
            disabled={isCountingDown}
          >
            {copied
              ? "Copied - shutting down..."
              : isCountingDown
                ? `Shutting down in ${countdown}s`
                : "Copy & Shutdown"}
          </Button>
        </div>
      )}
    </div>
  );
}
