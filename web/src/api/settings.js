import { apiJSON } from "./client";

export function getSettings() {
  return apiJSON("/api/settings");
}

export function patchSettings(body) {
  return apiJSON("/api/settings", {
    method: "PATCH",
    body: JSON.stringify(body),
  });
}

export function getRequireLogin() {
  return apiJSON("/api/settings/require-login");
}

export function patchRequireLogin(requireLogin) {
  return apiJSON("/api/settings/require-login", {
    method: "PATCH",
    body: JSON.stringify({ requireLogin }),
  });
}
