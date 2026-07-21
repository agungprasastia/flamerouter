import { apiJSON } from "./client";

export function listProviders() {
  return apiJSON("/api/providers");
}

export function getProvider(id) {
  return apiJSON(`/api/providers/${encodeURIComponent(id)}`);
}

export function createConnection(body) {
  return apiJSON("/api/providers/connections", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export function listProviderModels(id) {
  return apiJSON(`/api/providers/${encodeURIComponent(id)}/models`);
}
