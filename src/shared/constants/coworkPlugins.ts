export const DEFAULT_PLUGINS = [
  {
    name: "exa",
    title: "Exa",
    description: "Real-time web search and code documentation",
    url: "https://mcp.exa.ai/mcp",
    transport: "http",
    oauth: false,
    toolNames: ["web_search_exa", "web_fetch_exa"],
  },
  {
    name: "tavily",
    title: "Tavily",
    description: "Real-time web search optimized for LLM agents",
    url: "https://mcp.tavily.com/mcp",
    transport: "http",
    oauth: true,
    toolNames: [
      "tavily_search",
      "tavily_extract",
      "tavily_crawl",
      "tavily_map",
    ],
  },
];

// Local stdio plugins bridged via inline SSE endpoint on the app's port.
export const LOCAL_STDIO_PLUGINS = [
  {
    name: "browsermcp",
    title: "Browser MCP",
    description: "Control your running Chrome (requires Chrome extension)",
    extensionUrl:
      "https://chromewebstore.google.com/detail/browser-mcp-automate-your/bjfgambnhccakkhmkepdoekmckoijdlc",
    command: "npx",
    args: ["-y", "@browsermcp/mcp@latest"],
    toolNames: [
      "browser_navigate",
      "browser_snapshot",
      "browser_click",
      "browser_type",
      "browser_screenshot",
      "browser_get_console_logs",
      "browser_wait",
      "browser_press_key",
      "browser_go_back",
      "browser_go_forward",
    ],
  },
];

export interface CoworkPlugin {
  name: string;
  title?: string;
  description?: string;
  url?: string;
  transport?: string;
  oauth?: boolean;
  toolNames?: string[];
  extensionUrl?: string;
  command?: string;
  args?: string[];
  [key: string]: unknown;
}

export interface ManagedMcpServerEntry {
  name: string;
  url: string;
  transport: string;
  oauth?: boolean;
  toolPolicy?: Record<string, string>;
}

function buildManagedMcpServers(plugins?: unknown): ManagedMcpServerEntry[] {
  const list = Array.isArray(plugins) ? (plugins as Array<Partial<CoworkPlugin>>) : [];
  const out: ManagedMcpServerEntry[] = [];
  const seen = new Set<string>();
  for (const p of list) {
    if (!p?.name || !p?.url || seen.has(p.name)) continue;
    seen.add(p.name);
    const entry: ManagedMcpServerEntry = {
      name: p.name,
      url: p.url,
      transport: p.transport || (/\/sse(\b|\/)/i.test(p.url) ? "sse" : "http"),
    };
    if (p.oauth) entry.oauth = true;
    if (Array.isArray(p.toolNames) && p.toolNames.length > 0) {
      // Strip any pre-existing "{name}-" prefixes (idempotent across re-applies),
      // then emit both bare + single-prefixed variants to match runtime tool naming.
      const prefix = `${p.name}-`;
      const bare = new Set<string>();
      for (const raw of p.toolNames) {
        if (typeof raw !== "string" || !raw) continue;
        let t = raw;
        while (t.startsWith(prefix)) t = t.slice(prefix.length);
        bare.add(t);
      }
      const policy: Record<string, string> = {};
      for (const t of bare) {
        policy[t] = "allow";
        policy[`${prefix}${t}`] = "allow";
      }
      entry.toolPolicy = policy;
    }
    out.push(entry);
  }
  return out;
}

export default {
  DEFAULT_PLUGINS,
  LOCAL_STDIO_PLUGINS,
  buildManagedMcpServers,
};

