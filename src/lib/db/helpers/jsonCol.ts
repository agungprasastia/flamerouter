export function parseJson<T = unknown>(str: string | null | undefined, fallback: T | null = null): T | null {
  if (str == null) return fallback;
  if (typeof str !== "string") return str as unknown as T;
  try {
    return JSON.parse(str) as T;
  } catch {
    return fallback;
  }
}

export function stringifyJson(value: unknown): string {
  return JSON.stringify(value ?? null);
}
