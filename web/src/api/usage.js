import { apiJSON } from "./client";

export function stats() {
  return apiJSON("/api/usage/stats");
}

export function chart() {
  return apiJSON("/api/usage/chart");
}
