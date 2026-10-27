import { ChevronLeft, ChevronRight } from "lucide-react";

import { PAGE_SIZE } from "../lib/fleetSort";

/** Operator-selectable page sizes (user-facing list, shared default). */
export const PAGE_SIZE_OPTIONS: readonly number[] = [5, 10, 20, 25, 50];

export interface PaginationProps {
  page: number;
  totalPages: number;
  total: number;
  onPageChange: (page: number) => void;
  pageSize?: number;
  /** When set, renders the per-page select (resets to page 1 on change). */
  onPageSizeChange?: (size: number) => void;
}

export default function Pagination({
  page,
  totalPages,
  total,
  onPageChange,
  pageSize = PAGE_SIZE,
  onPageSizeChange,
}: PaginationProps) {
  const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const end = Math.min(page * pageSize, total);
  return (
    <nav className="pagination" aria-label="Session pagination">
      <span className="pagination-total mono">
        {start}–{end} of {total}
      </span>
      {onPageSizeChange !== undefined && (
        <label className="pagination-size mono">
          <span>Per page</span>
          <select
            value={pageSize}
            onChange={(event) => onPageSizeChange(Number(event.target.value))}
            aria-label="Rows per page"
          >
            {PAGE_SIZE_OPTIONS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
      )}
      <button
        type="button"
        className="pagination-button"
        aria-label="Previous page"
        disabled={page <= 1}
        onClick={() => onPageChange(page - 1)}
      >
        <ChevronLeft aria-hidden="true" size={12} />
        <span>Prev</span>
      </button>
      <span className="pagination-status mono" aria-live="polite">
        Page {page} of {totalPages}
      </span>
      <button
        type="button"
        className="pagination-button"
        aria-label="Next page"
        disabled={page >= totalPages}
        onClick={() => onPageChange(page + 1)}
      >
        <span>Next</span>
        <ChevronRight aria-hidden="true" size={12} />
      </button>
    </nav>
  );
}
