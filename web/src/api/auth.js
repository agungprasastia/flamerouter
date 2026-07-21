import { apiJSON } from "./client";

export function login(password) {
  return apiJSON("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ password }),
  });
}

export function logout() {
  return apiJSON("/api/auth/logout", { method: "POST", body: "{}" });
}

export function status() {
  return apiJSON("/api/auth/status");
}

export function requireLogin() {
  return apiJSON("/api/settings/require-login");
}
