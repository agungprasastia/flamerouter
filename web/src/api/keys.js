import { apiJSON } from "./client";

export function listKeys() {
  return apiJSON("/api/keys");
}

export function createKey(name) {
  return apiJSON("/api/keys", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export function deleteKey(id) {
  return apiJSON(`/api/keys/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function updateKey(id, isActive) {
  return apiJSON(`/api/keys/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify({ isActive }),
  });
}
