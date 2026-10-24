/**
 * KPI strip (DESIGN sections 4 + 8): six stat cards sharing one anatomy —
 * 11px uppercase tracked label / 20px mono value / 12px delta.
 * Deltas carry a Lucide directional glyph + sign, never color alone.
 */

import { ArrowDown, ArrowUp, Minus } from "lucide-react";

import type { KPIs } from "../lib/sse";
import type { ConnectionPhase } from "../lib/sse";
import { formatCompact, formatUSD } from "../lib/format";

type DeltaTone = "run" | "ok" | "warn" | "err" | "mute";

interface Delta {
  tone: DeltaTone;
  text: string;
  direction: "up" | "down" | "flat";
}

function signed(current: number, previous: number | null): Delta {
  if (previous === null || previous === undefined || current === previous) {
    return { tone: "mute", text: "±0", direction: "flat" };
  }
  const diff = current - previous;
  return {
    tone: "mute",
    text: `${diff > 0 ? "+" : "−"}${Math.abs(diff)}`,
    direction: diff > 0 ? "up" : "down",
  };
}

function withTone(delta: Delta, tone: DeltaTone): Delta {
  return delta.direction === "flat" ? delta : { ...delta, tone };
}

/** errors: up is bad (err), down is good (ok). */
function errorDelta(current: number, previous: number | null): Delta {
  const base = signed(current, previous);
  if (base.direction === "flat") return base;
  return withTone(base, base.direction === "up" ? "err" : "ok");
}

/** waiting-input: up is stale-risk (warn), down is relief (ok). */
function waitingDelta(current: number, previous: number | null): Delta {
  const base = signed(current, previous);
  if (base.direction === "flat") return base;
  return withTone(base, base.direction === "up" ? "warn" : "ok");
}

/** informational counters: nonzero change rides the accent, never alone. */
function infoDelta(current: number, previous: number | null): Delta {
  return withTone(signed(current, previous), "run");
}

function DeltaIcon({ direction }: { direction: Delta["direction"] }) {
  if (direction === "up") return <ArrowUp aria-hidden="true" />;
  if (direction === "down") return <ArrowDown aria-hidden="true" />;
  return <Minus aria-hidden="true" />;
}

function DeltaLine({ delta }: { delta: Delta }) {
  return (
    <div className={`kpi-delta kpi-delta-${delta.tone}`}>
      <DeltaIcon direction={delta.direction} />
      <span>{delta.text}</span>
    </div>
  );
}

function sseDotClass(phase: ConnectionPhase): string {
  if (phase === "connected") return "sse-dot-ok";
  if (phase === "closed") return "sse-dot-mute";
  return "sse-dot-warn";
}

function sseValue(phase: ConnectionPhase): string {
  if (phase === "connected") return "LIVE";
  if (phase === "closed") return "OFF";
  if (phase === "reconnecting") return "RETRY";
  return "LINK";
}

export interface KpiStripProps {
  kpis: KPIs;
  /** Previous tick for deltas; null renders neutral ±0. */
  prevKpis?: KPIs | null;
  phase: ConnectionPhase;
  lastSeq: number;
}

export default function KpiStrip({ kpis, prevKpis, phase, lastSeq }: KpiStripProps) {
  const prev = prevKpis ?? null;
  const tokensDelta = infoDelta(kpis.tokens_per_hour, prev?.tokens_per_hour ?? null);
  const costDelta = infoDelta(
    Math.round(kpis.cost_today * 100),
    prev ? Math.round(prev.cost_today * 100) : null,
  );

  return (
    <section className="kpi-strip" aria-label="Fleet KPIs">
      <div className="kpi-card">
        <div className="kpi-label">Active</div>
        <div className="kpi-value">{kpis.active}</div>
        <DeltaLine delta={infoDelta(kpis.active, prev?.active ?? null)} />
      </div>
      <div className="kpi-card">
        <div className="kpi-label">Waiting input</div>
        <div className="kpi-value">{kpis.waiting_input}</div>
        <DeltaLine delta={waitingDelta(kpis.waiting_input, prev?.waiting_input ?? null)} />
      </div>
      <div className="kpi-card">
        <div className="kpi-label">Tokens/hr</div>
        <div className="kpi-value">{formatCompact(kpis.tokens_per_hour)}</div>
        <DeltaLine
          delta={{
            ...tokensDelta,
            text:
              tokensDelta.direction === "flat"
                ? "±0"
                : `${tokensDelta.direction === "up" ? "+" : "−"}${formatCompact(
                    Math.abs(kpis.tokens_per_hour - (prev?.tokens_per_hour ?? kpis.tokens_per_hour)),
                  )}/h`,
          }}
        />
      </div>
      <div className="kpi-card">
        <div className="kpi-label">Cost today</div>
        <div className="kpi-value">{formatUSD(kpis.cost_today)}</div>
        <DeltaLine
          delta={{
            ...costDelta,
            text:
              costDelta.direction === "flat"
                ? "±0"
                : `${costDelta.direction === "up" ? "+" : "−"}${formatUSD(
                    Math.abs(kpis.cost_today - (prev?.cost_today ?? kpis.cost_today)),
                  )}`,
          }}
        />
      </div>
      <div className="kpi-card">
        <div className="kpi-label">Errors</div>
        <div className="kpi-value">{kpis.errors}</div>
        <DeltaLine delta={errorDelta(kpis.errors, prev?.errors ?? null)} />
      </div>
      <div className="kpi-card">
        <div className="kpi-label">SSE stream</div>
        <div className="kpi-value">{sseValue(phase)}</div>
        <div className="kpi-delta">
          <span className={`sse-dot ${sseDotClass(phase)}`} aria-hidden="true" />
          <span>
            {phase === "connected"
              ? `seq ${lastSeq}`
              : phase === "reconnecting"
                ? "reconnecting…"
                : phase === "closed"
                  ? "stream paused"
                  : "connecting…"}
          </span>
        </div>
      </div>
    </section>
  );
}
