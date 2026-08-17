"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { getBench, PrismApiError } from "@/lib/api";
import type { BenchReport } from "@/lib/types";

const COLORS: Record<string, string> = {
  "row-naive": "#f85149",
  "row-opt": "#f0b429",
  vectorized: "#3dd6c6",
};

export default function BenchPage() {
  const [report, setReport] = useState<BenchReport | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    getBench()
      .then(setReport)
      .catch((e: unknown) => {
        setErr(e instanceof PrismApiError ? e.message : "Could not load /bench");
      });
  }, []);

  const timing = useMemo(() => {
    if (!report) return [];
    const ids: string[] = [];
    const by: Record<string, { id: string; [variant: string]: string | number }> = {};
    for (const r of report.results) {
      if (!by[r.id]) {
        by[r.id] = { id: r.id };
        ids.push(r.id);
      }
      by[r.id][r.variant] = r.hot_median_ms;
    }
    return ids.map((id) => by[id]);
  }, [report]);

  const skips = useMemo(() => {
    if (!report) return [];
    const ids: string[] = [];
    const by: Record<string, { id: string; skipped: number; read: number; pct: number }> = {};
    for (const r of report.results) {
      if (r.variant !== "vectorized") continue;
      if (!by[r.id]) ids.push(r.id);
      const total = r.row_groups_read + r.row_groups_skipped;
      by[r.id] = {
        id: r.id,
        skipped: r.row_groups_skipped,
        read: r.row_groups_read,
        pct: total ? (100 * r.row_groups_skipped) / total : 0,
      };
    }
    return ids.map((id) => by[id]);
  }, [report]);

  return (
    <>
      <h1 className="page">Bench</h1>
      <p className="hint">
        Loaded from <code>GET /bench</code> (checked-in testdata sample unless you pointed prismd
        at another JSON). <strong>Not resume numbers.</strong> Lead with vectorized vs row-naive
        (no skip/prune). Measure laptop scale on Windows before rewriting the bullet.
      </p>
      {err && <div className="banner error">{err}</div>}
      {report && (
        <div className="banner warn">
          {report.note} Scale={report.scale}, rows={report.rows.toLocaleString()}.
        </div>
      )}
      <div className="charts">
        <div className="chart-card">
          <h3>Hot-cache median (ms) — 3-way breakdown</h3>
          <div style={{ width: "100%", height: 360 }}>
            <ResponsiveContainer>
              <BarChart data={timing}>
                <CartesianGrid stroke="#2a3340" vertical={false} />
                <XAxis dataKey="id" stroke="#8b9bb0" />
                <YAxis stroke="#8b9bb0" />
                <Tooltip
                  contentStyle={{ background: "#161b22", border: "1px solid #2a3340" }}
                />
                <Legend />
                <Bar dataKey="row-naive" fill={COLORS["row-naive"]} name="row-naive" />
                <Bar dataKey="row-opt" fill={COLORS["row-opt"]} name="row-opt" />
                <Bar dataKey="vectorized" fill={COLORS.vectorized} name="vectorized" />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
        <div className="chart-card">
          <h3>Row groups skipped (%) — vectorized</h3>
          <div style={{ width: "100%", height: 280 }}>
            <ResponsiveContainer>
              <BarChart data={skips}>
                <CartesianGrid stroke="#2a3340" vertical={false} />
                <XAxis dataKey="id" stroke="#8b9bb0" />
                <YAxis stroke="#8b9bb0" domain={[0, 100]} />
                <Tooltip
                  contentStyle={{ background: "#161b22", border: "1px solid #2a3340" }}
                />
                <Bar dataKey="pct" fill="#3dd6c6" name="% skipped" />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      </div>
      {report?.speedups && report.speedups.length > 0 && (
        <p className="hint" style={{ marginTop: 16 }}>
          Fixture speedups (vectorized vs row-naive, hot median):{" "}
          {report.speedups.map((s) => `${s.id} ${s.speedup_x.toFixed(1)}×`).join(" · ")}
        </p>
      )}
    </>
  );
}
