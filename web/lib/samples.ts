export type Sample = {
  id: string;
  label: string;
  hint: string;
  sql: string;
};

export const SAMPLES: Sample[] = [
  {
    id: "Q2",
    label: "Q2 — 7-day window (skip demo)",
    hint: "On the committed fixture this keeps 1 of 4 row groups.",
    sql: `SELECT COUNT(*), SUM(amount_cents)
FROM events
WHERE ts >= TIMESTAMP '2024-01-01' AND ts < TIMESTAMP '2024-01-08';`,
  },
  {
    id: "Q6",
    label: "Q6 — resume query (filter + group + top-N)",
    hint: "Full-year ts predicate does not skip on 2024-only data. Use Q2 to see skipping.",
    sql: `SELECT country, event_type, COUNT(*), SUM(amount_cents), AVG(amount_cents)
FROM events
WHERE ts >= TIMESTAMP '2024-01-01'
  AND country IN ('US', 'CA', 'GB')
  AND amount_cents > 0
GROUP BY country, event_type
ORDER BY COUNT(*) DESC
LIMIT 20;`,
  },
  {
    id: "Q1",
    label: "Q1 — full scan aggregate (prune)",
    hint: "COUNT/SUM; only amount_cents is decoded.",
    sql: `SELECT COUNT(*), SUM(amount_cents) FROM events;`,
  },
  {
    id: "Q3",
    label: "Q3 — low-selectivity filter",
    hint: "country = US hits many row groups; vectorized filter matters more than skip.",
    sql: `SELECT COUNT(*), SUM(amount_cents) FROM events WHERE country = 'US';`,
  },
  {
    id: "Q4",
    label: "Q4 — low-cardinality GROUP BY",
    hint: "Tiny hash table, CPU-bound.",
    sql: `SELECT country, event_type, COUNT(*), SUM(amount_cents)
FROM events
GROUP BY country, event_type
ORDER BY COUNT(*) DESC, country, event_type;`,
  },
  {
    id: "Q5",
    label: "Q5 — high-cardinality GROUP BY",
    hint: "Filtered user_id groups, top 50.",
    sql: `SELECT user_id, COUNT(*)
FROM events
WHERE country = 'US'
GROUP BY user_id
ORDER BY COUNT(*) DESC, user_id
LIMIT 50;`,
  },
  {
    id: "Q7",
    label: "Q7 — string IN-list",
    hint: "Dictionary / byte-compare path.",
    sql: `SELECT COUNT(*) FROM events WHERE event_type IN ('purchase', 'refund');`,
  },
  {
    id: "Q8",
    label: "Q8 — narrow projection",
    hint: "Three columns. Compare with Q8-wide.",
    sql: `SELECT event_id, ts, country
FROM events
WHERE country = 'US'
ORDER BY event_id
LIMIT 20;`,
  },
  {
    id: "Q8-wide",
    label: "Q8-wide — SELECT * (prune contrast)",
    hint: "Same filter as Q8, all columns.",
    sql: `SELECT *
FROM events
WHERE country = 'US'
ORDER BY event_id
LIMIT 20;`,
  },
];

export const DEFAULT_SAMPLE_ID = "Q2";
