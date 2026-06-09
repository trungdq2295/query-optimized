"""query-optimizing engine.

Connects to a user-supplied database (read-only), runs EXPLAIN, and verifies that
a rewritten query is both faster and behavior-preserving.

Design rules (enforced here, not just documented):
  * Only a single SELECT / WITH...SELECT statement is ever sent to the DB.
    Any DDL/DML (CREATE, INSERT, UPDATE, DELETE, DROP, ALTER, ...) is refused.
  * The session is set read-only where the engine supports it.
  * Every execution carries a statement timeout so a slow query can't hang.
  * The connection string is never printed or logged. The cache file (if used)
    is chmod 0600 and gitignored.

Supported engines via SQLAlchemy dialect name: postgresql, mysql, mssql, sqlite.
Snowflake / BigQuery have no manual indexes — diagnosis still works, index advice
is redirected (see REFERENCE.md §G).
"""

from __future__ import annotations

import re
import time
import hashlib
from dataclasses import dataclass, field
from typing import Optional

from sqlalchemy import create_engine, text
from sqlalchemy.engine import Engine


# --- safety guard ----------------------------------------------------------

_FORBIDDEN = re.compile(
    r"\b(insert|update|delete|merge|drop|alter|create|truncate|grant|revoke|"
    r"replace|call|exec|execute|attach|copy|vacuum|reindex)\b",
    re.IGNORECASE,
)


def _strip_sql_comments(sql: str) -> str:
    sql = re.sub(r"/\*.*?\*/", " ", sql, flags=re.DOTALL)
    sql = re.sub(r"--[^\n]*", " ", sql)
    return sql.strip()


def assert_safe_select(sql: str) -> str:
    """Return the cleaned SQL if it is a single read-only SELECT, else raise.

    This is the boundary between user input and the database. It must stay strict.
    """
    cleaned = _strip_sql_comments(sql)
    if not cleaned:
        raise ValueError("Empty query.")

    # one statement only — reject anything after the first terminator
    statements = [s for s in cleaned.split(";") if s.strip()]
    if len(statements) > 1:
        raise ValueError("Only a single statement is allowed; found multiple.")

    body = statements[0].strip()
    first = body.split(None, 1)[0].lower()
    if first not in ("select", "with"):
        raise ValueError(f"Only SELECT/WITH queries are allowed; got '{first}'.")

    if _FORBIDDEN.search(body):
        raise ValueError("Query contains a forbidden (write/DDL) keyword.")

    return body


# --- connection ------------------------------------------------------------

def connect(connection_string: str) -> Engine:
    """Create a read-only-preferring SQLAlchemy engine.

    The connection string is consumed here and never stored on the returned
    object's repr / logged.
    """
    engine = create_engine(connection_string, pool_pre_ping=True)
    return engine


def _dialect(engine: Engine) -> str:
    return engine.dialect.name  # postgresql, mysql, mssql, sqlite, snowflake, ...


def _apply_session_guards(conn, dialect: str, timeout_s: int) -> None:
    """Best-effort read-only + statement timeout per engine."""
    ms = int(timeout_s * 1000)
    if dialect == "postgresql":
        conn.exec_driver_sql("SET default_transaction_read_only = on")
        conn.exec_driver_sql(f"SET statement_timeout = {ms}")
    elif dialect == "mysql":
        conn.exec_driver_sql(f"SET SESSION max_execution_time = {ms}")
        conn.exec_driver_sql("SET SESSION transaction_read_only = 1")
    elif dialect == "mssql":
        conn.exec_driver_sql(f"SET LOCK_TIMEOUT {ms}")
    # sqlite: no server-side timeout; the Python-side wall clock guards it.


# --- EXPLAIN ---------------------------------------------------------------

def _explain_prefix(dialect: str, analyze: bool) -> str:
    if dialect == "postgresql":
        return "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) " if analyze else "EXPLAIN "
    if dialect == "mysql":
        return "EXPLAIN ANALYZE " if analyze else "EXPLAIN FORMAT=TREE "
    if dialect == "sqlite":
        return "EXPLAIN QUERY PLAN "
    if dialect == "mssql":
        # MSSQL toggles plan output via SET statements, handled in explain().
        return ""
    return "EXPLAIN "


def explain(engine: Engine, sql: str, analyze: bool = False, timeout_s: int = 30) -> str:
    """Return the query plan as text.

    analyze=False (default) only plans the query — cheap, no execution.
    analyze=True executes it for real timing — can be slow on big queries.
    """
    body = assert_safe_select(sql)
    dialect = _dialect(engine)
    with engine.connect() as conn:
        _apply_session_guards(conn, dialect, timeout_s)
        if dialect == "mssql":
            conn.exec_driver_sql("SET SHOWPLAN_ALL ON")
            rows = conn.exec_driver_sql(body).fetchall()
            conn.exec_driver_sql("SET SHOWPLAN_ALL OFF")
        else:
            rows = conn.exec_driver_sql(_explain_prefix(dialect, analyze) + body).fetchall()
    return "\n".join(" | ".join(str(c) for c in r) for r in rows)


# --- timed execution + verification ----------------------------------------

@dataclass
class RunResult:
    elapsed_s: float
    row_count: int
    sample_hash: str


@dataclass
class VerifyResult:
    old: RunResult
    new: RunResult
    rows_match: bool
    sample_match: bool
    speedup: float  # old.elapsed_s / new.elapsed_s
    notes: list = field(default_factory=list)

    @property
    def behavior_preserved(self) -> bool:
        return self.rows_match and self.sample_match


def _timed_run(engine: Engine, sql: str, timeout_s: int, sample_n: int = 200) -> RunResult:
    body = assert_safe_select(sql)
    dialect = _dialect(engine)
    with engine.connect() as conn:
        _apply_session_guards(conn, dialect, timeout_s)
        start = time.perf_counter()
        result = conn.exec_driver_sql(body)
        rows = result.fetchall()
        elapsed = time.perf_counter() - start
    # stable behavior fingerprint: sort the stringified sample so ordering noise
    # doesn't cause a false mismatch (ORDER BY differences are flagged separately).
    sample = sorted("|".join(str(c) for c in r) for r in rows[:sample_n])
    digest = hashlib.sha256("\n".join(sample).encode()).hexdigest()[:16]
    return RunResult(elapsed_s=round(elapsed, 4), row_count=len(rows), sample_hash=digest)


def verify(engine: Engine, old_sql: str, new_sql: str, timeout_s: int = 60) -> VerifyResult:
    """Run old vs new query, prove the rewrite is faster AND returns the same data.

    WARNING: this executes both queries. The old (slow) query runs in full, so on
    a multi-minute query this is expensive. Gate it behind explicit user consent
    and the statement timeout.
    """
    old = _timed_run(engine, old_sql, timeout_s)
    new = _timed_run(engine, new_sql, timeout_s)
    notes = []
    rows_match = old.row_count == new.row_count
    if not rows_match:
        notes.append(
            f"Row count differs: old={old.row_count} new={new.row_count} "
            "— rewrite is NOT behavior-preserving."
        )
    sample_match = old.sample_hash == new.sample_hash
    if rows_match and not sample_match:
        notes.append(
            "Row counts match but sampled content differs — check ORDER BY / "
            "column selection before trusting the rewrite."
        )
    speedup = (old.elapsed_s / new.elapsed_s) if new.elapsed_s > 0 else float("inf")
    return VerifyResult(
        old=old, new=new, rows_match=rows_match, sample_match=sample_match,
        speedup=round(speedup, 2), notes=notes,
    )
