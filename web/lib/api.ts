import type { ApiError, BenchReport, EngineKind, QueryResult, TableInfo, TableListItem } from "./types";

export const API_BASE =
  (typeof process !== "undefined" && process.env.NEXT_PUBLIC_API) || "http://127.0.0.1:8080";

export class PrismApiError extends Error {
  status: number;
  pos?: number;
  constructor(message: string, status: number, pos?: number) {
    super(message);
    this.status = status;
    this.pos = pos;
  }
}

async function parseError(res: Response): Promise<never> {
  let msg = res.statusText || `HTTP ${res.status}`;
  let pos: number | undefined;
  try {
    const body = (await res.json()) as ApiError;
    if (body.error) msg = body.error;
    pos = body.pos;
  } catch {
    /* ignore */
  }
  throw new PrismApiError(msg, res.status, pos);
}

export async function getHealth(): Promise<{ ok: boolean; version: string; data_dir: string }> {
  const res = await fetch(`${API_BASE}/health`);
  if (!res.ok) await parseError(res);
  return res.json();
}

export async function getTables(): Promise<{ data_dir: string; tables: TableListItem[] }> {
  const res = await fetch(`${API_BASE}/tables`);
  if (!res.ok) await parseError(res);
  return res.json();
}

export async function getTable(name: string): Promise<TableInfo> {
  const res = await fetch(`${API_BASE}/tables/${encodeURIComponent(name)}`);
  if (!res.ok) await parseError(res);
  return res.json();
}

export async function runQuery(sql: string, engine: EngineKind, explain = true): Promise<QueryResult> {
  const res = await fetch(`${API_BASE}/query`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sql, engine, explain }),
  });
  if (!res.ok) await parseError(res);
  return res.json();
}

export async function getBench(): Promise<BenchReport> {
  const res = await fetch(`${API_BASE}/bench`);
  if (!res.ok) await parseError(res);
  return res.json();
}
