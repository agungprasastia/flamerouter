"use client";

import { useParams, notFound, useRouter } from "next/navigation";
import Link from "next/link";
import { useState, useEffect } from "react";
import {
  Badge,
  Button,
  AddCustomEmbeddingModal,
  NoAuthProxyCard,
  ProviderInfoCard,
} from "@/shared/components";
import ProviderIcon from "@/shared/components/ProviderIcon";
import {
  MEDIA_PROVIDER_KINDS,
  AI_PROVIDERS,
  isCustomEmbeddingProvider,
} from "@/shared/constants/providers";
import type { ProviderEntry } from "@/shared/constants/providers";
import ConnectionsCard from "@/app/(dashboard)/dashboard/providers/components/ConnectionsCard";
import ModelsCard from "@/app/(dashboard)/dashboard/providers/components/ModelsCard";
import { KIND_EXAMPLE_CONFIG } from "./components/exampleShared";
import { EmbeddingExampleCard } from "./components/EmbeddingExampleCard";
import { TtsExampleCard } from "./components/TtsExampleCard";
import { GenericExampleCard } from "./components/GenericExampleCard";
import { SttExampleCard } from "./components/SttExampleCard";

interface CustomEmbeddingNode {
  id: string;
  name?: string;
  prefix?: string;
  [key: string]: unknown;
}

export default function MediaProviderDetailPage() {
  const params = useParams();
  const kindParam = Array.isArray(params?.kind) ? params.kind[0] : params?.kind;
  const idParam = Array.isArray(params?.id) ? params.id[0] : params?.id;
  const kind = kindParam || "";
  const id = idParam || "";
  const router = useRouter();
  const kindConfig = MEDIA_PROVIDER_KINDS.find((k) => k.id === kind);
  const isCustom = isCustomEmbeddingProvider(id) && kind === "embedding";

  const handleDeleteCustom = async () => {
    if (!confirm("Delete this Custom Embedding node?")) return;
    try {
      const res = await fetch(`/api/provider-nodes/${id}`, {
        method: "DELETE",
      });
      if (res.ok) router.push(`/dashboard/media-providers/${kind}`);
    } catch (error) {
      console.log("Error deleting custom embedding node:", error);
    }
  };

  const [customNode, setCustomNode] = useState<CustomEmbeddingNode | null>(null);
  const [customLoading, setCustomLoading] = useState(isCustom);
  const [showEditModal, setShowEditModal] = useState(false);

  useEffect(() => {
    if (!isCustom) return;
    let cancelled = false;
    fetch("/api/provider-nodes", { cache: "no-store" })
      .then((r) => r.json())
      .then((d: { nodes?: CustomEmbeddingNode[] }) => {
        if (cancelled) return;
        setCustomNode((d.nodes || []).find((n) => n.id === id) || null);
        setCustomLoading(false);
      })
      .catch(() => {
        if (!cancelled) setCustomLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [id, isCustom]);

  if (!kindConfig || !id) return notFound();

  const builtInProvider = AI_PROVIDERS[id] as ProviderEntry | undefined;

  const provider: ProviderEntry | null = isCustom
    ? customNode
      ? {
          id,
          name: customNode.name || "Custom Embedding",
          color: "#6366F1",
          textIcon: "CE",
        }
      : null
    : builtInProvider ?? null;

  if (!isCustom && !builtInProvider) return notFound();
  if (isCustom && !customLoading && !customNode) return notFound();
  if (isCustom && customLoading) {
    return (
      <div className="text-text-muted text-sm py-12 text-center">
        Loading...
      </div>
    );
  }
  if (!provider) return notFound();

  const kinds = isCustom ? ["embedding"] : (provider.serviceKinds ?? ["llm"]);
  if (!isCustom && !kinds.includes(kind)) return notFound();

  const providerInfoConfig = ((): Record<string, unknown> | null | undefined => {
    if (isCustom) return null;
    if (kind === "webFetch") return provider.fetchConfig as Record<string, unknown> | undefined;
    if (kind === "tts") return provider.ttsConfig as Record<string, unknown> | undefined;
    if (kind === "stt") return provider.sttConfig as Record<string, unknown> | undefined;
    if (kind === "embedding") return provider.embeddingConfig as Record<string, unknown> | undefined;
    return (
      (provider.searchConfig as Record<string, unknown> | undefined) || {
        mode: "chat-completions",
        defaultModel: (provider.searchViaChat as { defaultModel?: string } | undefined)?.defaultModel,
        pricingUrl: (provider.searchViaChat as { pricingUrl?: string } | undefined)?.pricingUrl,
        freeTier: (provider.searchViaChat as { freeTier?: string } | undefined)?.freeTier,
      }
    );
  })();

  return (
    <div className="flex flex-col gap-8">
      <div>
        <Link
          href={`/dashboard/media-providers/${kind}`}
          className="inline-flex items-center gap-1 text-sm text-text-muted hover:text-primary transition-colors mb-4"
        >
          <span className="material-symbols-outlined text-lg">arrow_back</span>
          {kindConfig.label}
        </Link>

        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:gap-4">
          <div
            className="size-12 rounded-lg flex items-center justify-center shrink-0"
            style={{ backgroundColor: `${provider.color || "#6366F1"}15` }}
          >
            <ProviderIcon
              src={`/providers/${provider.id}.png`}
              alt={provider.name}
              size={48}
              className="object-contain rounded-lg max-w-[48px] max-h-[48px]"
              fallbackText={
                (provider.textIcon as string) || provider.id.slice(0, 2).toUpperCase()
              }
              fallbackColor={provider.color as string}
            />
          </div>
          <div className="flex-1">
            <div className="flex flex-wrap items-center gap-2 sm:gap-3">
              <h1 className="text-3xl font-semibold tracking-tight">
                {provider.name}
              </h1>
              {!isCustom && (provider.notice as { apiKeyUrl?: string } | undefined)?.apiKeyUrl && (
                <a
                  href={(provider.notice as { apiKeyUrl: string }).apiKeyUrl}
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
            <div className="flex items-center gap-1.5 mt-1 flex-wrap">
              {isCustom && (
                <Badge variant="default" size="sm">
                  Custom · {customNode?.prefix}
                </Badge>
              )}
              {kinds.map((k) => (
                <Badge
                  key={k}
                  variant={k === kind ? "primary" : "default"}
                  size="sm"
                >
                  {k.toUpperCase()}
                </Badge>
              ))}
            </div>
          </div>
          {isCustom && (
            <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
              <Button
                size="sm"
                variant="secondary"
                icon="edit"
                onClick={() => setShowEditModal(true)}
              >
                Edit
              </Button>
              <Button
                size="sm"
                variant="secondary"
                icon="delete"
                onClick={handleDeleteCustom}
              >
                Delete
              </Button>
            </div>
          )}
        </div>
      </div>

      {!isCustom && (provider.kindNotice as Record<string, string> | undefined)?.[kind] && (
        <div className="flex items-start gap-3 px-4 py-3 rounded-lg bg-amber-500/10 border border-amber-500/30 text-amber-700 dark:text-amber-400">
          <span className="material-symbols-outlined text-[20px] mt-0.5">
            warning
          </span>
          <p className="text-sm">{(provider.kindNotice as Record<string, string>)[kind]}</p>
        </div>
      )}

      {!isCustom && (provider.notice as { text?: string; apiKeyUrl?: string } | undefined)?.text && !provider.deprecated && (
        <div className="flex flex-col gap-2 rounded-lg border border-blue-500/30 bg-blue-500/10 px-3 py-2 sm:flex-row sm:items-center">
          <span className="material-symbols-outlined text-[16px] text-blue-500 shrink-0">
            info
          </span>
          <p className="min-w-0 flex-1 text-xs leading-relaxed text-blue-600 dark:text-blue-400">
            {(provider.notice as { text: string }).text}
          </p>
          {(provider.notice as { apiKeyUrl?: string }).apiKeyUrl && (
            <a
              href={(provider.notice as { apiKeyUrl: string }).apiKeyUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex justify-center rounded bg-blue-500 px-2 py-1 text-xs font-medium text-white transition-colors hover:bg-blue-600 sm:py-0.5"
            >
              Get API Key →
            </a>
          )}
        </div>
      )}

      {!isCustom && provider.noAuth ? (
        <NoAuthProxyCard providerId={id} />
      ) : (
        <ConnectionsCard providerId={id} isOAuth={false} />
      )}

      {kind !== "tts" && kind !== "webSearch" && kind !== "webFetch" && (
        <ModelsCard
          providerId={id}
          kindFilter={kind}
          providerAliasOverride={isCustom ? customNode?.prefix : undefined}
        />
      )}

      {!isCustom &&
        Boolean(
          provider.searchConfig ||
            provider.fetchConfig ||
            provider.ttsConfig ||
            provider.sttConfig ||
            provider.embeddingConfig ||
            provider.searchViaChat,
        ) && (
          <ProviderInfoCard
            config={providerInfoConfig}
            provider={provider as { notice?: { apiKeyUrl?: string; text?: string }; website?: string }}
            title={`${kindConfig.label} Config`}
          />
        )}

      {kind === "embedding" && (
        <EmbeddingExampleCard
          providerId={id}
          customAlias={customNode?.prefix}
        />
      )}
      {kind === "tts" && <TtsExampleCard providerId={id} />}
      {kind === "stt" && !isCustom && <SttExampleCard providerId={id} />}
      {!isCustom && KIND_EXAMPLE_CONFIG[kind] && (
        <GenericExampleCard providerId={id} kind={kind} />
      )}

      {isCustom && (
        <AddCustomEmbeddingModal
          isOpen={showEditModal}
          node={customNode}
          onClose={() => setShowEditModal(false)}
          onSaved={(updated) => {
            setCustomNode(updated as CustomEmbeddingNode);
            setShowEditModal(false);
          }}
        />
      )}
    </div>
  );
}
