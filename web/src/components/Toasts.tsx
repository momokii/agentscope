/**
 * Toasts: the in-app alert surface. Every alert lands here as a dismissible
 * card (linking to its session when it has one) — this is also the fallback
 * surface when the Notification API is unavailable (non-secure contexts like
 * plain-HTTP tailnet IPs can never grant permission).
 */

import { buildSessionHash } from "../lib/routing";
import type { AlertEvent } from "../lib/alerts";

export interface Toast extends AlertEvent {
  /** Unique per toast (alerts may repeat across ticks). */
  toastId: number;
}

export default function Toasts({
  toasts,
  onDismiss,
}: {
  toasts: readonly Toast[];
  onDismiss: (toastId: number) => void;
}) {
  if (toasts.length === 0) return null;
  return (
    <div className="toasts" role="status" aria-label="Alerts">
      {toasts.map((toast) => (
        <div key={toast.toastId} className="toast mono">
          <div className="toast-head">
            <span className="toast-title">{toast.title}</span>
            <button
              type="button"
              className="toast-close"
              aria-label="Dismiss alert"
              onClick={() => onDismiss(toast.toastId)}
            >
              ×
            </button>
          </div>
          <div className="toast-body">{toast.body}</div>
          {toast.sessionId !== undefined && (
            <a
              className="toast-link"
              href={buildSessionHash(toast.sessionId)}
            >
              Open session
            </a>
          )}
        </div>
      ))}
    </div>
  );
}
