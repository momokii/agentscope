/**
 * Shared activity row (ticker strip + full log view): seq stamp + glyph +
 * linked session title + one-liner + source (project · harness). One markup,
 * one set of classes — both surfaces stay identical.
 */

import type { ActivityEvent } from "../lib/activity";
import { harnessGlyph } from "../lib/activity";
import { buildSessionHash } from "../lib/routing";

export default function ActivityRow({ event }: { event: ActivityEvent }) {
  return (
    <>
      <span className="ticker-seq">seq {event.seq}</span>
      <span className="ticker-glyph" aria-hidden="true">
        {event.glyph}
      </span>
      <a
        className="ticker-session"
        href={buildSessionHash(event.sessionId)}
        title={`${event.sessionTitle} · ${event.project}`}
      >
        {event.sessionTitle}
      </a>
      <span className="ticker-text">{event.text}</span>
      <span
        className="ticker-source"
        title={`project ${event.project} · harness ${event.harness}`}
      >
        {event.project} · {harnessGlyph(event.harness)}
      </span>
    </>
  );
}
