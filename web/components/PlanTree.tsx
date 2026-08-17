import type { PlanNode } from "@/lib/types";

function bits(n: PlanNode): string[] {
  const out: string[] = [];
  if (n.table) out.push(`table=${n.table}`);
  if (n.columns?.length) out.push(`cols=[${n.columns.join(", ")}]`);
  if (n.pruned_cols) out.push(`pruned=${n.pruned_cols}`);
  if (n.row_groups != null) {
    out.push(`rg kept=${n.kept_row_groups ?? "?"} / ${n.row_groups} skipped=${n.skipped_row_groups ?? 0}`);
  }
  if (n.pushed) out.push(`pushed=${n.pushed}`);
  if (n.residual) out.push(`residual=${n.residual}`);
  if (n.group_by?.length) out.push(`group=[${n.group_by.join(", ")}]`);
  if (n.aggs?.length) out.push(`aggs=[${n.aggs.join(", ")}]`);
  if (n.order?.length) out.push(`order=${n.order.join(", ")}`);
  if (n.limit) out.push(`limit=${n.limit}`);
  if (n.rows_in != null) out.push(`rows_in=${n.rows_in}`);
  if (n.bytes_read) out.push(`bytes=${n.bytes_read}`);
  if (n.jobs && n.jobs > 1) out.push(`jobs=${n.jobs}`);
  return out;
}

export function PlanTree({ node }: { node: PlanNode }) {
  return (
    <div className="plan-node">
      <div className="head">{node.op}</div>
      {bits(node).map((b) => (
        <div className="kv" key={b}>
          {b}
        </div>
      ))}
      {node.child && <PlanTree node={node.child} />}
    </div>
  );
}
