import type { EngineKind, QueryResult } from "./types";

const KEY = "prism:last";

export type LastRun = {
  sql: string;
  engine: EngineKind;
  result: QueryResult;
};

export function saveLastRun(run: LastRun): void {
  if (typeof window === "undefined") return;
  sessionStorage.setItem(KEY, JSON.stringify(run));
}

export function loadLastRun(): LastRun | null {
  if (typeof window === "undefined") return null;
  const raw = sessionStorage.getItem(KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as LastRun;
  } catch {
    return null;
  }
}
