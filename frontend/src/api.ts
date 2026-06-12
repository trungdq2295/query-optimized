// api.ts — typed client for the Go backend. /optimize is a POST that streams
// Server-Sent Events, so we read the response body with a fetch reader and
// parse SSE frames by hand (EventSource only supports GET).

const BASE = import.meta.env.VITE_API_BASE ?? "";

export type Stage =
  | "diagnosing"
  | "proposing"
  | "verifying"
  | "baseline"
  | "done";

export interface Progress {
  stage: Stage;
  attempt?: number;
  message?: string;
}

export interface RunResult {
  elapsed_s: number;
  row_count: number;
  sample_hash: string;
}

export interface Verdict {
  status: "verified" | "behavior_changed" | "not_faster" | "unverifiable";
  behavior_preserved?: boolean;
  speedup?: number;
  old?: RunResult;
  new?: RunResult;
  notes?: string[] | null;
}

export interface Proposal {
  kind: "rewrite" | "index" | "partition" | "shard";
  original_sql: string;
  rewritten_sql?: string;
  ddl?: string;
  rationale: string;
  self_serve: boolean;
}

export interface OptimizeResult {
  proposal: Proposal;
  verdict: Verdict;
  engine: string;
  attempts: number;
  proposer: string;
  baseline_id?: string;
}

export interface RecheckResult {
  outcome: "improved" | "no_change" | "worse" | "behavior_changed";
  before: RunResult;
  after: RunResult;
  before_plan: string;
  after_plan: string;
  speedup: number;
  rows_preserved: boolean;
  notes: string[];
}

export interface Health {
  status: string;
  mode: string;
  engine: string;
}

export async function getHealth(): Promise<Health> {
  const r = await fetch(`${BASE}/health`);
  if (!r.ok) throw new Error(`health ${r.status}`);
  return r.json();
}

export async function recheck(baselineId: string): Promise<RecheckResult> {
  const r = await fetch(`${BASE}/recheck`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ baseline_id: baselineId }),
  });
  const data = await r.json();
  if (!r.ok) throw new Error(data.error || `recheck ${r.status}`);
  return data;
}

interface OptimizeHandlers {
  onProgress: (p: Progress) => void;
  onResult: (r: OptimizeResult) => void;
  onError: (msg: string) => void;
}

// optimize POSTs the SQL and consumes the SSE stream, dispatching each frame.
export async function optimize(sql: string, h: OptimizeHandlers): Promise<void> {
  const resp = await fetch(`${BASE}/optimize`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sql }),
  });
  if (!resp.body) {
    h.onError("no response stream");
    return;
  }

  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });

    // SSE frames are separated by a blank line.
    let sep: number;
    while ((sep = buf.indexOf("\n\n")) !== -1) {
      const frame = buf.slice(0, sep);
      buf = buf.slice(sep + 2);
      dispatch(frame, h);
    }
  }
}

function dispatch(frame: string, h: OptimizeHandlers): void {
  let event = "message";
  let data = "";
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) data += line.slice(5).trim();
  }
  if (!data) return;
  const parsed = JSON.parse(data);
  if (event === "progress") h.onProgress(parsed as Progress);
  else if (event === "result") h.onResult(parsed as OptimizeResult);
  else if (event === "error") h.onError(parsed.error || "unknown error");
}
