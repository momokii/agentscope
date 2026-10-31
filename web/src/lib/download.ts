/**
 * Client-side JSON download (Wave 6 follow-ups): fleet snapshots and
 * session details export without any new endpoint — the data is already
 * in the browser. Blob URL + anchor click; the object URL is revoked
 * after the click so repeated exports don't leak memory.
 */

export function downloadJson(filename: string, data: unknown): void {
  const text = JSON.stringify(data, null, 2);
  const blob = new Blob([text], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  try {
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
  } finally {
    // Revoke on the next tick: revoking synchronously can cancel the
    // download in some browsers before it starts.
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
}

/** Timestamped export filename, e.g. `fleet-20261028-093015.json`. */
export function exportFilename(prefix: string, at = new Date()): string {
  const pad = (n: number): string => String(n).padStart(2, "0");
  const stamp = `${at.getFullYear()}${pad(at.getMonth() + 1)}${pad(at.getDate())}-${pad(at.getHours())}${pad(at.getMinutes())}${pad(at.getSeconds())}`;
  const safe = prefix
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
  return `${safe === "" ? "export" : safe}-${stamp}.json`;
}
