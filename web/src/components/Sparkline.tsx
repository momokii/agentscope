/**
 * Inline 72x24 fleet sparkline (DESIGN section 8): SVG polyline, no axes,
 * no animation (isAnimationActive equivalent — static markup, 1Hz-capped
 * upstream by the SSE client). Empty data renders a flat placeholder at
 * mid-height, never crashes.
 */

const WIDTH = 72;
const HEIGHT = 24;

export function sparklinePoints(data: readonly number[]): string {
  if (data.length === 0) return "";
  if (data.length === 1) {
    const y = HEIGHT / 2;
    return `0,${y} ${WIDTH},${y}`;
  }
  const min = Math.min(...data);
  const max = Math.max(...data);
  const span = max - min;
  return data
    .map((value, i) => {
      const x = (i / (data.length - 1)) * WIDTH;
      const y = span === 0 ? HEIGHT / 2 : HEIGHT - 2 - ((value - min) / span) * (HEIGHT - 4);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

export default function Sparkline({ data }: { data: readonly number[] }) {
  if (data.length === 0) {
    return (
      <svg
        className="spark spark-flat"
        width={WIDTH}
        height={HEIGHT}
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        aria-hidden="true"
      >
        <line x1="0" y1={HEIGHT / 2} x2={WIDTH} y2={HEIGHT / 2} />
      </svg>
    );
  }
  return (
    <svg
      className="spark"
      width={WIDTH}
      height={HEIGHT}
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      aria-hidden="true"
    >
      <polyline points={sparklinePoints(data)} />
    </svg>
  );
}
