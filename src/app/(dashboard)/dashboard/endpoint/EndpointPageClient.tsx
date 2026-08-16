"use client";
/* eslint-disable react-hooks/immutability, react-hooks/set-state-in-effect */

import { useState, useEffect, useRef, useCallback } from "react";
import PropTypes from "prop-types";
import {
  Check,
  CheckCircle2,
  CircleAlert,
  CloudUpload,
  Code2,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  Globe2,
  KeyRound,
  LoaderCircle,
  Lock,
  LockKeyhole,
  Plus,
  Power,
  Square,
  Trash2,
  Users,
} from "lucide-react";
import {
  Button,
  Input,
  Modal,
  CardSkeleton,
  Toggle,
  ConfirmModal,
} from "@/shared/components";
import { useCopyToClipboard } from "@/shared/hooks/useCopyToClipboard";
import {
  TUNNEL_BENEFITS,
  TUNNEL_PING_INTERVAL_MS,
  TUNNEL_PING_MAX_MS,
  STATUS_POLL_FAST_MS,
  REACHABLE_MISS_THRESHOLD,
  CLIENT_PING_FAST_MS,
} from "./endpointConstants";
import { clientPingUrl, clientPingAny } from "./endpointPing";
import EndpointRow from "./components/EndpointRow";
import StatusAlert from "./components/StatusAlert";
import Tooltip from "./components/Tooltip";
import SecurityWarning from "./components/SecurityWarning";

const tunnelBenefitIcons = {
  public: Globe2,
  group: Users,
  code: Code2,
  lock: Lock,
};

function maskKey(key?: string, keyId?: string, machineId?: string) {
  if (key && typeof key === "string") {
    if (key.length <= 12) return "••••••••••••••••";
    return `${key.slice(0, 7)}...${key.slice(-4)}`;
  }
  if (keyId && machineId) {
    return `sk-${machineId.slice(0, 6)}...${keyId}`;
  }
  if (keyId) {
    return `sk-...${keyId}`;
  }
  return "••••••••••••••••";
}

export default function APIPageClient({ machineId }) {
  const [keys, setKeys] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showAddModal, setShowAddModal] = useState(false);
  const [newKeyName, setNewKeyName] = useState("");
  const [createdKey, setCreatedKey] = useState(null);
  const [confirmState, setConfirmState] = useState(null);

  const [requireApiKey, setRequireApiKey] = useState(false);
  const [requireLogin, setRequireLogin] = useState(true);
  const [hasPassword, setHasPassword] = useState(true);
  const [tunnelDashboardAccess, setTunnelDashboardAccess] = useState(false);

  // Cloudflare Tunnel state
  const [tunnelChecking, setTunnelChecking] = useState(true);
  const [tunnelEnabled, setTunnelEnabled] = useState(false);
  const [tunnelReachable, setTunnelReachable] = useState(false);
  const [tunnelUrl, setTunnelUrl] = useState("");
  const [tunnelPublicUrl, setTunnelPublicUrl] = useState("");
  const [tunnelLoading, setTunnelLoading] = useState(false);
  const [tunnelProgress, setTunnelProgress] = useState("");
  const [tunnelStatus, setTunnelStatus] = useState(null);
  const [showEnableTunnelModal, setShowEnableTunnelModal] = useState(false);
  const [showDisableTunnelModal, setShowDisableTunnelModal] = useState(false);

  // Tailscale state
  const [tsEnabled, setTsEnabled] = useState(false);
  const [tsReachable, setTsReachable] = useState(false);
  const [tsUrl, setTsUrl] = useState("");
  const [tsLoading, setTsLoading] = useState(false);
  const [tsProgress, setTsProgress] = useState("");
  const [tsStatus, setTsStatus] = useState(null);
  const [tsAuthUrl, setTsAuthUrl] = useState("");
  const [tsAuthLabel, setTsAuthLabel] = useState("");
  const [tsInstalled, setTsInstalled] = useState(null); // null=checking, true/false
  const [tsInstalling, setTsInstalling] = useState(false);
  const [tsInstallLog, setTsInstallLog] = useState([]);
  const [tsSudoPassword, setTsSudoPassword] = useState("");
  const [tsConnecting, setTsConnecting] = useState(false);
  const [showTsModal, setShowTsModal] = useState(false);
  const [showDisableTsModal, setShowDisableTsModal] = useState(false);
  const tsLogRef = useRef(null);

  // Debounce reachable=false: server may briefly return false during background refresh.
  // Only flip UI to "reconnecting" after N consecutive misses to avoid spinner flicker.
  const tunnelMissRef = useRef(0);
  const tsMissRef = useRef(0);
  // Browser-side reachable cache (independent of backend DNS quirks)
  const tunnelClientReachableRef = useRef(false);
  const tsClientReachableRef = useRef(false);
  // Track whether reachable=true was ever observed in this session.
  // Distinguishes "Checking..." (initial cold cache) from "Reconnecting..." (lost connection).
  const tunnelEverReachableRef = useRef(false);
  const tsEverReachableRef = useRef(false);
  const [tunnelEverReachable, setTunnelEverReachable] = useState(false);
  const [tsEverReachable, setTsEverReachable] = useState(false);

  // API key visibility toggle state
  const [visibleKeys, setVisibleKeys] = useState(new Set());
  const defaultKeyProvisionedRef = useRef(false);

  // Client-side local/remote detection (UI hint only, not a security gate)
  const [isRemoteHost, setIsRemoteHost] = useState(false);
  useEffect(() => {
    if (typeof window !== "undefined")
      setIsRemoteHost(
        !["localhost", "127.0.0.1", "::1"].includes(window.location.hostname),
      );
  }, []);

  const { copied, copy } = useCopyToClipboard();

  // Security gate: block remote exposure while dashboard uses default password or login is off.
  const isLoginUnsafe = !requireLogin || !hasPassword;
  const unsafeReason = !requireLogin
    ? 'Enable "Require login" and set a custom password before activating the tunnel.'
    : "Change the default dashboard password before activating the tunnel.";

  // Auto-scroll install log
  useEffect(() => {
    if (tsLogRef.current)
      tsLogRef.current.scrollTop = tsLogRef.current.scrollHeight;
  }, [tsInstallLog]);

  useEffect(() => {
    fetchData();
    loadSettings();
  }, []);

  // Status poll: only while degraded (not yet reachable). Stop once healthy to avoid spam.
  // Visibility re-check: refresh once when tab becomes visible.
  useEffect(() => {
    const anyEnabled = tunnelEnabled || tsEnabled;
    if (!anyEnabled) return;
    const tunnelHealthy = !tunnelEnabled || tunnelReachable;
    const tsHealthy = !tsEnabled || tsReachable;
    const allHealthy = tunnelHealthy && tsHealthy;
    const onVisible = () => {
      if (!document.hidden) syncTunnelStatus();
    };
    document.addEventListener("visibilitychange", onVisible);
    if (allHealthy)
      return () => document.removeEventListener("visibilitychange", onVisible);
    const timer = setInterval(() => {
      if (!document.hidden) syncTunnelStatus();
    }, STATUS_POLL_FAST_MS);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [tunnelEnabled, tsEnabled, tunnelReachable, tsReachable]);

  // Browser-side periodic ping: probes tunnel/tailscale URLs directly so UI stays
  // "reachable" even when backend DNS (1.1.1.1) hiccups on *.ts.net or *.trycloudflare.com.
  // Adaptive: slow when healthy, fast when degraded; pause when tab hidden.
  useEffect(() => {
    const probeBoth = async () => {
      if (document.hidden) return;
      if (tunnelEnabled && (tunnelUrl || tunnelPublicUrl)) {
        const ok = await clientPingAny(tunnelPublicUrl, tunnelUrl);
        tunnelClientReachableRef.current = ok;
        if (ok) {
          tunnelMissRef.current = 0;
          setTunnelReachable(true);
          if (!tunnelEverReachableRef.current) {
            tunnelEverReachableRef.current = true;
            setTunnelEverReachable(true);
          }
        } else {
          tunnelMissRef.current += 1;
          if (tunnelMissRef.current >= REACHABLE_MISS_THRESHOLD)
            setTunnelReachable(false);
        }
      } else {
        tunnelClientReachableRef.current = false;
      }
      if (tsEnabled && tsUrl) {
        const ok = await clientPingUrl(tsUrl);
        tsClientReachableRef.current = ok;
        if (ok) {
          tsMissRef.current = 0;
          setTsReachable(true);
          if (!tsEverReachableRef.current) {
            tsEverReachableRef.current = true;
            setTsEverReachable(true);
          }
        } else {
          tsMissRef.current += 1;
          if (tsMissRef.current >= REACHABLE_MISS_THRESHOLD)
            setTsReachable(false);
        }
      } else {
        tsClientReachableRef.current = false;
      }
    };
    const anyEnabled =
      (tunnelEnabled && (tunnelUrl || tunnelPublicUrl)) || (tsEnabled && tsUrl);
    if (!anyEnabled) return;
    probeBoth();
    const tunnelHealthy = !tunnelEnabled || tunnelReachable;
    const tsHealthy = !tsEnabled || tsReachable;
    if (tunnelHealthy && tsHealthy) return;
    const id = setInterval(probeBoth, CLIENT_PING_FAST_MS);
    return () => clearInterval(id);
  }, [
    tunnelEnabled,
    tunnelUrl,
    tunnelPublicUrl,
    tsEnabled,
    tsUrl,
    tunnelReachable,
    tsReachable,
  ]);

  // Client-side reachable only (server no longer probes; watchdog handles backend health).
  // Miss-debounce: only flip to false after N consecutive misses.
  const updateReachable = useCallback(
    (_unused, clientRef, missRef, setter, everRef, everSetter) => {
      const reachable = clientRef.current;
      if (reachable) {
        missRef.current = 0;
        setter(true);
        if (!everRef.current) {
          everRef.current = true;
          everSetter(true);
        }
      } else {
        missRef.current += 1;
        if (missRef.current >= REACHABLE_MISS_THRESHOLD) setter(false);
      }
    },
    [],
  );

  // Trust user intent (settingsEnabled): UI stays "enabled" while watchdog restarts process
  const syncTunnelStatus = async () => {
    try {
      const statusRes = await fetch("/api/tunnel/status", {
        cache: "no-store",
      });
      if (!statusRes.ok) return;
      const data = await statusRes.json();
      const tEnabled =
        data.tunnel?.settingsEnabled ?? data.tunnel?.enabled ?? false;
      const tUrl = data.tunnel?.tunnelUrl || "";
      setTunnelUrl(tUrl);
      setTunnelPublicUrl(data.tunnel?.publicUrl || "");
      setTunnelEnabled(tEnabled);
      updateReachable(
        null,
        tunnelClientReachableRef,
        tunnelMissRef,
        setTunnelReachable,
        tunnelEverReachableRef,
        setTunnelEverReachable,
      );

      const tsEn =
        data.tailscale?.settingsEnabled ?? data.tailscale?.enabled ?? false;
      const tsUrlVal = data.tailscale?.tunnelUrl || "";
      setTsUrl(tsUrlVal);
      setTsEnabled(tsEn);
      updateReachable(
        null,
        tsClientReachableRef,
        tsMissRef,
        setTsReachable,
        tsEverReachableRef,
        setTsEverReachable,
      );
    } catch {
      /* ignore poll errors */
    }
  };

  const loadSettings = async () => {
    setTunnelChecking(true);
    try {
      const [settingsRes, statusRes] = await Promise.all([
        fetch("/api/settings"),
        fetch("/api/tunnel/status", { cache: "no-store" }),
      ]);
      if (settingsRes.ok) {
        const data = await settingsRes.json();
        setRequireApiKey(data.requireApiKey || false);
        setRequireLogin(data.requireLogin !== false);
        setHasPassword(data.hasPassword || false);
        setTunnelDashboardAccess(data.tunnelDashboardAccess || false);
      }
      if (statusRes.ok) {
        const data = await statusRes.json();
        const tEnabled =
          data.tunnel?.settingsEnabled ?? data.tunnel?.enabled ?? false;
        const tUrl = data.tunnel?.tunnelUrl || "";
        setTunnelUrl(tUrl);
        setTunnelPublicUrl(data.tunnel?.publicUrl || "");
        setTunnelEnabled(tEnabled);
        updateReachable(
          null,
          tunnelClientReachableRef,
          tunnelMissRef,
          setTunnelReachable,
          tunnelEverReachableRef,
          setTunnelEverReachable,
        );

        const tsEn =
          data.tailscale?.settingsEnabled ?? data.tailscale?.enabled ?? false;
        const tsUrlVal = data.tailscale?.tunnelUrl || "";
        setTsUrl(tsUrlVal);
        setTsEnabled(tsEn);
        updateReachable(
          null,
          tsClientReachableRef,
          tsMissRef,
          setTsReachable,
          tsEverReachableRef,
          setTsEverReachable,
        );
      }
    } catch (error) {
      console.log("Error loading settings:", error);
    } finally {
      setTunnelChecking(false);
    }
  };

  const handleTunnelDashboardAccess = async (value) => {
    try {
      const res = await fetch("/api/settings", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tunnelDashboardAccess: value }),
      });
      if (res.ok) setTunnelDashboardAccess(value);
    } catch (error) {
      console.log("Error updating tunnelDashboardAccess:", error);
    }
  };

  const handleRequireApiKey = async (value) => {
    try {
      const res = await fetch("/api/settings", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ requireApiKey: value }),
      });
      if (res.ok) setRequireApiKey(value);
    } catch (error) {
      console.log("Error updating requireApiKey:", error);
    }
  };

  const fetchData = async () => {
    try {
      const fetchKeys = async () => {
        const res = await fetch("/api/keys");
        if (!res.ok) return [];
        const data = await res.json();
        return data.keys || [];
      };

      let existing = await fetchKeys();
      // Auto-provision a default key for first-time users so the endpoint works out of the box.
       if (existing.length === 0 && !defaultKeyProvisionedRef.current) {
         defaultKeyProvisionedRef.current = true;
        try {
          const createRes = await fetch("/api/keys", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ name: "Default Key" }),
          });
          if (createRes.ok) existing = await fetchKeys();
        } catch {
          /* fall through to empty render */
        }
      }
      setKeys(existing);
    } catch (error) {
      console.log("Error fetching data:", error);
    } finally {
      setLoading(false);
    }
  };

  // u2500u2500u2500 Cloudflare Tunnel handlers
  // Ping tunnel health until reachable. Race multiple URLs (shortlink + direct) — 1 OK is enough.
  const pingTunnelHealth = async (...urls) => {
    setTunnelLoading(true);
    setTunnelProgress("Waiting for tunnel ready...");
    const targets = urls.filter(Boolean).map((u) => `${u}/api/health`);
    const start = Date.now();
    while (Date.now() - start < TUNNEL_PING_MAX_MS) {
      await new Promise((r) => setTimeout(r, TUNNEL_PING_INTERVAL_MS));
      const ok = await Promise.any(
        targets.map(async (h) => {
          const p = await fetch(h, { mode: "cors", cache: "no-store" });
          if (p.ok) return true;
          throw new Error("not ready");
        }),
      ).catch(() => false);
      if (ok) {
        setTunnelEnabled(true);
        setTunnelLoading(false);
        setTunnelProgress("");
        return true;
      }
      // Every 5 pings (~10s), check if backend process still alive
      if ((Date.now() - start) % 10000 < TUNNEL_PING_INTERVAL_MS) {
        try {
          const statusRes = await fetch("/api/tunnel/status");
          if (statusRes.ok) {
            const status = await statusRes.json();
            if (!status.tunnel?.enabled) {
              setTunnelStatus({
                type: "error",
                message: "Tunnel process stopped unexpectedly.",
              });
              setTunnelLoading(false);
              setTunnelProgress("");
              return false;
            }
          }
        } catch {
          /* ignore */
        }
      }
    }
    setTunnelStatus({
      type: "error",
      message: "Tunnel created but not reachable. Please try again.",
    });
    setTunnelLoading(false);
    setTunnelProgress("");
    return false;
  };

  const handleEnableTunnel = async () => {
    setShowEnableTunnelModal(false);
    setTunnelLoading(true);
    setTunnelStatus(null);
    setTunnelProgress("Creating tunnel...");

    // Poll download progress while enable request is pending
    let polling = true;
    const pollProgress = async () => {
      while (polling) {
        try {
          const r = await fetch("/api/tunnel/status");
          if (r.ok) {
            const s = await r.json();
            if (s.download?.downloading) {
              setTunnelProgress(
                `Downloading cloudflared... ${s.download.progress}%`,
              );
            } else if (polling) {
              setTunnelProgress("Creating tunnel...");
            }
          }
        } catch {
          /* ignore */
        }
        await new Promise((r) => setTimeout(r, 1000));
      }
    };
    pollProgress();

    try {
      const res = await fetch("/api/tunnel/enable", { method: "POST" });
      polling = false;
      const data = await res.json();
      if (!res.ok) {
        setTunnelStatus({
          type: "error",
          message: data.error || "Failed to enable tunnel",
        });
        return;
      }

      const url = data.tunnelUrl;
      if (!url) {
        setTunnelStatus({ type: "error", message: "No tunnel URL returned" });
        return;
      }

      setTunnelUrl(url);
      setTunnelPublicUrl(data.publicUrl || "");
      await pingTunnelHealth(data.publicUrl, url);
    } catch (error) {
      setTunnelStatus({ type: "error", message: error.message });
    } finally {
      polling = false;
      setTunnelLoading(false);
      setTunnelProgress("");
    }
  };

  const handleDisableTunnel = async () => {
    setTunnelLoading(true);
    setTunnelStatus(null);
    try {
      const res = await fetch("/api/tunnel/disable", { method: "POST" });
      const data = await res.json();
      if (res.ok) {
        setTunnelEnabled(false);
        setTunnelUrl("");
        setShowDisableTunnelModal(false);
        setTunnelStatus({ type: "success", message: "Tunnel disabled" });
      } else {
        setTunnelStatus({
          type: "error",
          message: data.error || "Failed to disable tunnel",
        });
      }
    } catch (error) {
      setTunnelStatus({ type: "error", message: error.message });
    } finally {
      setTunnelLoading(false);
    }
  };

  // u2500u2500u2500 Tailscale handlers
  const checkTailscaleInstalled = async () => {
    setTsInstalled(null);
    try {
      const res = await fetch("/api/tunnel/tailscale-check");
      if (res.ok) {
        const data = await res.json();
        setTsInstalled(data.installed);
        return data;
      }
    } catch {
      /* ignore */
    }
    setTsInstalled(false);
    return { installed: false };
  };

  const handleInstallTailscale = async () => {
    setTsInstalling(true);
    setTsStatus(null);
    setTsInstallLog([]);
    try {
      const res = await fetch("/api/tunnel/tailscale-install", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sudoPassword: tsSudoPassword }),
      });
      setTsSudoPassword("");

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split("\n\n");
        buffer = parts.pop() || "";
        for (const part of parts) {
          const lines = part.split("\n");
          let event = "progress";
          let data = null;
          for (const line of lines) {
            if (line.startsWith("event: ")) event = line.slice(7).trim();
            if (line.startsWith("data: ")) {
              try {
                data = JSON.parse(line.slice(6));
              } catch {
                /* skip */
              }
            }
          }
          if (!data) continue;
          if (event === "progress") {
            setTsInstallLog((prev) => [...prev.slice(-50), data.message]);
          } else if (event === "done") {
            setTsInstalled(true);
            setTsInstalling(false);
            setShowTsModal(false);
            handleConnectTailscale();
            return;
          } else if (event === "error") {
            setTsStatus({
              type: "error",
              message: data.error || "Install failed",
            });
          }
        }
      }
    } catch (e) {
      setTsStatus({ type: "error", message: e.message });
    } finally {
      setTsInstalling(false);
    }
  };

  // Ping Tailscale health until reachable
  const pingTsHealth = async (url) => {
    setTsProgress("Waiting for Tailscale ready...");
    const healthUrl = `${url}/api/health`;
    const start = Date.now();
    while (Date.now() - start < TUNNEL_PING_MAX_MS) {
      await new Promise((r) => setTimeout(r, TUNNEL_PING_INTERVAL_MS));
      try {
        const ping = await fetch(healthUrl, {
          mode: "no-cors",
          cache: "no-store",
        });
        if (ping.ok || ping.type === "opaque") return true;
      } catch {
        /* not ready yet */
      }
    }
    return false;
  };

  // Show inline login button instead of auto-opening popup (browsers block popups
  // opened after async work because the user gesture is lost).
  const requestUserAuth = (url, label) => {
    setTsAuthUrl(url);
    setTsAuthLabel(label);
  };

  const clearUserAuth = () => {
    setTsAuthUrl("");
    setTsAuthLabel("");
  };

  const handleConnectTailscale = async () => {
    setShowTsModal(false);
    setTsConnecting(true);
    setTsLoading(true);
    setTsStatus(null);
    setTsProgress("Connecting...");
    clearUserAuth();
    try {
      const res = await fetch("/api/tunnel/tailscale-enable", {
        method: "POST",
      });
      const data = await res.json();

      if (res.ok && data.success) {
        setTsUrl(data.tunnelUrl || "");
        const reachable = await pingTsHealth(data.tunnelUrl);
        setTsEnabled(true);
        setTsStatus(
          reachable
            ? null
            : { type: "warning", message: "Connected but not reachable yet." },
        );
        return;
      }

      if (data.needsLogin && data.authUrl) {
        requestUserAuth(data.authUrl, "Open Login Page");
        setTsProgress('Login required — click "Open Login Page" to continue');
        for (let i = 0; i < 40; i++) {
          await new Promise((r) => setTimeout(r, 3000));
          try {
            const r2 = await fetch("/api/tunnel/tailscale-check");
            if (r2.ok) {
              const check = await r2.json();
              if (check.loggedIn) {
                clearUserAuth();
                setTsProgress("Starting funnel...");
                const res2 = await fetch("/api/tunnel/tailscale-enable", {
                  method: "POST",
                });
                const data2 = await res2.json();
                if (res2.ok && data2.success) {
                  setTsUrl(data2.tunnelUrl || "");
                  const ok2 = await pingTsHealth(data2.tunnelUrl);
                  setTsEnabled(true);
                  setTsStatus(
                    ok2
                      ? null
                      : {
                          type: "warning",
                          message: "Connected but not reachable yet.",
                        },
                  );
                } else if (data2.funnelNotEnabled && data2.enableUrl) {
                  await pollFunnelEnable(data2.enableUrl);
                } else {
                  setTsStatus({
                    type: "error",
                    message: data2.error || "Failed to start funnel",
                  });
                }
                return;
              }
            }
          } catch {
            /* retry */
          }
        }
        clearUserAuth();
        setTsStatus({
          type: "error",
          message: "Login timed out. Please try again.",
        });
        return;
      }

      if (data.funnelNotEnabled && data.enableUrl) {
        await pollFunnelEnable(data.enableUrl);
        return;
      }

      setTsStatus({
        type: "error",
        message: data.error || "Failed to connect",
      });
    } catch (error) {
      setTsStatus({ type: "error", message: error.message });
    } finally {
      setTsLoading(false);
      setTsConnecting(false);
      setTsProgress("");
      clearUserAuth();
    }
  };

  const pollFunnelEnable = async (enableUrl) => {
    requestUserAuth(enableUrl, "Open Funnel Settings");
    setTsProgress('Click "Open Funnel Settings" to enable Funnel...');
    for (let i = 0; i < 40; i++) {
      await new Promise((r) => setTimeout(r, 3000));
      try {
        const res = await fetch("/api/tunnel/tailscale-enable", {
          method: "POST",
        });
        const data = await res.json();
        if (res.ok && data.success) {
          clearUserAuth();
          setTsUrl(data.tunnelUrl || "");
          const ok3 = await pingTsHealth(data.tunnelUrl);
          setTsEnabled(true);
          setTsStatus(
            ok3
              ? null
              : {
                  type: "warning",
                  message: "Connected but not reachable yet.",
                },
          );
          return;
        }
        if (data.funnelNotEnabled) continue;
        if (data.error) {
          clearUserAuth();
          setTsStatus({ type: "error", message: data.error });
          return;
        }
      } catch {
        /* retry */
      }
    }
    clearUserAuth();
    setTsStatus({
      type: "error",
      message: "Timed out waiting for Funnel to be enabled.",
    });
  };

  const handleDisableTailscale = async () => {
    setTsLoading(true);
    setTsStatus(null);
    try {
      const res = await fetch("/api/tunnel/tailscale-disable", {
        method: "POST",
      });
      const data = await res.json();
      if (res.ok) {
        setTsEnabled(false);
        setTsUrl("");
        setShowDisableTsModal(false);
        setTsStatus({ type: "success", message: "Tailscale disabled" });
      } else {
        setTsStatus({
          type: "error",
          message: data.error || "Failed to disable Tailscale",
        });
      }
    } catch (e) {
      setTsStatus({ type: "error", message: e.message });
    } finally {
      setTsLoading(false);
    }
  };

  const handleOpenTsModal = async () => {
    setTsStatus(null);
    setTsInstallLog([]);
    const data = await checkTailscaleInstalled();
    if (data?.installed && data?.hasCachedPassword) {
      handleConnectTailscale();
    } else {
      setShowTsModal(true);
    }
  };

  const handleCreateKey = async () => {
    if (!newKeyName.trim()) return;

    try {
      const res = await fetch("/api/keys", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: newKeyName }),
      });
      const data = await res.json();

      if (res.ok) {
        setCreatedKey(data.key);
        await fetchData();
        setNewKeyName("");
        setShowAddModal(false);
      }
    } catch (error) {
      console.log("Error creating key:", error);
    }
  };

  const handleDeleteKey = async (id) => {
    setConfirmState({
      title: "Delete API Key",
      message: "Delete this API key?",
      onConfirm: async () => {
        setConfirmState(null);
        try {
          const res = await fetch(`/api/keys/${id}`, { method: "DELETE" });
          if (res.ok) {
            setKeys(keys.filter((k) => k.id !== id));
            setVisibleKeys((prev) => {
              const next = new Set(prev);
              next.delete(id);
              return next;
            });
          }
        } catch (error) {
          console.log("Error deleting key:", error);
        }
      },
    });
  };

  const handleToggleKey = async (id, isActive) => {
    try {
      const res = await fetch(`/api/keys/${id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ isActive }),
      });
      if (res.ok) {
        setKeys((prev) =>
          prev.map((k) => (k.id === id ? { ...k, isActive } : k)),
        );
      }
    } catch (error) {
      console.log("Error toggling key:", error);
    }
  };

  const maskKey = (fullKey) => {
    if (!fullKey || fullKey.length <= 10) return fullKey || "";
    return (
      fullKey.slice(0, 6) + "•".repeat(fullKey.length - 10) + fullKey.slice(-4)
    );
  };

  const toggleKeyVisibility = (keyId) => {
    setVisibleKeys((prev) => {
      const next = new Set(prev);
      if (next.has(keyId)) next.delete(keyId);
      else next.add(keyId);
      return next;
    });
  };

  const [baseUrl, setBaseUrl] = useState("/v1");

  // Hydration fix: Only access window on client side
  useEffect(() => {
    if (typeof window !== "undefined") {
      setBaseUrl(`${window.location.origin}/v1`);
    }
  }, []);

  if (loading) {
    return (
      <div className="flex flex-col gap-8">
        <CardSkeleton />
        <CardSkeleton />
      </div>
    );
  }

  const currentEndpoint = baseUrl;

  return (
    <div className="flex flex-col gap-8 editorial-reveal">
      <section className="editorial-intro">
        <div>
          <p className="editorial-kicker">Gateway / Control plane</p>
          <h2 className="mt-3 max-w-2xl text-3xl font-semibold leading-[0.98] tracking-[-0.045em] md:text-5xl">
            Your models. One live route.
          </h2>
          <p className="mt-4 max-w-2xl text-sm leading-6 text-text-muted">
            Copy a route into any OpenAI-compatible client, then control remote
            exposure and request authentication from one surface.
          </p>
        </div>
        <div className="border-l border-border pl-4 md:pl-6">
          <div className="flex items-center gap-2 text-xs font-mono uppercase tracking-[0.12em] text-text-muted">
            <span className="relative size-2 rounded-full bg-success before:absolute before:inset-[-3px] before:rounded-full before:border before:border-success/30" />
            Local gateway active
          </div>
          <code className="mt-3 block break-all text-sm text-text-main">
            {currentEndpoint}
          </code>
          <p className="mt-3 text-xs text-text-muted">
            {requireApiKey ? "API key required" : "API key optional"} · {requireLogin && hasPassword ? "Dashboard secured" : "Security action required"}
          </p>
        </div>
      </section>

      <div className="grid grid-cols-1 items-start gap-10 xl:grid-cols-[minmax(0,1.35fr)_minmax(18rem,0.65fr)]">
        <section className="min-w-0">
          <div className="flex items-end justify-between gap-4">
            <div>
              <p className="editorial-kicker">01 / Routes</p>
              <h2 className="mt-2 text-xl font-semibold tracking-tight">Endpoint ledger</h2>
            </div>
            <p className="hidden max-w-xs text-right text-xs leading-5 text-text-muted sm:block">
              Local route stays private. Remote routes require an explicit security posture.
            </p>
          </div>

          <div className="editorial-ledger mt-5 border-y border-border">
            {/* Local */}
            <EndpointRow
              label="Local"
              url={currentEndpoint}
              copyId="local_url"
              copied={copied}
              onCopy={copy}
            />
            {/* Cloudflare Tunnel */}
            <div className="grid min-w-0 grid-cols-1 gap-2 py-4 sm:grid-cols-[7rem_minmax(0,1fr)_auto] sm:items-center">
              <span
                className={`font-mono text-xs ${
                  tunnelEnabled
                    ? "text-primary"
                    : "text-text-muted"
                }`}
              >
                Tunnel
              </span>
              <div className="flex min-w-0 flex-wrap items-center gap-2 sm:col-span-2">
              {tunnelEnabled && !tunnelLoading && tunnelReachable ? (
                <>
                  <Input
                    value={`${tunnelPublicUrl || tunnelUrl}/v1`}
                    readOnly
                    className="min-w-0 font-mono text-sm"
                    inputClassName="border-0 bg-transparent px-0 shadow-none"
                  />
                  <button
                    onClick={() =>
                      copy(`${tunnelPublicUrl || tunnelUrl}/v1`, "tunnel_url")
                    }
                    aria-label="Copy Tunnel"
                    className="inline-flex min-h-10 items-center justify-center gap-2 rounded px-3 text-xs font-medium text-text-muted transition-colors hover:bg-black/5 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 dark:hover:bg-white/5"
                  >
                    {copied === "tunnel_url" ? <Check size={16} strokeWidth={1.75} aria-hidden="true" /> : <Copy size={16} strokeWidth={1.75} aria-hidden="true" />}
                    <span>{copied === "tunnel_url" ? "Copied" : "Copy"}</span>
                  </button>
                  <div className="ml-auto border-l border-border pl-2">
                    <button
                      onClick={() => setShowDisableTunnelModal(true)}
                      aria-label="Disable Tunnel"
                      className="shrink-0 rounded p-2 text-red-500 transition-colors hover:bg-red-500/10"
                      title="Disable Tunnel"
                    >
                      <Power size={18} strokeWidth={1.75} aria-hidden="true" />
                    </button>
                  </div>
                </>
              ) : tunnelEnabled && !tunnelLoading && !tunnelReachable ? (
                <>
                  <div className="flex-1 flex items-center gap-2 px-3 py-1.5 rounded border border-amber-300 dark:border-amber-800 bg-amber-500/5 text-sm text-amber-600 dark:text-amber-400">
                    <LoaderCircle size={16} className="animate-spin" aria-hidden="true" />
                    {tunnelEverReachable
                      ? "Tunnel reconnecting..."
                      : "Tunnel checking..."}
                  </div>
                  <div className="ml-auto border-l border-border pl-2">
                    <button
                      onClick={() => setShowDisableTunnelModal(true)}
                      aria-label="Disable Tunnel"
                      className="shrink-0 rounded p-2 text-red-500 transition-colors hover:bg-red-500/10"
                      title="Disable Tunnel"
                    >
                      <Power size={18} strokeWidth={1.75} aria-hidden="true" />
                    </button>
                  </div>
                </>
              ) : tunnelLoading ? (
                <>
                  <div className="flex-1 flex items-center gap-2 px-3 py-1.5 rounded border border-border bg-input text-sm text-text-muted">
                    <LoaderCircle size={16} className="animate-spin" aria-hidden="true" />
                    {tunnelProgress || "Creating tunnel..."}
                  </div>
                  <div className="ml-auto border-l border-border pl-2">
                    <button
                      onClick={() => {
                        setTunnelLoading(false);
                        setTunnelProgress("");
                      }}
                      aria-label="Stop Tunnel"
                      className="shrink-0 rounded p-2 text-red-500 transition-colors hover:bg-red-500/10"
                      title="Stop"
                    >
                      <Square size={18} strokeWidth={1.75} aria-hidden="true" />
                    </button>
                  </div>
                </>
              ) : tunnelStatus?.type === "error" ? (
                <>
                  <div className="flex-1 flex items-center gap-2 px-3 py-1.5 rounded border border-red-300 dark:border-red-800 bg-red-500/5 text-sm text-red-600 dark:text-red-400">
                    <CircleAlert size={16} aria-hidden="true" />
                    {tunnelStatus.message}
                  </div>
                  <Button
                    size="sm"
                    icon={<CloudUpload size={16} aria-hidden="true" />}
                    onClick={() => setShowEnableTunnelModal(true)}
                  >
                    Enable
                  </Button>
                </>
              ) : tunnelChecking ? (
                <>
                  <div className="flex-1 flex items-center gap-2 px-3 py-1.5 rounded border border-border bg-input text-sm text-text-muted">
                    <LoaderCircle size={16} className="animate-spin" aria-hidden="true" />
                    Checking...
                  </div>
                  <div className="ml-auto border-l border-border pl-2">
                    <button
                      onClick={() => setTunnelChecking(false)}
                      aria-label="Stop Tunnel check"
                      className="shrink-0 rounded p-2 text-red-500 transition-colors hover:bg-red-500/10"
                      title="Stop"
                    >
                      <Square size={18} strokeWidth={1.75} aria-hidden="true" />
                    </button>
                  </div>
                </>
              ) : (
                <Button
                  size="sm"
                  icon={<CloudUpload size={16} aria-hidden="true" />}
                  onClick={() => {
                    if (isLoginUnsafe) {
                      setTunnelStatus({
                        type: "error",
                        message: `Security required: ${unsafeReason}`,
                      });
                      return;
                    }
                    if (!requireApiKey) {
                      setTunnelStatus({
                        type: "error",
                        message:
                          'Security required: Enable "Require API key" before activating the tunnel.',
                      });
                      return;
                    }
                    setShowEnableTunnelModal(true);
                  }}
                >
                  Enable
                </Button>
              )}
              </div>
            </div>
            {/* Tailscale */}
            <div className="grid min-w-0 grid-cols-1 gap-2 py-4 sm:grid-cols-[7rem_minmax(0,1fr)_auto] sm:items-center">
              <span
                className={`font-mono text-xs ${
                  tsEnabled
                    ? "text-primary"
                    : "text-text-muted"
                }`}
              >
                Tailscale
              </span>
              <div className="flex min-w-0 flex-wrap items-center gap-2 sm:col-span-2">
              {tsEnabled && !tsLoading && tsReachable ? (
                <>
                  <Input
                    value={`${tsUrl}/v1`}
                    readOnly
                    className="min-w-0 font-mono text-sm"
                    inputClassName="border-0 bg-transparent px-0 shadow-none"
                  />
                  <button
                    onClick={() => copy(`${tsUrl}/v1`, "ts_url")}
                    aria-label="Copy Tailscale"
                    className="inline-flex min-h-10 items-center justify-center gap-2 rounded px-3 text-xs font-medium text-text-muted transition-colors hover:bg-black/5 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 dark:hover:bg-white/5"
                  >
                    {copied === "ts_url" ? <Check size={16} strokeWidth={1.75} aria-hidden="true" /> : <Copy size={16} strokeWidth={1.75} aria-hidden="true" />}
                    <span>{copied === "ts_url" ? "Copied" : "Copy"}</span>
                  </button>
                  <div className="ml-auto border-l border-border pl-2">
                    <button
                      onClick={() => setShowDisableTsModal(true)}
                      aria-label="Disable Tailscale"
                      className="shrink-0 rounded p-2 text-red-500 transition-colors hover:bg-red-500/10"
                      title="Disable Tailscale"
                    >
                      <Power size={18} strokeWidth={1.75} aria-hidden="true" />
                    </button>
                  </div>
                </>
              ) : tsEnabled && !tsLoading && !tsReachable ? (
                <>
                  <div className="flex-1 flex items-center gap-2 px-3 py-1.5 rounded border border-amber-300 dark:border-amber-800 bg-amber-500/5 text-sm text-amber-600 dark:text-amber-400">
                    <LoaderCircle size={16} className="animate-spin" aria-hidden="true" />
                    {tsEverReachable
                      ? "Tailscale reconnecting..."
                      : "Tailscale checking..."}
                  </div>
                  <div className="ml-auto border-l border-border pl-2">
                    <button
                      onClick={() => setShowDisableTsModal(true)}
                      aria-label="Disable Tailscale"
                      className="shrink-0 rounded p-2 text-red-500 transition-colors hover:bg-red-500/10"
                      title="Disable Tailscale"
                    >
                      <Power size={18} strokeWidth={1.75} aria-hidden="true" />
                    </button>
                  </div>
                </>
              ) : tsLoading || tsConnecting ? (
                <>
                  <div className="flex-1 flex items-center gap-2 px-3 py-1.5 rounded border border-border bg-input text-sm text-text-muted">
                    <LoaderCircle size={16} className="animate-spin" aria-hidden="true" />
                    {tsProgress || "Connecting..."}
                  </div>
                  {tsAuthUrl && (
                    <Button
                      size="sm"
                      icon={<ExternalLink size={16} aria-hidden="true" />}
                      onClick={() =>
                        window.open(
                          tsAuthUrl,
                          "tailscale_auth",
                          "width=600,height=700,noopener,noreferrer",
                        )
                      }
                    >
                      {tsAuthLabel || "Open"}
                    </Button>
                  )}
                  <div className="ml-auto border-l border-border pl-2">
                    <button
                      onClick={() => {
                        setTsLoading(false);
                        setTsConnecting(false);
                        setTsProgress("");
                        clearUserAuth();
                      }}
                      aria-label="Stop Tailscale connection"
                      className="shrink-0 rounded p-2 text-red-500 transition-colors hover:bg-red-500/10"
                      title="Stop"
                    >
                      <Square size={18} strokeWidth={1.75} aria-hidden="true" />
                    </button>
                  </div>
                </>
              ) : tsStatus?.type === "error" ? (
                <>
                  <div className="flex-1 flex items-center gap-2 px-3 py-1.5 rounded border border-red-300 dark:border-red-800 bg-red-500/5 text-sm text-red-600 dark:text-red-400">
                    <CircleAlert size={16} aria-hidden="true" />
                    {tsStatus.message}
                  </div>
                  <Button size="sm" icon={<LockKeyhole size={16} aria-hidden="true" />} onClick={handleOpenTsModal}>
                    Enable
                  </Button>
                </>
              ) : (
                <Button
                  size="sm"
                  icon={<LockKeyhole size={16} aria-hidden="true" />}
                  onClick={() => {
                    if (isLoginUnsafe) {
                      setTsStatus({
                        type: "error",
                        message: `Security required: ${unsafeReason}`,
                      });
                      return;
                    }
                    handleOpenTsModal();
                  }}
                  className="bg-primary hover:bg-primary-hover text-white!"
                >
                  Enable
                </Button>
              )}
              </div>
            </div>
          </div>

          {/* Pre-enable security gate banner */}
          {isLoginUnsafe && !tunnelEnabled && !tsEnabled && (
            <div className="mt-4">
              <SecurityWarning
                message={unsafeReason}
                action={{ label: "Open settings", href: "/dashboard/profile" }}
              />
            </div>
          )}

          {/* Security warnings when tunnel or tailscale is active */}
          {(tunnelEnabled || tsEnabled) && (
            <div className="mt-4 flex flex-col gap-2">
              {!requireApiKey && (
                <SecurityWarning
                  message="Require API key is disabled — your endpoint is publicly accessible without authentication."
                  action={{ label: "Enable", href: "#require-api-key" }}
                />
              )}
              {(!requireLogin || !hasPassword) && (
                <SecurityWarning
                  message={
                    !requireLogin
                      ? "Require login is disabled — anyone can access your dashboard via tunnel."
                      : "Dashboard uses the default password — change it in Profile settings."
                  }
                  action={{
                    label: !requireLogin ? "Enable" : "Change password",
                    href: "/dashboard/profile",
                  }}
                />
              )}
            </div>
          )}

          {/* Tunnel dashboard access option */}
          {(tunnelEnabled || tsEnabled) && (
            <div className="mt-4 pt-4 border-t border-border flex items-center gap-3">
              <Toggle
                aria-label="Allow dashboard access via tunnel"
                checked={tunnelDashboardAccess}
                onChange={() =>
                  handleTunnelDashboardAccess(!tunnelDashboardAccess)
                }
              />
              <div className="flex items-center gap-1.5">
                <p className="font-medium text-sm">
                  Allow dashboard access via tunnel
                </p>
                <Tooltip text="When enabled, the dashboard can be accessed through your tunnel or Tailscale URL (login still required). When disabled, dashboard access via tunnel/Tailscale is completely blocked." />
              </div>
            </div>
          )}
        </section>

        {/* API Keys */}
        <section id="require-api-key" className="min-w-0 border-t border-border pt-5 xl:border-t-0 xl:pt-0">
          <div className="mb-5 flex items-end justify-between gap-3">
            <div>
              <p className="editorial-kicker">02 / Security</p>
              <h2 className="mt-2 text-xl font-semibold tracking-tight">API keys</h2>
            </div>
            <Button icon={<Plus size={16} aria-hidden="true" />} onClick={() => setShowAddModal(true)}>
              Create Key
            </Button>
          </div>

          <div className="flex items-center justify-between pb-4 mb-4 border-b border-border">
            <div>
              <p className="font-medium">Require API key</p>
              <p className="text-sm text-text-muted">
                Requests without a valid key will be rejected
              </p>
            </div>
            <Toggle
              aria-label="Require API key"
              checked={requireApiKey}
              onChange={() => handleRequireApiKey(!requireApiKey)}
            />
          </div>

          {isRemoteHost && !requireApiKey && (
            <div className="mb-4 -mt-2">
              <SecurityWarning message="Endpoint is exposed without an API key." />
            </div>
          )}

          {keys.length === 0 ? (
            <div className="text-center py-12">
              <div className="inline-flex items-center justify-center w-16 h-16 rounded-full bg-primary/10 text-primary mb-4">
                <KeyRound size={32} aria-hidden="true" />
              </div>
              <p className="text-text-main font-medium mb-1">No API keys yet</p>
              <p className="text-sm text-text-muted mb-4">
                Create your first API key to get started
              </p>
              <Button icon={<Plus size={16} aria-hidden="true" />} onClick={() => setShowAddModal(true)}>
                Create Key
              </Button>
            </div>
          ) : (
            <div className="editorial-ledger border-y border-border">
              {keys.map((key) => (
                <div
                  key={key.id}
                   className={`group flex min-w-0 items-center justify-between gap-3 py-4 ${key.isActive === false ? "opacity-60" : ""}`}
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium">{key.name}</p>
                    <div className="flex items-center gap-2 mt-1">
                      <code className="min-w-0 break-all font-mono text-xs text-text-muted">
                        {visibleKeys.has(key.id)
                          ? (key.key || (key.keyId ? `sk-${key.machineId?.slice(0, 6)}...${key.keyId}` : "••••••••••••••••"))
                          : maskKey(key.key, key.keyId, key.machineId)}
                      </code>
                      <button
                        onClick={() => toggleKeyVisibility(key.id)}
                        aria-label={visibleKeys.has(key.id) ? `Hide ${key.name}` : `Show ${key.name}`}
                        className="rounded p-1 text-text-muted transition-all hover:bg-black/5 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 dark:hover:bg-white/5"
                        title={
                          visibleKeys.has(key.id) ? "Hide key" : "Show key"
                        }
                      >
                        {visibleKeys.has(key.id) ? <EyeOff size={16} strokeWidth={1.75} aria-hidden="true" /> : <Eye size={16} strokeWidth={1.75} aria-hidden="true" />}
                      </button>
                      <button
                        onClick={() => copy(key.key || key.keyId || key.id, key.id)}
                        aria-label={`Copy ${key.name}`}
                        className="inline-flex items-center gap-1 rounded p-1 text-text-muted transition-all hover:bg-black/5 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 dark:hover:bg-white/5"
                      >
                        {copied === key.id ? <Check size={16} strokeWidth={1.75} aria-hidden="true" /> : <Copy size={16} strokeWidth={1.75} aria-hidden="true" />}
                        <span className="text-xs">{copied === key.id ? "Copied" : "Copy"}</span>
                      </button>
                    </div>
                    <p className="text-xs text-text-muted mt-1">
                      Created {new Date(key.createdAt).toLocaleDateString()}
                    </p>
                    {key.isActive === false && (
                      <p className="text-xs text-orange-500 mt-1">Paused</p>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    <Toggle
                      aria-label={`${key.isActive ?? true ? "Disable" : "Enable"} ${key.name}`}
                      size="sm"
                      checked={key.isActive ?? true}
                      onChange={(checked) => {
                        if (key.isActive && !checked) {
                          setConfirmState({
                            title: "Pause API Key",
                            message: `Pause API key "${key.name}"?\n\nThis key will stop working immediately but can be resumed later.`,
                            onConfirm: async () => {
                              setConfirmState(null);
                              handleToggleKey(key.id, checked);
                            },
                          });
                        } else {
                          handleToggleKey(key.id, checked);
                        }
                      }}
                      title={key.isActive ? "Pause key" : "Resume key"}
                    />
                    <button
                      onClick={() => handleDeleteKey(key.id)}
                      aria-label={`Delete ${key.name}`}
                      className="rounded p-2 text-red-500 opacity-100 transition-all hover:bg-red-500/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-500/40 sm:opacity-0 sm:group-focus-within:opacity-100 sm:group-hover:opacity-100"
                    >
                      <Trash2 size={18} strokeWidth={1.75} aria-hidden="true" />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      {/* Add Key Modal */}
      <Modal
        isOpen={showAddModal}
        title="Create API Key"
        onClose={() => {
          setShowAddModal(false);
          setNewKeyName("");
        }}
      >
        <div className="flex flex-col gap-4">
          <Input
            label="Key Name"
            value={newKeyName}
            onChange={(e) => setNewKeyName(e.target.value)}
            placeholder="Production Key"
          />
          <div className="flex gap-2">
            <Button
              onClick={handleCreateKey}
              fullWidth
              disabled={!newKeyName.trim()}
            >
              Create
            </Button>
            <Button
              onClick={() => {
                setShowAddModal(false);
                setNewKeyName("");
              }}
              variant="ghost"
              fullWidth
            >
              Cancel
            </Button>
          </div>
        </div>
      </Modal>

      {/* Created Key Modal */}
      <Modal
        isOpen={!!createdKey}
        title="API Key Created"
        onClose={() => setCreatedKey(null)}
      >
        <div className="flex flex-col gap-4">
          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-lg p-4">
            <p className="text-sm text-yellow-800 dark:text-yellow-200 mb-2 font-medium">
              Save this key now!
            </p>
            <p className="text-sm text-yellow-700 dark:text-yellow-300">
              This is the only time you will see this key. Store it securely.
            </p>
          </div>
          <div className="flex gap-2">
            <Input
              value={createdKey || ""}
              readOnly
              className="flex-1 font-mono text-sm"
            />
            <Button
              variant="secondary"
              icon={copied === "created_key" ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
              onClick={() => copy(createdKey, "created_key")}
            >
              {copied === "created_key" ? "Copied!" : "Copy"}
            </Button>
          </div>
          <Button onClick={() => setCreatedKey(null)} fullWidth>
            Done
          </Button>
        </div>
      </Modal>

      {/* Enable Tunnel Modal */}
      <Modal
        isOpen={showEnableTunnelModal}
        title="Enable Tunnel"
        onClose={() => setShowEnableTunnelModal(false)}
      >
        <div className="flex flex-col gap-4">
          <div className="bg-surface-2 border border-border-subtle rounded-lg p-4">
            <div className="flex items-start gap-3">
              <CloudUpload size={20} className="text-primary" aria-hidden="true" />
              <div>
                <p className="text-sm text-text-main font-medium mb-1">
                  Cloudflare Tunnel
                </p>
                <p className="text-sm text-text-muted">
                  Expose your local FlameRouter to the internet. No port
                  forwarding, no static IP needed. Share endpoint URL with your
                  team or use it in Cursor, Cline, and other AI tools from
                  anywhere.
                </p>
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            {TUNNEL_BENEFITS.map((benefit) => {
              const BenefitIcon = tunnelBenefitIcons[benefit.icon] || Globe2;
              return (
                <div
                  key={benefit.title}
                  className="flex flex-col items-center text-center p-3 rounded-lg bg-sidebar/50"
                >
                  <BenefitIcon size={20} className="mb-1 text-primary" aria-hidden="true" />
                  <p className="text-xs font-semibold">{benefit.title}</p>
                  <p className="text-xs text-text-muted">{benefit.desc}</p>
                </div>
              );
            })}
          </div>

          <p className="text-xs text-text-muted">
            Requires outbound port 7844 (TCP/UDP). Connection may take 10-30s.
          </p>

          <div className="flex gap-2">
            <Button onClick={handleEnableTunnel} fullWidth>
              Start Tunnel
            </Button>
            <Button
              onClick={() => setShowEnableTunnelModal(false)}
              variant="ghost"
              fullWidth
            >
              Cancel
            </Button>
          </div>
        </div>
      </Modal>

      {/* Disable Cloudflare Tunnel Modal */}
      <Modal
        isOpen={showDisableTunnelModal}
        title="Disable Tunnel"
        onClose={() => !tunnelLoading && setShowDisableTunnelModal(false)}
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-muted">
            The Cloudflare tunnel will be disconnected. Remote access via tunnel
            URL will stop working.
          </p>
          <div className="flex gap-2">
            <Button
              onClick={handleDisableTunnel}
              fullWidth
              disabled={tunnelLoading}
              variant="danger"
            >
              {tunnelLoading ? "Disabling..." : "Disable"}
            </Button>
            <Button
              onClick={() => setShowDisableTunnelModal(false)}
              variant="ghost"
              fullWidth
              disabled={tunnelLoading}
            >
              Cancel
            </Button>
          </div>
        </div>
      </Modal>

      {/* Tailscale Modal */}
      <Modal
        isOpen={showTsModal}
        title="Tailscale Funnel"
        onClose={() => {
          if (!tsInstalling) {
            setShowTsModal(false);
            setTsSudoPassword("");
            setTsStatus(null);
          }
        }}
      >
        <div className="flex flex-col gap-4">
          {/* Checking state */}
          {tsInstalled === null && (
            <p className="text-sm text-text-muted flex items-center gap-2">
              <LoaderCircle size={16} className="animate-spin" aria-hidden="true" />
              Checking...
            </p>
          )}

          {/* Not installed */}
          {tsInstalled === false && !tsInstalling && (
            <div className="flex flex-col gap-3">
              <p className="text-sm text-text-muted">
                Tailscale is not installed. Install it to enable Funnel.
              </p>
              <div className="flex gap-2">
                <Button onClick={handleInstallTailscale} fullWidth>
                  Install Tailscale
                </Button>
                <Button
                  onClick={() => setShowTsModal(false)}
                  variant="ghost"
                  fullWidth
                >
                  Cancel
                </Button>
              </div>
            </div>
          )}

          {/* Installing with progress log */}
          {tsInstalling && (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2 text-sm text-text-muted">
                <LoaderCircle size={16} className="animate-spin" aria-hidden="true" />
                Installing Tailscale...
              </div>
              {tsInstallLog.length > 0 && (
                <div
                  ref={tsLogRef}
                  className="bg-black/5 dark:bg-white/5 rounded p-2 max-h-40 overflow-y-auto font-mono text-xs text-text-muted"
                >
                  {tsInstallLog.map((line, i) => (
                    <div key={i}>{line}</div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Installed: show Connect button */}
          {tsInstalled === true && !tsInstalling && (
            <div className="flex flex-col gap-3">
              <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
                <CheckCircle2 size={16} aria-hidden="true" />
                Tailscale installed
              </div>
              <div className="flex gap-2">
                <Button onClick={() => handleConnectTailscale()} fullWidth>
                  Connect
                </Button>
                <Button
                  onClick={() => setShowTsModal(false)}
                  variant="ghost"
                  fullWidth
                >
                  Cancel
                </Button>
              </div>
            </div>
          )}

          {tsStatus && <StatusAlert status={tsStatus} />}
        </div>
      </Modal>

      {/* Disable Tailscale Modal */}
      <Modal
        isOpen={showDisableTsModal}
        title="Disable Tailscale"
        onClose={() => !tsLoading && setShowDisableTsModal(false)}
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-muted">
            Tailscale Funnel will be stopped. Remote access via Tailscale URL
            will stop working.
          </p>
          <div className="flex gap-2">
            <Button
              onClick={handleDisableTailscale}
              fullWidth
              disabled={tsLoading}
              variant="danger"
            >
              {tsLoading ? "Disabling..." : "Disable"}
            </Button>
            <Button
              onClick={() => setShowDisableTsModal(false)}
              variant="ghost"
              fullWidth
              disabled={tsLoading}
            >
              Cancel
            </Button>
          </div>
        </div>
      </Modal>

      {/* Confirm Modal */}
      <ConfirmModal
        isOpen={!!confirmState}
        onClose={() => setConfirmState(null)}
        onConfirm={confirmState?.onConfirm}
        title={confirmState?.title || "Confirm"}
        message={confirmState?.message}
        variant="danger"
      />
    </div>
  );
}

APIPageClient.propTypes = {
  machineId: PropTypes.string.isRequired,
};
