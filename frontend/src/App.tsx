import { useEffect, useState } from "react";
import {
  optimize,
  recheck,
  getHealth,
  type OptimizeResult,
  type RecheckResult,
  type Stage,
  type Health,
} from "./api";

const STEPS: { stage: Stage; label: string }[] = [
  { stage: "diagnosing", label: "Diagnosing (EXPLAIN)" },
  { stage: "proposing", label: "Proposing" },
  { stage: "verifying", label: "Verifying (run old vs new)" },
];

const DEFAULT_SQL = `SELECT c.id, c.name,
       (SELECT COUNT(*) FROM orders o
        WHERE o.customer_id = c.id AND o.status = 'shipped') AS shipped
FROM customers c
WHERE c.country = 'US'`;

export default function App() {
  const [sql, setSql] = useState(DEFAULT_SQL);
  const [running, setRunning] = useState(false);
  const [active, setActive] = useState<Stage | null>(null);
  const [reached, setReached] = useState<Set<Stage>>(new Set());
  const [result, setResult] = useState<OptimizeResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [health, setHealth] = useState<Health | null>(null);

  useEffect(() => {
    getHealth().then(setHealth).catch(() => setHealth(null));
  }, []);

  async function run() {
    setRunning(true);
    setResult(null);
    setError(null);
    setReached(new Set());
    setActive("diagnosing");
    try {
      await optimize(sql, {
        onProgress: (p) => {
          setActive(p.stage);
          setReached((prev) => new Set(prev).add(p.stage));
        },
        onResult: (r) => setResult(r),
        onError: (msg) => setError(msg),
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
      setActive(null);
    }
  }

  return (
    <div className="wrap">
      <header>
        <div className="brand">
          <div className="logo">q</div>
          <div>
            <h1>qopt</h1>
            <p>connect · diagnose · verify — prove a SQL rewrite is faster <i>and</i> safe</p>
          </div>
        </div>
        <div className="modepill">
          mode: <b>{health?.mode ?? "…"}</b>
          {health?.engine ? <> · {health.engine}</> : null}
        </div>
      </header>

      <div className="card">
        <div className="row">
          <span className="hint">
            {health?.mode === "hosted"
              ? "hosted mode runs against the shared demo database — no connection string needed"
              : "runs against the server's configured database"}
          </span>
        </div>

        <textarea value={sql} onChange={(e) => setSql(e.target.value)} spellCheck={false} />

        <div className="actions">
          <button onClick={run} disabled={running || !sql.trim()}>
            {running ? "Working…" : "Optimize ▶"}
          </button>
        </div>

        {(running || reached.size > 0) && (
          <div className="steps on">
            {STEPS.map((s) => {
              const isActive = active === s.stage;
              const isDone = reached.has(s.stage) && !isActive;
              return (
                <div key={s.stage} className={`step ${isActive ? "run" : isDone ? "done" : ""}`}>
                  <span className="dot" />
                  {s.label}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {error && (
        <div className="card error">
          <div className="sec-h"><span className="ic">⚠️</span>Could not optimize</div>
          <pre className="sql">{error}</pre>
        </div>
      )}

      {result && <Results result={result} />}

      <footer>qopt · AI proposes, deterministic Go verifies · nothing predicted, everything measured</footer>
    </div>
  );
}

function Results({ result }: { result: OptimizeResult }) {
  const { proposal, verdict } = result;
  const isRewrite = proposal.kind === "rewrite";

  return (
    <div className="results on">
      <div className="card">
        <div className="sec-h"><span className="ic">🔎</span>Why it's slow</div>
        <p style={{ margin: 0 }}>{proposal.rationale}</p>
        <div className="advice">
          proposer: <code>{result.proposer}</code> · engine: <code>{result.engine}</code> · attempts: {result.attempts}
        </div>
      </div>

      {isRewrite ? (
        <RewriteCard result={result} />
      ) : (
        <SystemChangeCard result={result} />
      )}

      {verdict.notes && verdict.notes.length > 0 && !isRewrite && (
        <div className="card">
          <div className="sec-h"><span className="ic">📋</span>Notes</div>
          <ul className="why">
            {verdict.notes.map((n, i) => (
              <li key={i}>{n}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function RewriteCard({ result }: { result: OptimizeResult }) {
  const { proposal, verdict } = result;
  const verified = verdict.status === "verified";
  return (
    <div className="card">
      <div className="sec-h">
        <span className="ic">✏️</span>Rewritten query
        <span
          className={`badge ${verified ? "ok" : "bad"}`}
          style={{ marginLeft: "auto" }}
        >
          {verified ? "✓ VERIFIED" : `✗ ${verdict.status.toUpperCase()}`}
        </span>
      </div>
      <CopyBtn text={proposal.rewritten_sql ?? ""} />
      <pre className="sql">{proposal.rewritten_sql}</pre>

      {verdict.old && verdict.new && (
        <div className="metrics">
          <Metric k="Old" v={`${verdict.old.elapsed_s.toFixed(3)} s`} />
          <Metric k="New" v={`${verdict.new.elapsed_s.toFixed(3)} s`} />
          <Metric k="Speedup" v={verdict.speedup ? `${verdict.speedup.toFixed(1)}×` : "—"} big />
          <Metric k="Rows" v={verdict.behavior_preserved ? "match ✓" : "CHANGED ✗"} />
        </div>
      )}
      {verified && verdict.new && (
        <div className="advice">
          behavior-preserving: row count + sample hash identical (
          <code>{verdict.new.sample_hash}</code>). Safe to use.
        </div>
      )}
    </div>
  );
}

function SystemChangeCard({ result }: { result: OptimizeResult }) {
  const { proposal, baseline_id } = result;
  const [state, setState] = useState<"idle" | "loading" | "done" | "error">("idle");
  const [rc, setRc] = useState<RecheckResult | null>(null);
  const [err, setErr] = useState<string | null>(null);

  async function verifyAfter() {
    if (!baseline_id) return;
    setState("loading");
    try {
      setRc(await recheck(baseline_id));
      setState("done");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
      setState("error");
    }
  }

  const confirmed = state === "done" && rc;
  const label = proposal.kind === "index" ? "Index recommendation" : `${proposal.kind} recommendation`;

  return (
    <div className="card">
      <div className="sec-h">
        <span className="ic">⚡</span>{label}
        <span
          className={`badge ${confirmed ? "ok" : "warn"}`}
          style={{ marginLeft: "auto" }}
        >
          {confirmed ? "✓ CONFIRMED" : "→ NEEDS ENGINEER"}
        </span>
      </div>

      {proposal.ddl && (
        <>
          <CopyBtn text={proposal.ddl} />
          <pre className="sql">{proposal.ddl}</pre>
        </>
      )}
      <div className="advice">
        qopt <b>never changes your schema</b> — hand this to your DBA / engineer to apply.
      </div>

      {baseline_id && state !== "done" && (
        <>
          <div className="advice" style={{ marginTop: 10, borderTop: "1px dashed var(--line)", paddingTop: 10 }}>
            Baseline captured (<code>{baseline_id}</code>). After the engineer applies the change,
            come back and prove the real effect — measured, not predicted:
          </div>
          <div className="actions" style={{ justifyContent: "flex-start", marginTop: 8 }}>
            <button className="ghost" onClick={verifyAfter} disabled={state === "loading"}>
              {state === "loading" ? "Re-running…" : "Verify after applied"}
            </button>
          </div>
        </>
      )}

      {state === "error" && <pre className="sql">{err}</pre>}

      {confirmed && rc && (
        <div style={{ marginTop: 10 }}>
          <div className="metrics">
            <Metric k="Before" v={`${rc.before.elapsed_s.toFixed(4)} s`} />
            <Metric k="After" v={`${rc.after.elapsed_s.toFixed(4)} s`} />
            <Metric k="Speedup" v={`${rc.speedup.toFixed(1)}×`} big={rc.outcome === "improved"} />
            <Metric k="Rows" v={rc.rows_preserved ? "match ✓" : "CHANGED ✗"} />
          </div>
          {rc.notes.map((n, i) => (
            <div className="advice" key={i}>{n}</div>
          ))}
        </div>
      )}
    </div>
  );
}

function Metric({ k, v, big }: { k: string; v: string; big?: boolean }) {
  return (
    <div className="metric">
      <div className="k">{k}</div>
      <div className={`v ${big ? "big" : ""}`}>{v}</div>
    </div>
  );
}

function CopyBtn({ text }: { text: string }) {
  const [label, setLabel] = useState("copy");
  return (
    <span
      className="copy"
      onClick={() => {
        navigator.clipboard?.writeText(text);
        setLabel("copied ✓");
        setTimeout(() => setLabel("copy"), 1200);
      }}
    >
      {label}
    </span>
  );
}
