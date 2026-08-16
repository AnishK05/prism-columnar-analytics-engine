"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { API_BASE, getHealth } from "@/lib/api";
import { Sidebar } from "./Sidebar";

const LINKS = [
  { href: "/", label: "Workbench" },
  { href: "/plan", label: "Plan" },
  { href: "/bench", label: "Bench" },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const path = usePathname();
  const [health, setHealth] = useState<string>("checking prismd…");
  const [ok, setOk] = useState(false);

  useEffect(() => {
    getHealth()
      .then((h) => {
        setOk(true);
        setHealth(`prismd ${h.version}`);
      })
      .catch(() => {
        setOk(false);
        setHealth(`offline · ${API_BASE}`);
      });
  }, []);

  return (
    <div className="app">
      <header className="topbar">
        <Link href="/" className="brand">
          Prism <span>OLAP</span>
        </Link>
        <nav className="nav">
          {LINKS.map((l) => (
            <Link key={l.href} href={l.href} className={path === l.href ? "active" : ""}>
              {l.label}
            </Link>
          ))}
        </nav>
        <div className={`status ${ok ? "ok" : "bad"}`}>{health}</div>
      </header>
      <div className="shell">
        <Sidebar />
        <main className="main">{children}</main>
      </div>
    </div>
  );
}
