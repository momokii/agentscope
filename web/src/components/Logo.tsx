/**
 * Brand mark (Wave 5+ polish): terminal prompt chevron + block cursor on
 * the near-black panel token, accent stroke. Mirrors web/public/logo.svg
 * (the tab favicon) — keep the two in sync; colors trace web/DESIGN.md
 * section 2 tokens.
 */

export default function Logo({ size = 16 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 64 64"
      role="img"
      aria-label="opencode mission control"
    >
      <rect
        x="3"
        y="3"
        width="58"
        height="58"
        rx="13"
        fill="var(--ink-950)"
        stroke="var(--ink-600)"
        strokeWidth="3"
      />
      <path
        d="M21 23 L33 32 L21 41"
        fill="none"
        stroke="var(--accent)"
        strokeWidth="5.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <rect x="37" y="37" width="9" height="5.5" rx="1.5" fill="var(--accent)" />
    </svg>
  );
}
