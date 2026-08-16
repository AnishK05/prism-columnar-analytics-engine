"use client";

import { useEffect, useState } from "react";
import { getTable, getTables, PrismApiError } from "@/lib/api";
import type { TableInfo, TableListItem } from "@/lib/types";

export function Sidebar() {
  const [tables, setTables] = useState<TableListItem[]>([]);
  const [dataDir, setDataDir] = useState("");
  const [selected, setSelected] = useState<string>("events");
  const [info, setInfo] = useState<TableInfo | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    getTables()
      .then((r) => {
        setTables(r.tables);
        setDataDir(r.data_dir);
        if (r.tables.some((t) => t.name === "events")) setSelected("events");
        else if (r.tables[0]) setSelected(r.tables[0].name);
      })
      .catch((e: unknown) => {
        setErr(e instanceof PrismApiError ? e.message : "prismd is not reachable");
      });
  }, []);

  useEffect(() => {
    if (!selected) return;
    getTable(selected)
      .then(setInfo)
      .catch(() => setInfo(null));
  }, [selected]);

  return (
    <aside className="sidebar">
      <h2>Tables</h2>
      {err && <div className="banner error">{err}. Start prismd — see docs/WINDOWS.md §7–8.</div>}
      {dataDir && <div className="tbl-meta" style={{ marginBottom: 8 }}>{dataDir}</div>}
      {tables.map((t) => (
        <button
          key={t.name}
          className={`tbl-btn${selected === t.name ? " active" : ""}`}
          onClick={() => setSelected(t.name)}
          type="button"
        >
          {t.name}
          <span className="tbl-meta">
            {t.rows.toLocaleString()} rows · {t.row_groups} rg · {t.files} files
          </span>
        </button>
      ))}
      {info && (
        <div className="schema">
          <h2>Schema · {info.table}</h2>
          <table>
            <tbody>
              {info.schema.map((f) => (
                <tr key={f.name}>
                  <td>{f.name}</td>
                  <td>{f.type}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {info.ts_clustering && (
            <div className="cluster">
              ts clustered: {info.ts_clustering}
              {info.min_ts_ms != null && info.max_ts_ms != null && (
                <div>
                  {new Date(info.min_ts_ms).toISOString().slice(0, 10)} …{" "}
                  {new Date(info.max_ts_ms).toISOString().slice(0, 10)}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </aside>
  );
}
