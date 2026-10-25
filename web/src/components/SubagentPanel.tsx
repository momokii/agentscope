import { useMemo } from "react";

import RelativeTime from "./RelativeTime";
import StatusBadge from "./StatusBadge";
import { formatCompact, formatUSD } from "../lib/format";
import { buildSessionHash } from "../lib/routing";
import type { SessionRow } from "../lib/sse";
import type { ExportSession } from "../lib/transcript";

export interface SubagentPanelProps {
  /** Direct children from the detail seam (transcript data). */
  children: ExportSession[];
  /**
   * Snapshot rows for ground-truth per-child status/tokens/cost/activity.
   * Status is rendered ONLY from the matching row — never inferred.
   */
  sessions: readonly SessionRow[];
  nowMs?: number;
}

export default function SubagentPanel({
  children,
  sessions,
  nowMs,
}: SubagentPanelProps) {
  const byId = useMemo(
    () => new Map(sessions.map((row) => [row.id, row])),
    [sessions],
  );
  const now = nowMs ?? Date.now();

  return (
    <section className="subagents" aria-label="Subagents">
      <div className="subagents-head">
        <span>Subagents</span>
        <span className="count mono">({children.length})</span>
      </div>
      {children.length === 0 ? (
        <div className="empty-state">No subagents in this session.</div>
      ) : (
        <ul className="subagent-list">
          {children.map((child) => {
            const row = byId.get(child.info.id);
            const agentName = row?.agent ?? child.info.agent;
            const exportModel = `${child.info.model.providerID}/${child.info.model.id}`;
            const modelLabel = row?.model ?? exportModel;
            const tokens = row?.tokens.total ?? child.info.tokens.total;
            const cost = row?.cost ?? child.info.cost;
            const updatedRelativeMs =
              row?.updated_relative_ms ??
              Math.max(0, now - child.info.time.updated);
            return (
              <li key={child.info.id}>
                <a
                  className="subagent-row"
                  data-session-id={child.info.id}
                  href={buildSessionHash(child.info.id)}
                >
                  {row !== undefined ? (
                    <StatusBadge status={row.status} />
                  ) : (
                    <span className="subagent-nostatus">
                      status unavailable
                    </span>
                  )}
                  <span className="subagent-title">{child.info.title}</span>
                  <span className="subagent-meta mono">
                    <span>{agentName ?? modelLabel}</span>
                    {agentName !== null && agentName !== undefined && (
                      <span className="subagent-model">{modelLabel}</span>
                    )}
                    <span>{formatCompact(tokens)} tok</span>
                    <span>{formatUSD(cost)}</span>
                    <RelativeTime
                      updatedRelativeMs={updatedRelativeMs}
                      nowMs={now}
                    />
                  </span>
                </a>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
