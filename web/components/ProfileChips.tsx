import type { Profile } from "@/lib/types";

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / 1024 / 1024).toFixed(1)} MiB`;
}

export function ProfileChips({ profile, truncated }: { profile: Profile; truncated?: boolean }) {
  const skip = profile.row_groups_skipped;
  const total = profile.row_groups_total || skip + profile.row_groups_read;
  return (
    <div className="chips">
      <span className="chip">
        <strong>{profile.elapsed_ms.toFixed(2)} ms</strong> wall
      </span>
      <span className="chip">
        engine <strong>{profile.engine}</strong>
        {profile.jobs ? ` · ${profile.jobs} jobs` : ""}
      </span>
      <span className="chip">
        read <strong>{profile.rows_read.toLocaleString()}</strong> rows
      </span>
      <span className="chip">
        emitted <strong>{profile.rows_emitted.toLocaleString()}</strong>
      </span>
      <span className={`chip${skip > 0 ? " hot" : ""}`}>
        skipped <strong>{skip}</strong> / {total} row groups
      </span>
      {profile.columns_read != null && (
        <span className="chip">
          cols <strong>{profile.columns_read}</strong>
        </span>
      )}
      <span className="chip">{fmtBytes(profile.bytes_read)} decoded</span>
      {truncated && <span className="chip">truncated</span>}
    </div>
  );
}
