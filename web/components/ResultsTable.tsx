import type { QueryResult } from "@/lib/types";

function cell(v: unknown): string {
  if (v === null || v === undefined) return "NULL";
  if (typeof v === "number") return Number.isInteger(v) ? String(v) : String(v);
  return String(v);
}

export function ResultsTable({ result }: { result: QueryResult }) {
  if (!result.columns?.length) {
    return <div className="banner info">No columns in the result.</div>;
  }
  return (
    <div className="results">
      <table>
        <thead>
          <tr>
            {result.columns.map((c, i) => (
              <th key={c + i} title={result.types?.[i]}>
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {result.rows.map((row, ri) => (
            <tr key={ri}>
              {row.map((v, ci) => (
                <td key={ci}>{cell(v)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
