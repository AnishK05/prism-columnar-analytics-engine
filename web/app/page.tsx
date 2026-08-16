"use client";

import { useMemo, useState } from "react";
import { ProfileChips } from "@/components/ProfileChips";
import { ResultsTable } from "@/components/ResultsTable";
import { PrismApiError, runQuery } from "@/lib/api";
import { DEFAULT_SAMPLE_ID, SAMPLES } from "@/lib/samples";
import { saveLastRun } from "@/lib/session";
import type { EngineKind, QueryResult } from "@/lib/types";

export default function WorkbenchPage() {
  const initial = SAMPLES.find((s) => s.id === DEFAULT_SAMPLE_ID) ?? SAMPLES[0];
  const [sampleId, setSampleId] = useState(initial.id);
  const [sql, setSql] = useState(initial.sql);
  const [engine, setEngine] = useState<EngineKind>("vectorized");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [result, setResult] = useState<QueryResult | null>(null);

  const sample = useMemo(() => SAMPLES.find((s) => s.id === sampleId), [sampleId]);

  function pickSample(id: string) {
    const s = SAMPLES.find((x) => x.id === id);
    if (!s) return;
    setSampleId(id);
    setSql(s.sql);
    setErr("");
  }

  async function onRun() {
    setBusy(true);
    setErr("");
    try {
      const res = await runQuery(sql, engine, true);
      setResult(res);
      saveLastRun({ sql, engine, result: res });
    } catch (e: unknown) {
      setResult(null);
      if (e instanceof PrismApiError) {
        setErr(e.pos != null ? `${e.message} (column ${e.pos})` : e.message);
      } else {
        setErr("Request failed. Is prismd running on 127.0.0.1:8080?");
      }
    } finally {
      setBusy(false);
    }
  }

  const skip = result?.profile.row_groups_skipped ?? 0;
  const total = result?.profile.row_groups_total ?? 0;

  return (
    <>
      <h1 className="page">Workbench</h1>
      <p className="hint">
        Run Prism SQL against the catalog prismd loaded. Default sample is <strong>Q2</strong> so a
        first click shows row-group skipping on the committed fixture. <strong>Q6</strong> is the
        resume-style GROUP BY.
      </p>
      <div className="row">
        <label className="inline" htmlFor="sample">
          Sample
        </label>
        <select id="sample" value={sampleId} onChange={(e) => pickSample(e.target.value)}>
          {SAMPLES.map((s) => (
            <option key={s.id} value={s.id}>
              {s.label}
            </option>
          ))}
        </select>
        <span className="inline">Engine</span>
        <button
          type="button"
          className={`toggle${engine === "vectorized" ? " on" : ""}`}
          onClick={() => setEngine("vectorized")}
        >
          vectorized
        </button>
        <button
          type="button"
          className={`toggle${engine === "row" ? " on" : ""}`}
          onClick={() => setEngine("row")}
        >
          row-at-a-time
        </button>
        <button type="button" className="primary" onClick={onRun} disabled={busy}>
          {busy ? "Running…" : "Run"}
        </button>
      </div>
      {sample && <p className="hint">{sample.hint}</p>}
      <textarea value={sql} onChange={(e) => setSql(e.target.value)} spellCheck={false} />
      {err && <div className="banner error">{err}</div>}
      {result && (
        <>
          <ProfileChips profile={result.profile} truncated={result.truncated} />
          {skip > 0 && total > 0 && (
            <div className="banner ok">
              Skipped <strong>{skip}</strong> of <strong>{total}</strong> row groups using Parquet
              min/max on <code>ts</code>. Those groups were never decoded.
            </div>
          )}
          {skip === 0 && sampleId === "Q6" && (
            <div className="banner info">
              Q6&apos;s <code>ts &gt;= 2024-01-01</code> matches the whole clustered year on this
              fixture, so nothing is skipped. Switch to Q2 for the zone-map demo.
            </div>
          )}
          <ResultsTable result={result} />
        </>
      )}
    </>
  );
}
