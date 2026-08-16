const BASE64_BLOCK_SIZE = 4;

function validateXaiOAuthEndpoint(rawUrl: unknown, field: string): string {
  const value = String(rawUrl || "").trim();
  if (!value) throw new Error(`xai discovery ${field} is empty`);
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch (err: unknown) {
    const e = err as Error;
    throw new Error(`xai discovery ${field} is invalid: ${e.message}`);
  }
  if (parsed.protocol !== "https:")
    throw new Error(`xai discovery ${field} must use https: ${value}`);
  const host = parsed.hostname.toLowerCase().trim();
  if (host !== "x.ai" && !host.endsWith(".x.ai")) {
    throw new Error(`xai discovery ${field} host ${host} is not on x.ai`);
  }
  return value;
}

function decodeXaiIdTokenEmail(idToken?: string | null): string | undefined {
  if (!idToken || typeof idToken !== "string") return undefined;
  const parts = idToken.split(".");
  if (parts.length !== 3) return undefined;
  try {
    const base64 = (parts[1] || "").replace(/-/g, "+").replace(/_/g, "/");
    const padding =
      (BASE64_BLOCK_SIZE - (base64.length % BASE64_BLOCK_SIZE)) %
      BASE64_BLOCK_SIZE;
    const json = Buffer.from(base64 + "=".repeat(padding), "base64").toString(
      "utf8",
    );
    const payload = JSON.parse(json) as Record<string, string>;
    return (
      payload.email || payload.preferred_username || payload.sub || undefined
    );
  } catch {
    return undefined;
  }
}

function decodeJwtPayload(jwt?: string | null): Record<string, unknown> | null {
  try {
    if (!jwt || typeof jwt !== "string") return null;
    const parts = jwt.split(".");
    if (parts.length !== 3) return null;
    const base64 = (parts[1] || "").replace(/-/g, "+").replace(/_/g, "/");
    const missingPadding =
      (BASE64_BLOCK_SIZE - (base64.length % BASE64_BLOCK_SIZE)) %
      BASE64_BLOCK_SIZE;
    const padded = base64 + "=".repeat(missingPadding);
    return JSON.parse(Buffer.from(padded, "base64").toString("utf8")) as Record<string, unknown>;
  } catch {
    return null;
  }
}

function extractEmailFromAccessToken(accessToken?: string | null): string | undefined {
  const payload = decodeJwtPayload(accessToken);
  if (!payload) return undefined;
  return (
    (payload.email as string) || (payload.preferred_username as string) || (payload.sub as string) || undefined
  );
}

export async function fetchKiroProfileArn(accessToken?: string | null): Promise<string | null> {
  if (!accessToken) return null;
  try {
    const response = await fetch(
      "https://codewhisperer.us-east-1.amazonaws.com/ListAvailableProfiles",
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
          Authorization: `Bearer ${accessToken}`,
        },
        body: JSON.stringify({ maxResults: 10 }),
      },
    );
    if (!response.ok) return null;
    const data = (await response.json()) as { profiles?: Array<{ arn?: string; profileArn?: string }> };
    return data?.profiles?.[0]?.arn || data?.profiles?.[0]?.profileArn || null;
  } catch {
    return null;
  }
}

export function extractCodexAccountInfo(idToken?: string | null): {
  email?: string;
  chatgptAccountId?: string;
  chatgptPlanType?: string;
} {
  const payload = decodeJwtPayload(idToken);
  if (!payload) return {};
  const chatgpt = (payload["https://api.openai.com/auth"] as Record<string, unknown>) || {};
  return {
    email: payload.email as string | undefined,
    chatgptAccountId: (chatgpt.chatgpt_account_id as string | undefined) || (payload.account_id as string | undefined),
    chatgptPlanType: (chatgpt.chatgpt_plan_type as string | undefined) || (payload.plan_type as string | undefined),
  };
}

export {
  BASE64_BLOCK_SIZE,
  validateXaiOAuthEndpoint,
  decodeXaiIdTokenEmail,
  decodeJwtPayload,
  extractEmailFromAccessToken,
};
