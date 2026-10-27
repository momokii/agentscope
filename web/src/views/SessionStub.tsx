/**
 * Session summary stub (W2-2): minimal `#/sessions/:id` target.
 *
 * Reuses Snapshot row data only — NO new fetching, NO fabricated detail.
 * The full drill-down (Transcript/Timeline/Tools/Cost/Raw) lands in Wave 3;
 * a visible note states that boundary so nobody mistakes this for detail.
 */

import { ArrowLeft } from "lucide-react";

import type { SessionRow } from "../lib/sse";
import { formatCompact, formatUSD } from "../lib/format";
import HarnessBadge from "../components/HarnessBadge";
import StatusBadge from "../components/StatusBadge";
import RelativeTime from "../components/RelativeTime";
import Sparkline from "../components/Sparkline";
import { buildFleetHash } from "../lib/routing";

export interface SessionStubProps {
  row: SessionRow | null;
  id: string;
}

export default function SessionStub({ row, id }: SessionStubProps) {
  return (
    <div className="session-stub">
      <a className="stub-back mono" href={buildFleetHash()}>
        <ArrowLeft aria-hidden="true" size={12} />
        <span>back to fleet</span>
      </a>
      {row === null ? (
        <div className="empty-state">
          No session “{id}” in the latest snapshot.
        </div>
      ) : (
        <>
          <div className="stub-head">
            <StatusBadge status={row.status} />
            <HarnessBadge harness={row.harness} />
            <span className="stub-title">{row.title}</span>
          </div>
          <dl className="stub-facts mono">
            <div>
              <dt>id</dt>
              <dd>{row.id}</dd>
            </div>
            <div>
              <dt>project</dt>
              <dd>{row.project}</dd>
            </div>
            <div>
              <dt>cwd</dt>
              <dd>{row.pane.cwd !== "" ? row.pane.cwd : row.directory}</dd>
            </div>
            <div>
              <dt>model</dt>
              <dd>{row.model}</dd>
            </div>
            <div>
              <dt>tokens</dt>
              <dd>
                {formatCompact(row.tokens.total)} (in{" "}
                {formatCompact(row.tokens.in)} / out{" "}
                {formatCompact(row.tokens.out)})
              </dd>
            </div>
            <div>
              <dt>cost</dt>
              <dd>{formatUSD(row.cost)}</dd>
            </div>
            <div>
              <dt>updated</dt>
              <dd>
                <RelativeTime updatedRelativeMs={row.updated_relative_ms} />
              </dd>
            </div>
            <div>
              <dt>subagents</dt>
              <dd>{row.subagent_count}</dd>
            </div>
          </dl>
          <div className="stub-trend">
            <Sparkline data={row.spark} />
          </div>
        </>
      )}
      <p className="stub-note">
        Full drill-down (transcript, timeline, tools, cost) lands in Wave 3 —
        this is a summary stub over Snapshot data only.
      </p>
    </div>
  );
}
