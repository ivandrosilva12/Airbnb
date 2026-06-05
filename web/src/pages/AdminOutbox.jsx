import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// PAGE_SIZE matches the backend default (50) and stays well below the 200
// server-side cap. Operators almost never need to page deeper than the first
// screen — a healthy DLQ has zero rows.
const PAGE_SIZE = 50;

// fmtRelative returns a short "5m ago" / "2h ago" / "3d ago" string for
// quickly scanning when a record was dead-lettered. Falls back to the full
// locale timestamp for anything older than a week so the operator does not
// have to do mental arithmetic for stale records.
function fmtRelative(iso) {
  if (!iso) return '';
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return iso;
  const diff = Math.max(0, Date.now() - then);
  const min = Math.floor(diff / 60000);
  if (min < 1) return 'just now';
  if (min < 60) return `${min}m ago`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  if (d < 7) return `${d}d ago`;
  return new Date(iso).toLocaleString();
}

// truncate cuts a long error string to a scannable preview. The full text is
// rendered on click via the expandable cell so the operator never loses data.
function truncate(s, n = 80) {
  if (!s) return '';
  if (s.length <= n) return s;
  return s.slice(0, n) + '…';
}

// AdminOutbox is the operator-only view of the outbox dead-letter queue
// shipped by S100. It surfaces two things the existing /admin console did
// not: a live pending-count metric (so a stuck relay shows up before the
// DLQ fills) and a per-row Retry button (S142) that nudges a stuck record
// back into the recovery cycle. The RequireAdmin gate is enforced at the
// route level in App.jsx — we don't double-check here.
export default function AdminOutbox() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [pending, setPending] = useState(0);
  const [offset, setOffset] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  // expanded tracks which row's last-error cell is showing the full text.
  // Keyed by record id so paging away forgets the expansion (deliberate —
  // an expanded row only makes sense on the page it lives on).
  const [expanded, setExpanded] = useState({});
  // retrying is a Set of row ids currently in-flight (S142). A Set keeps
  // per-row isolation cleanly: only the clicked row's button reflects the
  // loading state, others stay idle even during a burst of clicks. The
  // request is idempotent on the server, but masking the in-flight button
  // prevents accidental double-submits.
  const [retrying, setRetrying] = useState(new Set());
  // retrySuccess is a transient, single-line confirmation shown after a
  // successful row removal. Cleared on the next retry attempt so it never
  // collides with a fresh error.
  const [retrySuccess, setRetrySuccess] = useState(false);

  const load = useCallback(async (nextOffset = 0) => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.listOutboxDLQ({ limit: PAGE_SIZE, offset: nextOffset });
      setItems(res.items || []);
      setPending(res.pending || 0);
      setOffset(nextOffset);
      setExpanded({});
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(0); }, [load]);

  // retry (S142) optimistically removes the row on 2xx and restores it on
  // failure. We snapshot index+row before the request so a failure can put
  // it back in the same position rather than appending it to the tail.
  async function retry(id) {
    setRetrying((prev) => {
      const next = new Set(prev);
      next.add(id);
      return next;
    });
    setError(null);
    setRetrySuccess(false);
    // Snapshot the row + its index for rollback on failure.
    let removed = null;
    let removedIndex = -1;
    setItems((prev) => {
      const idx = prev.findIndex((r) => r.id === id);
      if (idx >= 0) {
        removed = prev[idx];
        removedIndex = idx;
        const next = prev.slice();
        next.splice(idx, 1);
        return next;
      }
      return prev;
    });
    try {
      await api.requeueOutbox(id);
      setRetrySuccess(true);
    } catch {
      // Restore the row in its original slot so the operator can try again.
      if (removed) {
        setItems((prev) => {
          const next = prev.slice();
          const insertAt = Math.min(removedIndex, next.length);
          next.splice(insertAt, 0, removed);
          return next;
        });
      }
      setError(t('admin.dlq.retryError'));
    } finally {
      setRetrying((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }

  function toggleExpanded(id) {
    setExpanded((m) => ({ ...m, [id]: !m[id] }));
  }

  // hasMore is a best-effort flag: if the page is full we assume there could
  // be more behind it. The DLQ endpoint does not return a total count, so we
  // cannot show "Page X of Y" — that's fine; the operator paginates until the
  // page comes back short.
  const hasMore = items.length === PAGE_SIZE;
  const showingFrom = items.length === 0 ? 0 : offset + 1;
  const showingTo = offset + items.length;

  return (
    <div className="container">
      <h1>{t('admin.outbox.title')}</h1>
      <p className="muted">{t('admin.outbox.hint')}</p>

      <div className="metrics-grid">
        <div className="metric" aria-label={t('admin.outbox.pending')}>
          <div className="metric-value">{pending}</div>
          <div className="metric-label">{t('admin.outbox.pending')}</div>
        </div>
        <div className="metric" aria-label={t('admin.outbox.deadLettered')}>
          <div className="metric-value">{items.length}{hasMore ? '+' : ''}</div>
          <div className="metric-label">{t('admin.outbox.deadLettered')}</div>
        </div>
      </div>

      {retrySuccess && (
        <p className="success" role="status">{t('admin.dlq.retrySuccess')}</p>
      )}
      {error && <p className="error" role="alert">{error}</p>}

      {loading ? (
        <p role="status">{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.outbox.empty')}</p>
      ) : (
        <>
          <p className="muted">
            {t('admin.outbox.showingRange', { from: showingFrom, to: showingTo })}
          </p>
          <table className="admin-table">
            <thead>
              <tr>
                <th>{t('admin.outbox.colId')}</th>
                <th>{t('admin.outbox.colEvent')}</th>
                <th>{t('admin.outbox.colAttempts')}</th>
                <th>{t('admin.outbox.colWhen')}</th>
                <th>{t('admin.outbox.colError')}</th>
                <th>{t('admin.outbox.colActions')}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((r) => {
                const isExpanded = !!expanded[r.id];
                const isRetrying = retrying.has(r.id);
                // Short ID: first 8 chars of the UUID is enough to disambiguate
                // a page of ~50 rows. Full id stays in the title attribute for
                // copy-paste.
                const shortId = (r.id || '').slice(0, 8);
                return (
                  <tr key={r.id}>
                    <td><code className="muted-text" title={r.id}>{shortId}</code></td>
                    <td>{r.eventName}</td>
                    <td>{r.attempts}</td>
                    <td title={r.deadLetteredAt}>{fmtRelative(r.deadLetteredAt)}</td>
                    <td
                      className="dlq-error"
                      onClick={() => r.lastError && r.lastError.length > 80 && toggleExpanded(r.id)}
                      style={r.lastError && r.lastError.length > 80 ? { cursor: 'pointer' } : undefined}
                      title={r.lastError && r.lastError.length > 80 ? t('admin.outbox.clickToExpand') : ''}
                    >
                      {isExpanded ? r.lastError : truncate(r.lastError)}
                    </td>
                    <td className="admin-actions">
                      <button
                        type="button"
                        className="btn btn-primary btn-sm"
                        onClick={() => retry(r.id)}
                        disabled={isRetrying}
                        aria-busy={isRetrying || undefined}
                        aria-label={`${t('admin.dlq.retry')}: ${shortId}`}
                      >
                        {isRetrying ? t('admin.dlq.retrying') : t('admin.dlq.retry')}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          <div className="admin-actions">
            <button
              type="button"
              className="btn btn-ghost"
              onClick={() => load(Math.max(0, offset - PAGE_SIZE))}
              disabled={offset === 0 || loading}
            >
              {t('admin.outbox.prev')}
            </button>
            <button
              type="button"
              className="btn btn-ghost"
              onClick={() => load(offset + PAGE_SIZE)}
              disabled={!hasMore || loading}
            >
              {t('admin.outbox.next')}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
