import { ChevronLeft, ChevronRight } from "lucide-react";

import { PAGE_SIZE } from "../lib/fleetSort";

export interface PaginationProps {
  page: number;
  totalPages: number;
  total: number;
  onPageChange: (page: number) => void;
  pageSize?: number;
}

export default function Pagination({
  page,
  totalPages,
  total,
  onPageChange,
  pageSize = PAGE_SIZE,
}: PaginationProps) {
  const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const end = Math.min(page * pageSize, total);
  return (
    <nav className="pagination" aria-label="Session pagination">
      <span className="pagination-total mono">
        {start}–{end} of {total}
      </span>
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
