// UI display config — all providers derive from registry.display.
import REGISTRY from "@/shared/constants/providersRegistry";

export const RISK_NOTICE =
  "⚠️ Risk Notice: This provider uses a subscription/OAuth session not officially licensed for proxy/router use. Account may be restricted or banned. Use at your own risk.";

export interface ProviderDisplayConfig {
  name?: string;
  color?: string;
  icon?: string;
  description?: string;
  deprecationNotice?: string;
  [key: string]: unknown;
}

// Resolve "RISK_NOTICE" token → real notice text (registry stores token to avoid import cycle)
const resolveDisplay = (d: ProviderDisplayConfig): ProviderDisplayConfig =>
  d.deprecationNotice === "RISK_NOTICE"
    ? { ...d, deprecationNotice: RISK_NOTICE }
    : d;

export const PROVIDER_DISPLAY: Record<string, ProviderDisplayConfig> = Object.fromEntries(
  (REGISTRY as Array<{ id: string; display?: ProviderDisplayConfig }>)
    .filter((r) => r.display)
    .map((r) => [
      r.id,
      resolveDisplay(r.display!),
    ]),
);
