// Shared helpers used across provider entry files (currently trae + windsurf).

export function extractJsonPath(root: unknown, paths: readonly (readonly string[])[] | string[][]): string | null {
  for (const path of paths) {
    let cur: unknown = root;
    for (const key of path) {
      if (cur == null || typeof cur !== "object") {
        cur = undefined;
        break;
      }
      cur = (cur as Record<string, unknown>)[key];
    }
    if (typeof cur === "string" && cur.trim()) return cur.trim();
    if (typeof cur === "number") return String(cur);
  }
  return null;
}
