"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { PlanTree } from "@/components/PlanTree";
import { ProfileChips } from "@/components/ProfileChips";
import { loadLastRun, type LastRun } from "@/lib/session";

export default function PlanPage() {
  const [last, setLast] = useState<LastRun | null>(null);

  useEffect(() => {
    setLast(loadLastRun());
  }, []);

  if (!last) {
    return (
      <>
        <h1 className="page">Plan</h1>
        <p className="hint">
          Run a query on the <Link href="/">Workbench</Link> first. The physical plan from that
          run is stored in this tab.
        </p>
      </>
    );
  }

  const plan = last.result.profile.plan;
  return (
    <>
      <h1 className="page">Plan</h1>
      <p className="hint">
        Last run · {last.engine}. Nested list is the physical tree (scan → filter/agg → sort →
        limit).
      </p>
      <pre className="hint" style={{ whiteSpace: "pre-wrap", fontFamily: "var(--mono)" }}>
        {last.sql}
      </pre>
      <ProfileChips profile={last.result.profile} truncated={last.result.truncated} />
      {plan ? (
        <div className="plan">
          <PlanTree node={plan} />
        </div>
      ) : (
        <div className="banner warn">No plan on the last result. Re-run with explain enabled.</div>
      )}
    </>
  );
}
