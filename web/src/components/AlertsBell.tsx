/**
 * AlertsBell (Wave 6): the global alert gate. A bell in the app header with
 * a (?)–style panel: Notification opt-in (permission is requested only when
 * the operator flips the switch), a Test button proving the path, and the
 * daily budget input. Everything persists in localStorage; nothing leaves
 * the browser (no new network calls — alerts derive from the SSE snapshot
 * the App already owns).
 */

import { useState } from "react";
import { Bell, BellRing } from "lucide-react";

export interface AlertsConfig {
  enabled: boolean;
  budget: number | null;
}

export interface AlertsBellProps {
  config: AlertsConfig;
  onChange: (next: AlertsConfig) => void;
  supported: boolean;
  permission: NotificationPermission | "unsupported";
  onEnable: () => void;
  onTest: () => void;
  unread: number;
}

/** localStorage keys (operator-owned, never leaves the browser). */
export const ALERTS_ENABLED_KEY = "ocd.alerts.enabled";
export const ALERTS_BUDGET_KEY = "ocd.alerts.budget";

export function loadAlertsConfig(): AlertsConfig {
  let enabled = false;
  let budget: number | null = null;
  try {
    enabled = window.localStorage.getItem(ALERTS_ENABLED_KEY) === "1";
    const raw = window.localStorage.getItem(ALERTS_BUDGET_KEY);
    if (raw !== null && raw !== "") {
      const parsed = Number(raw);
      if (Number.isFinite(parsed) && parsed > 0) budget = parsed;
    }
  } catch {
    // Private mode etc: alerts simply don't persist across reloads.
  }
  return { enabled, budget };
}

export function saveAlertsConfig(config: AlertsConfig): void {
  try {
    window.localStorage.setItem(ALERTS_ENABLED_KEY, config.enabled ? "1" : "0");
    window.localStorage.setItem(
      ALERTS_BUDGET_KEY,
      config.budget === null ? "" : String(config.budget),
    );
  } catch {
    // Ignore persistence failures; the session still works in-memory.
  }
}

export function notificationSupport(): {
  supported: boolean;
  permission: NotificationPermission | "unsupported";
} {
  if (typeof Notification === "undefined") {
    return { supported: false, permission: "unsupported" };
  }
  return { supported: true, permission: Notification.permission };
}

export default function AlertsBell({
  config,
  onChange,
  supported,
  permission,
  onEnable,
  onTest,
  unread,
}: AlertsBellProps) {
  const [open, setOpen] = useState(false);
  return (
    <div className="alerts-bell">
      <button
        type="button"
        className={open ? "live-toggle live-toggle-on" : "live-toggle"}
        aria-expanded={open}
        aria-label={
          open ? "Hide alert settings" : "Alert settings (notifications, budget)"
        }
        title="Alert settings: notifications for waiting sessions, long runs, and the daily budget"
        onClick={() => setOpen((v) => !v)}
      >
        {config.enabled ? (
          <BellRing aria-hidden="true" size={12} />
        ) : (
          <Bell aria-hidden="true" size={12} />
        )}
        <span>Alerts</span>
        {unread > 0 && (
          <span className="alert-badge mono" aria-label={`${unread} unread alerts`}>
            {unread > 9 ? "9+" : unread}
          </span>
        )}
      </button>
      {open && (
        <div className="alerts-panel" role="dialog" aria-label="Alert settings">
          <label className="alerts-row">
            <input
              type="checkbox"
              checked={config.enabled}
              onChange={(event) =>
                onChange({ ...config, enabled: event.target.checked })
              }
            />
            <span>Notify me (waiting sessions, long runs, budget)</span>
          </label>
          {!supported && (
            <p className="alerts-note">
              Browser notifications unavailable here (page is not a secure
              context) — alerts still appear as in-app toasts.
            </p>
          )}
          {supported && permission === "denied" && (
            <p className="alerts-note">
              Notifications are blocked for this site — allow them in the
              browser site settings, or rely on in-app toasts.
            </p>
          )}
          {supported && permission === "default" && (
            <button type="button" className="filter-chip" onClick={onEnable}>
              <span>Enable browser notifications</span>
            </button>
          )}
          <button type="button" className="filter-chip" onClick={onTest}>
            <span>Send test alert</span>
          </button>
          <label className="alerts-row">
            <span>Daily budget ($)</span>
            <input
              type="number"
              min="0"
              step="0.01"
              inputMode="decimal"
              placeholder="off"
              defaultValue={config.budget ?? ""}
              key={config.budget ?? "empty"}
              onChange={(event) => {
                const raw = event.target.value;
                if (raw === "") {
                  onChange({ ...config, budget: null });
                  return;
                }
                const parsed = Number(raw);
                onChange({
                  ...config,
                  budget: Number.isFinite(parsed) && parsed > 0 ? parsed : null,
                });
              }}
              aria-label="Daily budget in USD"
            />
          </label>
          <p className="alerts-note">
            Budget compares against today's message spend (both harnesses).
            Alerts fire once per session spell / run / day.
          </p>
        </div>
      )}
    </div>
  );
}
