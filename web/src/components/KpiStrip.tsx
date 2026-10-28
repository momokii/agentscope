/**
 * KPI strip (DESIGN sections 4 + 8): six stat cards sharing one anatomy —
 * 11px uppercase tracked label / 20px mono value / 12px delta.
 * Deltas carry a Lucide directional glyph + sign, never color alone.
 */

import { ArrowDown, ArrowUp, Minus } from "lucide-react";

import type { DateFilter } from "../lib/fleetSort";
import type { KPIs } from "../lib/sse";
import type { ConnectionPhase } from "../lib/sse";
import { formatAgeShort, formatCompact, formatUSD } from "../lib/format";

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

/**
 * Live runtime probes are OPTIONAL until the backend lands them. A finite
 * number renders like any counter; anything else renders "—" with a title
 * explaining the runtime is not reporting yet — never a fabricated zero.
 */
function liveValue(raw: number | undefined): string {
  return typeof raw === "number" && Number.isFinite(raw) ? `${raw}` : "—";
}

function usdValue(raw: number | undefined): string {
  return typeof raw === "number" && Number.isFinite(raw)
    ? formatUSD(raw)
    : "—";
}

function liveDelta(raw: number | undefined, prev: number | undefined): Delta {
  if (typeof raw !== "number" || !Number.isFinite(raw)) {
    return { tone: "mute", text: "—", direction: "flat" };
  }
  return infoDelta(raw, typeof prev === "number" ? prev : null);
}

function usdDelta(
  current: number | undefined,
  previous: number | undefined,
): Delta {
  if (typeof current !== "number" || !Number.isFinite(current)) {
    return { tone: "mute", text: "—", direction: "flat" };
  }
  if (
    typeof previous !== "number" ||
    !Number.isFinite(previous) ||
    previous === current
  ) {
    return { tone: "mute", text: "±0", direction: "flat" };
  }
  const diff = current - previous;
  return {
    tone: "run",
    text: `${diff > 0 ? "+" : "−"}${formatUSD(Math.abs(diff))}`,
    direction: diff > 0 ? "up" : "down",
  };
}

const LIVE_ABSENT_TITLE = "runtime not yet reporting";
const COST_TOTAL_ABSENT_TITLE = "lifetime cost not yet reporting";
const METRICS_AGE_ABSENT_TITLE = "message-level metrics not yet reporting";

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
  /**
   * Ms since the displayed snapshot frame was applied (App's receivedAt
   * clock). Added to the snapshot's metrics_age_ms so the age ticks live
   * every second instead of freezing at the last publish during quiet
   * (watermark-skipped) stretches. Null/unknown renders the snapshot
   * value as-is.
   */
  frameAgeMs?: number | null;
  /**
   * Which cost window the Cost card shows: lifetime totals by default,
   * message-level windowed spend under a matching date filter. The
   * standalone Cost-today card already covers the today preset; custom
   * `from` falls back to the lifetime total.
   */
  costMode?: "total" | "24h" | "7d" | "30d";
}

/** DateFilter → cost card mode. Presets map to their message-level
 * window; custom `from` and `all` fall back to the lifetime total
 * (per-day attribution needs fixed windows — see DataGuide). */
export function costModeFor(date: DateFilter): "total" | "24h" | "7d" | "30d" {
  if (date.from !== null && date.from !== undefined) return "total";
  if (date.preset === "24h" || date.preset === "7d" || date.preset === "30d") {
    return date.preset;
  }
  return "total";
}

/** Cost card content for one window mode: label, value (with lifetime
 * fallback for snapshots that predate windowed metrics), and title. */
function costCard(
  kpis: KPIs,
  mode: NonNullable<KpiStripProps["costMode"]>,
): { label: string; value: number | undefined; title: string } {
  switch (mode) {
    case "24h":
      return {
        label: "Cost 24h",
        value: kpis.cost_24h ?? kpis.cost_total,
        title: "Message spend in the trailing 24 hours (both harnesses)",
      };
    case "7d":
      return {
        label: "Cost 7d",
        value: kpis.cost_7d ?? kpis.cost_total,
        title: "Message spend in the trailing 7 days (both harnesses)",
      };
    case "30d":
      return {
        label: "Cost 30d",
        value: kpis.cost_30d ?? kpis.cost_total,
        title: "Message spend in the trailing 30 days (both harnesses)",
      };
    case "total":
    default:
      return {
        label: "Cost total",
        value: kpis.cost_total,
        title: "Lifetime spend across all sessions",
      };
  }
}

function CostCardBody({
  kpis,
  costMode,
  delta,
}: {
  kpis: KPIs;
  costMode: NonNullable<KpiStripProps["costMode"]>;
  delta: Delta;
}) {
  const card = costCard(kpis, costMode);
  return (
    <>
      <div className="kpi-label" title={card.title}>
        {card.label}
      </div>
      <div
        className="kpi-value"
        title={card.value === undefined ? COST_TOTAL_ABSENT_TITLE : undefined}
      >
        {usdValue(card.value)}
      </div>
      <DeltaLine delta={delta} />
    </>
  );
}

export default function KpiStrip({
  kpis,
  prevKpis,
  phase,
  lastSeq,
  frameAgeMs = null,
  costMode = "total",
}: KpiStripProps) {
  const prev = prevKpis ?? null;
  const tokensDelta = infoDelta(kpis.tokens_per_hour, prev?.tokens_per_hour ?? null);
  const costDelta = infoDelta(
    Math.round(kpis.cost_today * 100),
    prev ? Math.round(prev.cost_today * 100) : null,
  );
  const costTotalDelta = usdDelta(kpis.cost_total, prev?.cost_total);
  const hasMetricsAge =
    typeof kpis.metrics_age_ms === "number" &&
    Number.isFinite(kpis.metrics_age_ms);

  return (
    <section className="kpi-strip" aria-label="Fleet KPIs">
      <div className="kpi-card">
        <div
          className="kpi-label"
          title="Sessions generating now (follows the date/bucket/harness/search filters, not the status chips)"
        >
          Active
        </div>
        <div className="kpi-value">{kpis.active}</div>
        <DeltaLine delta={infoDelta(kpis.active, prev?.active ?? null)} />
      </div>
      <div className="kpi-card">
        <div
          className="kpi-label"
          title="Sessions heuristically waiting on you (user spoke last, nothing in flight); follows the date/bucket/harness/search filters"
        >
          Waiting input
        </div>
        <div className="kpi-value">{kpis.waiting_input}</div>
        <DeltaLine delta={waitingDelta(kpis.waiting_input, prev?.waiting_input ?? null)} />
      </div>
      <div className="kpi-card">
        <div
          className="kpi-label"
          title="Message tokens in the trailing 60s × 60, both harnesses — a now-rate, independent of date filters"
        >
          Tokens/hr
        </div>
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
        <div
          className="kpi-label"
          title="Message spend since local midnight, both harnesses (container date may differ from host — see TZ)"
        >
          Cost today
        </div>
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
        <CostCardBody kpis={kpis} costMode={costMode} delta={costTotalDelta} />
      </div>
      <div className="kpi-card">
        <div
          className="kpi-label"
          title="Sessions whose newest turn failed (follows the date/bucket/harness/search filters)"
        >
          Errors
        </div>
        <div className="kpi-value">{kpis.errors}</div>
        <DeltaLine delta={errorDelta(kpis.errors, prev?.errors ?? null)} />
      </div>
      <div className="kpi-card">
        <div
          className="kpi-label"
          title="Alive opencode + pi processes seen via /proc — ground truth for 'actually running'"
        >
          Live procs
        </div>
        <div
          className="kpi-value"
          title={kpis.live_processes === undefined ? LIVE_ABSENT_TITLE : undefined}
        >
          {liveValue(kpis.live_processes)}
        </div>
        <DeltaLine
          delta={liveDelta(kpis.live_processes, prev?.live_processes)}
        />
      </div>
      <div className="kpi-card">
        <div
          className="kpi-label"
          title="Non-exited panes running an agent (opencode/pi); plain shells excluded so this matches the per-project columns"
        >
          Live panes
        </div>
        <div
          className="kpi-value"
          title={kpis.live_panes === undefined ? LIVE_ABSENT_TITLE : undefined}
        >
          {liveValue(kpis.live_panes)}
        </div>
        <DeltaLine delta={liveDelta(kpis.live_panes, prev?.live_panes)} />
      </div>
      <div className="kpi-card">
        <div
          className="kpi-label"
          title="Age of message-level metrics; session totals may be fresher"
        >
          Metrics age
        </div>
        <div
          className="kpi-value"
          title={
            hasMetricsAge
              ? "Live age: snapshot sample age plus time since this frame arrived"
              : METRICS_AGE_ABSENT_TITLE
          }
        >
          {hasMetricsAge
            ? formatAgeShort(
                (kpis.metrics_age_ms as number) + (frameAgeMs ?? 0),
              )
            : "—"}
        </div>
        <DeltaLine
          delta={{
            tone: "mute",
            text: hasMetricsAge ? "msg metrics" : "—",
            direction: "flat",
          }}
        />
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
