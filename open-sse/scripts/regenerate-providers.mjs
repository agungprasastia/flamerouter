// Regenerate flamerouter/src/providers/mod.rs from open-sse/providers/registry/*.js.
//
// Field derivation rules (open-sse registry shape):
//   - target_format: first entry of `transports[]` (or `transport`) `.format`.
//     Unsupported exotic formats (kiro, cursor, tts, ...) fall back to "openai" —
//     flamerouter has no custom executors for them yet.
//   - auth pairs: transport-level `auth {header, scheme}` (combined style) wins;
//     else provider-level `auth.apiKey` / `auth.oauth`. Defaults: Authorization/bearer.
//   - base_url + extra_headers are preserved from the existing Rust file.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REG_DIR = path.resolve(__dirname, "../../../open-sse/providers/registry");
const MOD_RS = path.resolve(__dirname, "../src/providers/mod.rs");

const SUPPORTED = new Set(["openai", "claude", "gemini", "openai-responses"]);

// ─── JS object scanning helpers (no deps) ───────────────────────────────────

const KNOWN_CONFIG_VARS = {
  GROK_CLI_BASE_URL: "https://cli-chat-proxy.grok.com/v1",
};
const { GROK_CLI_BASE_URL } = KNOWN_CONFIG_VARS;

function resolveTemplate(v) {
  // `baseUrl: \`${VAR}/rest\`` → resolve known config vars
  return v.replace(/\$\{(\w+)\}/g, (_, name) => KNOWN_CONFIG_VARS[name] ?? "");
}

function skipString(src, i) {
  const q = src[i];
  i++;
  while (i < src.length) {
    if (src[i] === "\\") i += 2;
    else if (src[i] === q) return i + 1;
    else i++;
  }
  return i;
}

function matchKey(src, i) {
  const m = /^[A-Za-z_$][\w$]*/.exec(src.slice(i));
  if (!m) return null;
  let j = i + m[0].length;
  while (j < src.length && /\s/.test(src[j])) j++;
  if (src[j] !== ":") return null;
  j++;
  while (j < src.length && /\s/.test(src[j])) j++;
  return { name: m[0], start: j };
}

// Split a `{ ... }` (or `[ ... ]`) chunk into its top-level key → value text map.
function topLevel(rawChunk) {
  // Unwrap the chunk's own outer braces so content sits at depth 0.
  let chunk = rawChunk.trim();
  if (chunk[0] === "{") chunk = chunk.slice(1, -1);
  const out = {};
  let depth = 0;
  let key = null;
  let keyStart = -1;
  for (let i = 0; i < chunk.length; i++) {
    const c = chunk[i];
    if (c === '"' || c === "'" || c === "`") {
      i = skipString(chunk, i) - 1;
      continue;
    }
    if (c === "/" && chunk[i + 1] === "/") {
      while (i < chunk.length && chunk[i] !== "\n") i++;
      continue;
    }
    if (c === "{" || c === "[") {
      depth++;
    } else if (c === "}" || c === "]") {
      depth--;
      if (depth === 0 && key) {
        out[key] = chunk.slice(keyStart, i + 1);
        key = null;
      }
    } else if (depth === 0 && key === null && (c === " " || c === "\t" || c === "\n" || c === "\r" || c === ",")) {
      // separator
    } else if (depth === 0 && key === null) {
      const m = matchKey(chunk, i);
      if (m) {
        const j = m.start;
        if (chunk[j] === "{") {
          key = m.name;
          keyStart = j;
          i = j - 1;
        } else if (chunk[j] === "[") {
          key = m.name;
          keyStart = j;
          i = j - 1;
        } else {
          let v;
          let end;
          if (chunk[j] === '"' || chunk[j] === "'" || chunk[j] === "`") {
            v = chunk.slice(j + 1, (end = skipString(chunk, j) - 1));
            i = end;
          } else {
            const tok = /^[^\s,}\n]+/.exec(chunk.slice(j));
            v = tok ? tok[0] : "";
            end = j + v.length;
            i = end - 1;
          }
          out[m.name] = v;
          out[m.name + "__kind"] = "scalar";
        }
      }
    }
  }
  return out;
}

function arrayElements(arrChunk) {
  const out = [];
  const inner = arrChunk.slice(1, -1);
  let depth = 0;
  let start = -1;
  for (let i = 0; i < inner.length; i++) {
    const c = inner[i];
    if (c === '"' || c === "'") {
      i = skipString(inner, i) - 1;
      continue;
    }
    if (c === "/" && inner[i + 1] === "/") {
      while (i < inner.length && inner[i] !== "\n") i++;
      continue;
    }
    if (c === "{") {
      if (depth === 0 && start === -1) start = i;
      depth++;
    } else if (c === "}") {
      depth--;
      if (depth === 0 && start !== -1) {
        out.push(inner.slice(start, i + 1));
        start = -1;
      }
    }
  }
  return out;
}

function kv(objText, key) {
  const re = new RegExp("\\b" + key + "\\s*:\\s*(\"([^\"]*)\"|true|false)");
  const m = re.exec(objText);
  if (!m) return undefined;
  return m[2] !== undefined ? m[2] : m[1] === "true";
}

function fmtOf(objText) {
  const m = /^\s*format\s*:\s*"([^"]*)"/m.exec(objText);
  return m ? m[1] : undefined;
}

// ─── Per-provider derivation ────────────────────────────────────────────────

function derive(src) {
  const idM = /^\s*id\s*:\s*"([^"]+)"/m.exec(src);
  if (!idM) return null;
  const id = idM[1];
  // Strip imports, then cut strictly inside `export default { ... }` so the
  // outer object's content sits at scanner depth 0.
  const noImports = src.replace(/^import[^;]+;/gm, "");
  const outer = noImports.indexOf("export default");
  const s = noImports.indexOf("{", outer) + 1;
  const e = noImports.lastIndexOf("}");
  const chunk = noImports.slice(s, e);
  const root = topLevel(chunk);

  let format;
  let baseUrl;
  let apiHeader = "Authorization";
  let apiScheme = "bearer";
  let oauthHeader = "Authorization";
  let oauthScheme = "bearer";

  const fromTransport = (tObj) => {
    if (!tObj) return;
    const t = topLevel(tObj);
    if (!baseUrl && t.baseUrl) baseUrl = t.baseUrl;
    if (!format) format = fmtOf(tObj);
    if (t.auth) {
      const a = topLevel(t.auth);
      if (a.header) {
        apiHeader = a.header;
        apiScheme = a.scheme ?? "bearer";
        oauthHeader = a.header;
        oauthScheme = a.scheme ?? "bearer";
      } else {
        applyNestedAuth(a);
      }
    }
  };

  const applyNestedAuth = (a) => {
    if (a.header) {
      apiHeader = a.header;
      apiScheme = a.scheme ?? "bearer";
      oauthHeader = a.header;
      oauthScheme = a.scheme ?? "bearer";
      return;
    }
    if (a.apiKey) {
      const k = topLevel(a.apiKey);
      if (k.header) {
        apiHeader = k.header;
        apiScheme = k.scheme ?? "bearer";
      }
    }
    if (a.oauth) {
      const o = topLevel(a.oauth);
      if (o.header) {
        oauthHeader = o.header;
        oauthScheme = o.scheme ?? "bearer";
      }
    }
  };

  if (root.transports) {
    const first = arrayElements(root.transports)[0];
    if (first) fromTransport(first);
  } else if (root.transport) {
    fromTransport(root.transport);
  }

  // Provider-level auth (claude-style): apiKey vs oauth mode
  if (root.auth) {
    applyNestedAuth(topLevel(root.auth));
  }

  if (format && !SUPPORTED.has(format)) format = "openai";
  if (baseUrl) baseUrl = resolveTemplate(baseUrl).replace(/^`|`$/g, "").trim();
  if (/^\$\{/.test(baseUrl ?? "")) baseUrl = ""; // unresolved template — not a static URL
  apiScheme = apiScheme === "Bearer" ? "bearer" : apiScheme;
  oauthScheme = oauthScheme === "Bearer" ? "bearer" : oauthScheme;

  return { id, format, baseUrl, apiHeader, apiScheme, oauthHeader, oauthScheme };
}

const RUST_ONLY_ALIASES = [
  // Aliases maintained locally, mirroring an open-sse provider.
  {
    id: "google",
    format: "gemini",
    baseUrl: "https://generativelanguage.googleapis.com/v1beta/models",
    apiHeader: "x-goog-api-key",
    apiScheme: "raw",
    oauthHeader: "Authorization",
    oauthScheme: "bearer",
    extraHeaders: "&[]",
  },
];

// ─── Existing Rust file: preserve extra_headers only ────────────────────────

const existingRs = fs.readFileSync(MOD_RS, "utf8");
const existing = new Map();
for (const m of existingRs.matchAll(/m\.insert\(\n\s+"([^"]+)",\n\s+Provider \{[\s\S]*?extra_headers: &(\[[\s\S]*?\]),?/g)) {
  existing.set(m[1], m[2]);
}

// ─── Emit ───────────────────────────────────────────────────────────────────

const jsFiles = fs.readdirSync(REG_DIR).filter((f) => f.endsWith(".js"));
const entries = [];
for (const f of jsFiles) {
  const src = fs.readFileSync(path.join(REG_DIR, f), "utf8");
  const d = derive(src);
  if (!d) continue;
  if (!d.format) d.format = "openai";
  d.baseUrl ??= "";
  d.apiHeader ??= "Authorization";
  d.apiScheme ??= "bearer";
  d.oauthHeader ??= "Authorization";
  d.oauthScheme ??= "bearer";
  d.extraHeaders = existing.get(d.id) ?? "&[]";
  entries.push({ ...d });
}

// Rust-only aliases not present in open-sse.
for (const a of RUST_ONLY_ALIASES) {
  if (!entries.some((x) => x.id === a.id)) {
    entries.push({ ...a, extraHeaders: existing.get(a.id) ?? a.extraHeaders });
  }
}

entries.sort((a, b) => a.id.localeCompare(b.id));

const lines = [];
lines.push("//! Provider registry — auto-generated from open-sse provider definitions.");
lines.push("//! Regenerate with `node flamerouter/scripts/regenerate-providers.mjs` — don't hand-edit.");
lines.push("//! target_format + auth pairs derive from open-sse transport/transports entries;");
lines.push("//! unsupported exotic formats fall back to \"openai\" (no custom executor yet).");
lines.push("");
lines.push("use std::collections::HashMap;");
lines.push("use std::sync::OnceLock;");
lines.push("");
lines.push("#[derive(Debug, Clone)]");
lines.push("pub struct Provider {");
lines.push("    pub id: &'static str,");
lines.push("    pub base_url: &'static str,");
lines.push("    pub target_format: &'static str,");
lines.push("    /// API-key auth pair — used when credentials carry `apiKey`.");
lines.push("    pub auth_header: &'static str,");
lines.push("    pub auth_scheme: &'static str,");
lines.push("    /// OAuth auth pair — used when credentials carry `accessToken`.");
lines.push("    pub oauth_header: &'static str,");
lines.push("    pub oauth_scheme: &'static str,");
lines.push("    pub extra_headers: &'static [(&'static str, &'static str)],");
lines.push("}");
lines.push("");
lines.push("static REGISTRY: OnceLock<HashMap<&'static str, Provider>> = OnceLock::new();");
lines.push("");
lines.push("/// Allow tests / dev to override a provider's base_url via env:");
lines.push("/// `FLAMEROUTER_BASE_URL_<PROVIDER_UPPER>` (e.g. FLAMEROUTER_BASE_URL_OPENAI=http://127.0.0.1:29999/v1).");
lines.push("pub fn base_url_for(p: &Provider) -> String {");
lines.push("    let key = format!(\"FLAMEROUTER_BASE_URL_{}\", p.id.to_uppercase().replace('-', \"_\"));");
lines.push("    std::env::var(&key).unwrap_or_else(|_| p.base_url.to_string())");
lines.push("}");
lines.push("");
lines.push("pub fn registry() -> &'static HashMap<&'static str, Provider> {");
lines.push("    REGISTRY.get_or_init(|| {");
lines.push("        let mut m = HashMap::new();");
for (const e of entries) {
  lines.push(`        m.insert(`);
  lines.push(`            "${e.id}",`);
  lines.push(`            Provider {`);
  lines.push(`                id: "${e.id}",`);
  lines.push(`                base_url: "${e.baseUrl}",`);
  lines.push(`                target_format: "${e.format}",`);
  lines.push(`                auth_header: "${e.apiHeader}",`);
  lines.push(`                auth_scheme: "${e.apiScheme}",`);
  lines.push(`                oauth_header: "${e.oauthHeader}",`);
  lines.push(`                oauth_scheme: "${e.oauthScheme}",`);
  const eh = e.extraHeaders.startsWith("&") ? e.extraHeaders : `&${e.extraHeaders}`;
  lines.push(`                extra_headers: ${eh},`);
  lines.push(`            },`);
  lines.push(`        );`);
}
lines.push("        m");
lines.push("    })");
lines.push("}");
lines.push("");
lines.push("/// Look up a provider by id.");
lines.push("pub fn get(id: &str) -> Option<&'static Provider> {");
lines.push("    registry().get(id)");
lines.push("}");
lines.push("");

const out = lines.join("\n");
fs.writeFileSync(MOD_RS, out);
console.log(`regenerated ${entries.length} providers into ${MOD_RS}`);