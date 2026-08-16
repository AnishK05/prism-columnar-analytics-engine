export type EngineKind = "vectorized" | "row";

export type PlanNode = {
  op: string;
  table?: string;
  columns?: string[];
  pushed?: string;
  residual?: string;
  group_by?: string[];
  aggs?: string[];
  order?: string[];
  limit?: number;
  files?: number;
  row_groups?: number;
  kept_row_groups?: number;
  skipped_row_groups?: number;
  pruned_cols?: number;
  bytes_read?: number;
  rows_in?: number;
  jobs?: number;
  child?: PlanNode;
};

export type Profile = {
  elapsed_ms: number;
  engine: string;
  jobs?: number;
  rows_read: number;
  rows_emitted: number;
  bytes_read: number;
  row_groups_total: number;
  row_groups_read: number;
  row_groups_skipped: number;
  columns_read?: number;
  plan?: PlanNode;
};

export type QueryResult = {
  columns: string[];
  types: string[];
  rows: unknown[][];
  truncated: boolean;
  profile: Profile;
};

export type ApiError = {
  error: string;
  pos?: number;
};

export type TableListItem = {
  name: string;
  files: number;
  rows: number;
  row_groups: number;
  compressed_bytes: number;
};

export type TableInfo = {
  table: string;
  dir: string;
  files: number;
  rows: number;
  row_groups: number;
  compressed_bytes: number;
  min_ts_ms?: number;
  max_ts_ms?: number;
  ts_clustering?: string;
  schema: { name: string; type: string }[];
};

export type BenchVariant = {
  name: string;
  engine: string;
  no_skip: boolean;
  no_prune: boolean;
};

export type BenchQueryRun = {
  id: string;
  showcase: string;
  variant: string;
  engine: string;
  first_run_ms: number;
  hot_median_ms: number;
  hot_p95_ms: number;
  rows_read: number;
  row_groups_read: number;
  row_groups_skipped: number;
  columns_read: number;
};

export type BenchReport = {
  schema: string;
  note: string;
  scale: string;
  rows: number;
  version?: string;
  git_sha?: string;
  variants: BenchVariant[];
  results: BenchQueryRun[];
  speedups?: { id: string; speedup_x: number }[];
};
