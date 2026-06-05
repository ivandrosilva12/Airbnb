import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// Admin is the platform moderation console: the KYC identity-verification
// review queue and the listing-report queue.
export default function Admin() {
  const { t } = useT();
  return (
    <div className="container">
      <h1>{t('admin.title')}</h1>
      {/* Sub-nav (S103): only sibling admin pages mounted at their own routes.
          Everything below this header is rendered inline on /admin itself. */}
      <nav className="admin-subnav" aria-label={t('admin.subnav')}>
        <Link to="/admin/outbox">{t('admin.outbox.title')}</Link>
      </nav>
      <ActiveAlertsPanel />
      <VerificationQueue />
      <ReportQueue />
      <DisputeQueue />
      <CouponsPanel />
      <SilencesPanel />
      <UsersPanel />
      <TaxRemittancePanel />
      <FraudPanel />
      <AuditLogPanel />
    </div>
  );
}

// FraudPanel (S73) — read-only review queue over the fraud assessments
// produced by S68 (postgres-backed since S72). Drives the admin "show me
// high-risk bookings" workflow: filter by level, scan the page, click
// through to the booking detail. The assessment itself is forensic
// (the booking has already landed); the operator decides what to do
// (manual review, account suspension, refund).
function FraudPanel() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  // Default to ?level=high — the queue is for things worth a human's
  // attention, not the routine low-risk noise. Admins can broaden the
  // floor with the picker.
  const [level, setLevel] = useState('high');

  async function load(nextLevel = level) {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListFraudAssessments({ level: nextLevel, limit: 50 });
      setItems(res.items || []);
      setTotal(res.total || 0);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { load(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  function onLevelChange(e) {
    const v = e.target.value;
    setLevel(v);
    load(v);
  }

  return (
    <section className="admin-panel" aria-label={t('admin.fraud.title')}>
      <h2>{t('admin.fraud.title')}</h2>
      <p className="muted">{t('admin.fraud.hint')}</p>
      <div className="admin-filters">
        <label>
          {t('admin.fraud.levelLabel')}
          <select value={level} onChange={onLevelChange} aria-label={t('admin.fraud.levelLabel')}>
            <option value="">{t('admin.fraud.anyLevel')}</option>
            <option value="medium">{t('admin.fraud.minMedium')}</option>
            <option value="high">{t('admin.fraud.minHigh')}</option>
          </select>
        </label>
      </div>
      {error && <p className="error" role="alert">{error}</p>}
      {loading ? (
        <p role="status">{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.fraud.empty')}</p>
      ) : (
        <>
          <p className="muted">{t('admin.fraud.total', { n: total })}</p>
          <table className="admin-table">
            <thead>
              <tr>
                <th>{t('admin.fraud.colScore')}</th>
                <th>{t('admin.fraud.colLevel')}</th>
                <th>{t('admin.fraud.colSignals')}</th>
                <th>{t('admin.fraud.colBooking')}</th>
                <th>{t('admin.fraud.colWhen')}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.id}>
                  <td><strong>{a.score}</strong></td>
                  <td>
                    <span className={`badge badge-fraud-${a.level}`}>
                      {t(`admin.fraud.level.${a.level}`)}
                    </span>
                  </td>
                  <td>
                    {/* Top 3 signals, comma-separated. The full list lives
                        in the future per-booking drill-down; here we show
                        the headline so the operator can triage at a glance. */}
                    {(a.signals || []).slice(0, 3).map((s) => (
                      <span key={s.name} className="badge badge-signal" title={s.note}>
                        {t(`admin.fraud.signal.${s.name}`)} (+{s.impact})
                      </span>
                    ))}
                  </td>
                  <td><code className="muted-text">{a.bookingId}</code></td>
                  <td>{new Date(a.createdAt).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  );
}

// TaxRemittancePanel (S66) — calendar-month tax remittance breakdown that
// closes the loop on the S62 backend. The operator picks a year + month, hits
// Generate, and gets the per-(country, city, currency) bucket with line
// totals + booking counts. A CSV export button serialises whatever is on
// screen so it can be fed straight into a regulator template.
function TaxRemittancePanel() {
  const { t } = useT();
  // Default to the previous calendar month — remittance is always retrospective
  // (you can't report a month that hasn't closed). Hard-coded fallback because
  // Date.now() isn't deterministic; the user can pick anything else.
  const now = new Date();
  const defaultMonth = now.getMonth() === 0 ? 12 : now.getMonth();
  const defaultYear = now.getMonth() === 0 ? now.getFullYear() - 1 : now.getFullYear();
  const [year, setYear] = useState(defaultYear);
  const [month, setMonth] = useState(defaultMonth);
  const [items, setItems] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  // S126 — the Export CSV button has its own loading + error state separate
  // from the report fetch above, so a failed CSV write doesn't blank the
  // table and a successful generate doesn't dismiss a previous export error.
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState(null);

  async function generate(e) {
    if (e) e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminTaxRemittance(year, Number(month));
      setItems(res.items || []);
      setLoaded(true);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }

  // Format a cents integer as "12.34" for human display + CSV export. The
  // currency is rendered separately so the locale doesn't shuffle it.
  function fmtAmount(cents) {
    return (cents / 100).toFixed(2);
  }

  // exportCsv builds a regulator-friendly CSV with one row per (jurisdiction,
  // rule) pair plus a totals row per jurisdiction. CRLF + quoted fields are
  // belt-and-braces for spreadsheet imports. S126 — runs as an async flow so
  // the button can advertise an "Exporting…" state and surface failures
  // (Blob/URL APIs can throw on locked-down browsers) inline rather than via
  // a noisy alert().
  async function exportCsv() {
    if (exporting) return;
    setExporting(true);
    setExportError(null);
    try {
      const rows = [
        ['period', 'country', 'city', 'currency', 'tax_rule', 'amount', 'booking_count'],
      ];
      for (const r of items) {
        for (const line of r.lines || []) {
          rows.push([
            r.period,
            r.country,
            r.city || '',
            r.currency,
            line.name,
            fmtAmount(line.amountCents),
            String(line.bookingCount),
          ]);
        }
        rows.push([
          r.period,
          r.country,
          r.city || '',
          r.currency,
          '__TOTAL__',
          fmtAmount(r.totalCents),
          String(r.bookingCount),
        ]);
      }
      const csv = rows
        .map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(','))
        .join('\r\n');
      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `airhost-tax-remittance-${year}-${String(month).padStart(2, '0')}.csv`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      setExportError(err.message || t('admin.tax.exportError'));
    } finally {
      setExporting(false);
    }
  }

  // Build a year list spanning the last 5 calendar years — remittance for
  // anything older is a historical curiosity at best.
  const years = [];
  for (let y = defaultYear; y >= defaultYear - 4; y--) years.push(y);

  return (
    <section className="admin-panel" aria-label={t('admin.tax.title')}>
      <h2>{t('admin.tax.title')}</h2>
      <p className="muted">{t('admin.tax.hint')}</p>
      <form
        className="admin-filters"
        onSubmit={generate}
        aria-label={t('admin.tax.filterLabel')}
      >
        <label>
          {t('admin.tax.year')}
          <select
            value={year}
            onChange={(e) => setYear(Number(e.target.value))}
            aria-label={t('admin.tax.year')}
          >
            {years.map((y) => (
              <option key={y} value={y}>{y}</option>
            ))}
          </select>
        </label>
        <label>
          {t('admin.tax.month')}
          <select
            value={month}
            onChange={(e) => setMonth(Number(e.target.value))}
            aria-label={t('admin.tax.month')}
          >
            {Array.from({ length: 12 }, (_, i) => i + 1).map((m) => (
              <option key={m} value={m}>{t(`admin.tax.months.${m}`)}</option>
            ))}
          </select>
        </label>
        <button type="submit" className="btn btn-primary" disabled={loading}>
          {loading ? t('common.loading') : t('admin.tax.generate')}
        </button>
        {/* S126 — Export CSV is always visible so the operator can see the
            affordance up front. It's disabled (with a tooltip) until a
            remittance period has been generated and has rows; once data is
            on screen it flips to an enabled idle state. While the CSV is
            being assembled and the download is triggered, it advertises a
            loading state via aria-busy + label swap so screen readers
            announce the transition. */}
        <button
          type="button"
          className="btn btn-ghost"
          onClick={exportCsv}
          disabled={!loaded || items.length === 0 || exporting}
          aria-busy={exporting ? 'true' : 'false'}
          title={
            !loaded || items.length === 0
              ? t('admin.tax.selectPeriodFirst')
              : undefined
          }
        >
          {exporting ? t('admin.tax.exporting') : t('admin.tax.exportCsv')}
        </button>
      </form>
      {error && <p className="error" role="alert">{error}</p>}
      {exportError && <p className="error" role="alert">{exportError}</p>}
      {!loaded ? (
        <p className="muted">{t('admin.tax.runHint')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.tax.empty')}</p>
      ) : (
        <ul className="admin-list">
          {items.map((r, idx) => (
            <li key={`${r.period}-${r.country}-${r.city}-${r.currency}-${idx}`} className="admin-item">
              <div className="admin-item-head">
                <strong>
                  {r.country}{r.city ? ` · ${r.city}` : ''} <span className="muted">({r.currency})</span>
                </strong>
                <span className="muted">{r.period}</span>
                <span className="muted">
                  {t('admin.tax.bookingCount', { n: r.bookingCount })}
                </span>
              </div>
              <table className="admin-table">
                <thead>
                  <tr>
                    <th>{t('admin.tax.colRule')}</th>
                    <th>{t('admin.tax.colAmount')}</th>
                    <th>{t('admin.tax.colBookings')}</th>
                  </tr>
                </thead>
                <tbody>
                  {(r.lines || []).map((line) => (
                    <tr key={line.name}>
                      <td>{line.name}</td>
                      <td>{fmtAmount(line.amountCents)} {r.currency}</td>
                      <td>{line.bookingCount}</td>
                    </tr>
                  ))}
                  <tr>
                    <td><strong>{t('admin.tax.total')}</strong></td>
                    <td><strong>{fmtAmount(r.totalCents)} {r.currency}</strong></td>
                    <td>{r.bookingCount}</td>
                  </tr>
                </tbody>
              </table>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// UsersPanel (S65) — admin browsing and moderation of platform accounts.
// Drives /admin/users with email substring + role + active-only filters;
// each row gets a Suspend or Unsuspend action wired to S61. Hard-fails the
// audit hook on the backend, so a successful action means the trail row
// actually landed.
function UsersPanel() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  // Filter state is local; the URL stays clean because admins rarely
  // bookmark a filtered users list. The Apply button (or Enter on the
  // input) triggers the fetch.
  const [filters, setFilters] = useState({ email: '', role: '', activeOnly: false });
  // S109 — client-side instant search across the currently-fetched page,
  // matching case-insensitively against name OR email substring. The
  // existing `filters.email` is server-side (used by the Apply button to
  // narrow the page); this is the "scan the page I just got" affordance
  // hosts asked for. Empty query → show all.
  const [search, setSearch] = useState('');

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListUsers({
        email: filters.email.trim(),
        role: filters.role,
        activeOnly: filters.activeOnly ? 'true' : '',
        limit: 50,
      });
      setItems(res.items || []);
      setTotal(res.total || 0);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { load(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  async function suspend(id, email) {
    if (!confirm(t('admin.users.confirmSuspend', { email }))) return;
    setError(null);
    try {
      await api.adminSuspendUser(id);
      await load();
    } catch (e) {
      setError(e.message);
    }
  }
  async function unsuspend(id) {
    setError(null);
    try {
      await api.adminUnsuspendUser(id);
      await load();
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <section className="admin-panel" aria-label={t('admin.users.title')}>
      <h2>{t('admin.users.title')}</h2>
      <form
        className="admin-filters"
        onSubmit={(e) => { e.preventDefault(); load(); }}
        aria-label={t('admin.users.filterLabel')}
      >
        <input
          type="search"
          placeholder={t('admin.users.emailPlaceholder')}
          value={filters.email}
          onChange={(e) => setFilters({ ...filters, email: e.target.value })}
          aria-label={t('admin.users.emailPlaceholder')}
        />
        <select
          value={filters.role}
          onChange={(e) => setFilters({ ...filters, role: e.target.value })}
          aria-label={t('admin.users.roleLabel')}
        >
          <option value="">{t('admin.users.anyRole')}</option>
          <option value="guest">{t('admin.users.roleGuest')}</option>
          <option value="host">{t('admin.users.roleHost')}</option>
          <option value="admin">{t('admin.users.roleAdmin')}</option>
        </select>
        <label className="checkbox-row">
          <input
            type="checkbox"
            checked={filters.activeOnly}
            onChange={(e) => setFilters({ ...filters, activeOnly: e.target.checked })}
          />
          <span>{t('admin.users.activeOnly')}</span>
        </label>
        <button type="submit" className="btn btn-primary">{t('admin.users.apply')}</button>
      </form>
      {/* S109 — client-side search box. Filters the already-fetched page
          by name OR email substring, case-insensitive. Independent from
          the server-side filter form above (which re-queries the API). */}
      <input
        type="search"
        className="admin-search"
        placeholder={t('admin.users.searchPlaceholder')}
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        aria-label={t('admin.users.searchPlaceholder')}
      />
      {error && <p className="error" role="alert">{error}</p>}
      {(() => {
        const q = search.trim().toLowerCase();
        const filtered = q === '' ? items : items.filter((u) => {
          const name = (u.name || '').toLowerCase();
          const email = (u.email || '').toLowerCase();
          return name.includes(q) || email.includes(q);
        });
        if (loading) return <p role="status">{t('common.loading')}</p>;
        if (filtered.length === 0) return <p>{t('admin.users.empty')}</p>;
        return (
        <>
          <p className="muted">{t('admin.users.total', { n: total })}</p>
          <table className="admin-table">
            <thead>
              <tr>
                <th>{t('admin.users.colEmail')}</th>
                <th>{t('admin.users.colName')}</th>
                <th>{t('admin.users.colRole')}</th>
                <th>{t('admin.users.colStatus')}</th>
                <th>{t('admin.users.colActions')}</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((u) => (
                <tr key={u.id}>
                  <td>{u.email}</td>
                  <td>{u.fullName}</td>
                  <td><span className={`badge badge-${u.role}`}>{u.role}</span></td>
                  <td>
                    <span className={`badge badge-${u.isActive ? 'active' : 'suspended'}`}>
                      {u.isActive ? t('admin.users.statusActive') : t('admin.users.statusSuspended')}
                    </span>
                  </td>
                  <td className="actions">
                    {u.isActive ? (
                      <button
                        type="button"
                        className="btn btn-ghost"
                        onClick={() => suspend(u.id, u.email)}
                        aria-label={`${t('admin.users.suspend')}: ${u.email}`}
                      >{t('admin.users.suspend')}</button>
                    ) : (
                      <button
                        type="button"
                        className="btn btn-ghost"
                        onClick={() => unsuspend(u.id)}
                        aria-label={`${t('admin.users.unsuspend')}: ${u.email}`}
                      >{t('admin.users.unsuspend')}</button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
        );
      })()}
    </section>
  );
}

// renderSlaChip surfaces the S122 three-state SLA indicator next to each open
// dispute in the admin queue. The backend (S20c, DisputeView) already exposes
// an `overdue` boolean computed against the case's `dueAt` deadline, so we
// trust that flag for the red state and only fall back to client-side math for
// the "within" vs. "approaching" split when the deadline has not yet elapsed.
// The chip's `title` shows the relative age of the case ("Opened Nh ago") so
// moderators can scan the queue without doing the arithmetic themselves.
function renderSlaChip(d, t) {
  const opened = d.openedAt ? new Date(d.openedAt) : null;
  const ageHours = opened ? Math.max(0, Math.floor((Date.now() - opened.getTime()) / 3600000)) : null;
  // Tooltip is intentionally a short, language-neutral hint ("Opened 31h ago")
  // — the visible chip text is the translated SLA bucket, which is what
  // matters for moderators scanning the queue at a glance.
  const tooltip = ageHours == null ? '' : `Opened ${ageHours}h ago`;
  // Backend `overdue` is the source of truth for the red state (it knows the
  // 7-day deadline). Client only differentiates within (<24h) vs approaching
  // (24-48h) when the deadline hasn't elapsed yet.
  if (d.overdue) {
    return (
      <span className="badge badge-overdue" title={tooltip}>
        {t('admin.disputes.slaOverdue')}
      </span>
    );
  }
  if (ageHours != null && ageHours >= 24) {
    return (
      <span className="badge badge-dispute-open" title={tooltip}>
        {t('admin.disputes.slaApproaching')}
      </span>
    );
  }
  return (
    <span className="badge badge-dispute-resolved" title={tooltip}>
      {t('admin.disputes.slaWithin')}
    </span>
  );
}

// DisputeQueue lists open Resolution Center cases and lets an admin record a
// public decision (resolve / reject) per case.
function DisputeQueue() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  // Per-dispute draft state: { resolution, refund (string), damage (string) }.
  // refund/damage stay strings so the input can be cleared without flicking
  // to NaN; the numeric conversion happens at submit time.
  const [drafts, setDrafts] = useState({});

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListOpenDisputes();
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { load(); }, []);

  function updateDraft(disputeId, patch) {
    setDrafts((d) => ({ ...d, [disputeId]: { ...(d[disputeId] || {}), ...patch } }));
  }

  // toCents turns a "12.34"-style euro/dollar input into integer cents. Empty
  // / non-numeric inputs collapse to 0 (interpreted as "no monetary effect").
  function toCents(raw) {
    if (!raw) return 0;
    const n = Number.parseFloat(raw);
    if (!Number.isFinite(n) || n <= 0) return 0;
    return Math.round(n * 100);
  }

  async function decide(disputeId, kind, fn) {
    const draft = drafts[disputeId] || {};
    const resolution = (draft.resolution || '').trim();
    if (!resolution) {
      setError(t('admin.dispute.needResolution'));
      return;
    }
    const body = {
      resolution,
      refundAmountCents: kind === 'resolve' ? toCents(draft.refund) : 0,
      damageAmountCents: kind === 'resolve' ? toCents(draft.damage) : 0,
    };
    setError(null);
    try {
      await fn(disputeId, body);
      setDrafts((d) => ({ ...d, [disputeId]: {} }));
      await load();
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <section className="admin-panel">
      <h2>{t('admin.dispute.title')}</h2>
      <p className="muted">{t('admin.dispute.hint')}</p>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.dispute.empty')}</p>
      ) : (
        <ul className="admin-list">
          {items.map((d) => (
            <li key={d.id} className="admin-item">
              <div className="admin-item-head">
                <strong>{t(`dispute.kind.${d.kind}`)}</strong>
                <span className={`badge badge-dispute-${d.status}`}>{t(`dispute.status.${d.status}`)}</span>
                {renderSlaChip(d, t)}
                {d.requestedAmountCents > 0 && (
                  <span className="muted">
                    {(d.requestedAmountCents / 100).toFixed(2)} {d.currency}
                  </span>
                )}
              </div>
              <div className="admin-item-body">
                <p>{d.reason}</p>
                {d.hostResponse && <p className="muted-text">{t('admin.dispute.hostResponse')}: {d.hostResponse}</p>}
                {d.evidence && d.evidence.length > 0 && (
                  <ul className="evidence-list">
                    {d.evidence.map((e) => (
                      <li key={e.id}>
                        {e.note}
                        {e.url && <> — <a href={e.url} target="_blank" rel="noopener noreferrer">{t('admin.dispute.evidenceLink')}</a></>}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
              <textarea
                placeholder={t('admin.dispute.decisionPlaceholder')}
                value={(drafts[d.id] && drafts[d.id].resolution) || ''}
                onChange={(e) => updateDraft(d.id, { resolution: e.target.value })}
              />
              <div className="admin-money-row">
                <label>
                  {t('admin.dispute.refundAmount')}
                  <input
                    type="number" min="0" step="0.01"
                    placeholder="0.00"
                    value={(drafts[d.id] && drafts[d.id].refund) || ''}
                    onChange={(e) => updateDraft(d.id, { refund: e.target.value })}
                  />
                </label>
                <label>
                  {t('admin.dispute.damageAmount')}
                  <input
                    type="number" min="0" step="0.01"
                    placeholder="0.00"
                    value={(drafts[d.id] && drafts[d.id].damage) || ''}
                    onChange={(e) => updateDraft(d.id, { damage: e.target.value })}
                  />
                </label>
                <span className="muted">{d.currency || ''}</span>
              </div>
              <p className="muted-text">{t('admin.dispute.moneyHint')}</p>
              <div className="admin-actions">
                <button className="btn btn-primary btn-sm" onClick={() => decide(d.id, 'resolve', api.adminResolveDispute)}>{t('admin.dispute.resolve')}</button>
                <button className="btn btn-ghost btn-sm" onClick={() => decide(d.id, 'reject', api.adminRejectDispute)}>{t('admin.dispute.reject')}</button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// CouponsPanel lets an admin mint promo codes (percentage or fixed amount) and
// deactivate existing ones.
function CouponsPanel() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [form, setForm] = useState({
    code: '', kind: 'percentage', percent: 10, amountCents: 1000,
    currency: 'EUR', minNights: 0, maxRedemptions: 0, expiresAt: '',
  });
  const [saving, setSaving] = useState(false);

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListCoupons();
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function create(e) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    try {
      const body = {
        code: form.code.trim(),
        kind: form.kind,
        minNights: Number(form.minNights) || 0,
        maxRedemptions: Number(form.maxRedemptions) || 0,
        expiresAt: form.expiresAt ? new Date(form.expiresAt).toISOString() : '',
      };
      if (form.kind === 'percentage') {
        body.percent = Number(form.percent) / 100; // UI is whole percent
      } else {
        body.amountCents = Math.round(Number(form.amountCents));
        body.currency = form.currency.trim().toUpperCase();
      }
      await api.adminCreateCoupon(body);
      setForm({ ...form, code: '' });
      load();
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  async function deactivate(id) {
    setError(null);
    try {
      await api.adminDeactivateCoupon(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  function describe(c) {
    if (c.kind === 'percentage') return `${Math.round(c.percent * 100)}%`;
    return c.amount ? c.amount.display : '';
  }

  return (
    <section>
      <h2>{t('admin.couponsTitle')}</h2>
      <p className="muted">{t('admin.couponsHelp')}</p>
      {error && <p className="error">{error}</p>}

      <form className="silence-form" onSubmit={create}>
        <label>
          {t('admin.couponCode')}
          <input type="text" value={form.code} placeholder="SUMMER25" onChange={(e) => setForm({ ...form, code: e.target.value })} required />
        </label>
        <label>
          {t('admin.couponKind')}
          <select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
            <option value="percentage">{t('admin.couponPercentage')}</option>
            <option value="fixed">{t('admin.couponFixed')}</option>
          </select>
        </label>
        {form.kind === 'percentage' ? (
          <label>
            {t('admin.couponPercent')}
            <input type="number" min="1" max="100" value={form.percent} onChange={(e) => setForm({ ...form, percent: e.target.value })} />
          </label>
        ) : (
          <>
            <label>
              {t('admin.couponAmountCents')}
              <input type="number" min="1" value={form.amountCents} onChange={(e) => setForm({ ...form, amountCents: e.target.value })} />
            </label>
            <label>
              {t('admin.couponCurrency')}
              <input type="text" value={form.currency} onChange={(e) => setForm({ ...form, currency: e.target.value })} />
            </label>
          </>
        )}
        <label>
          {t('admin.couponMinNights')}
          <input type="number" min="0" value={form.minNights} onChange={(e) => setForm({ ...form, minNights: e.target.value })} />
        </label>
        <label>
          {t('admin.couponMaxRedemptions')}
          <input type="number" min="0" value={form.maxRedemptions} onChange={(e) => setForm({ ...form, maxRedemptions: e.target.value })} />
        </label>
        <label>
          {t('admin.couponExpires')}
          <input type="date" value={form.expiresAt} onChange={(e) => setForm({ ...form, expiresAt: e.target.value })} />
        </label>
        <button className="btn btn-primary" type="submit" disabled={saving}>{t('admin.couponCreate')}</button>
      </form>

      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.couponsEmpty')}</p>
      ) : (
        <table className="admin-table">
          <thead>
            <tr>
              <th>{t('admin.couponCode')}</th>
              <th>{t('admin.couponValue')}</th>
              <th>{t('admin.couponMinNights')}</th>
              <th>{t('admin.couponUses')}</th>
              <th>{t('admin.couponExpires')}</th>
              <th>{t('admin.silenceStatus')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((c) => (
              <tr key={c.id} className={c.active ? '' : 'row-muted'}>
                <td><code>{c.code}</code></td>
                <td>{describe(c)}</td>
                <td>{c.minNights || '—'}</td>
                <td>{c.redemptions}{c.maxRedemptions > 0 ? ` / ${c.maxRedemptions}` : ''}</td>
                <td>{c.expiresAt ? new Date(c.expiresAt).toLocaleDateString() : '—'}</td>
                <td>
                  <span className={`badge ${c.active ? 'badge-ok' : 'badge-firing'}`}>
                    {c.active ? t('admin.couponActive') : t('admin.couponInactive')}
                  </span>
                </td>
                <td className="admin-actions">
                  {c.active && (
                    <button className="btn btn-ghost" onClick={() => deactivate(c.id)}>{t('admin.couponDeactivate')}</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

// ActiveAlertsPanel reflects the live alert state pushed by Alertmanager —
// firing alerts and recently resolved ones — so ops can see status in-app, not
// just via email/Slack. It polls so a resolve shows up without a manual reload.
function ActiveAlertsPanel() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState(null);

  async function load() {
    try {
      const res = await api.adminListAlerts();
      setItems(res.items || []);
      setError(null);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoaded(true);
    }
  }

  useEffect(() => {
    load();
    const id = setInterval(load, 15000);
    return () => clearInterval(id);
  }, []);

  const firing = items.filter((a) => a.status === 'firing').length;

  return (
    <section>
      <h2>
        {t('admin.alertsTitle')}{' '}
        {loaded && (
          <span className={`badge ${firing ? 'badge-firing' : 'badge-ok'}`}>
            {firing ? t('admin.alertsFiringCount', { n: firing }) : t('admin.alertsAllClear')}
          </span>
        )}
      </h2>
      {error && <p className="error">{error}</p>}
      {!loaded ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.alertsEmpty')}</p>
      ) : (
        <table className="admin-table">
          <thead>
            <tr>
              <th>{t('admin.alertName')}</th>
              <th>{t('admin.alertSeverity')}</th>
              <th>{t('admin.silenceStatus')}</th>
              <th>{t('admin.alertSummary')}</th>
              <th>{t('admin.alertSince')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((a) => (
              <tr key={a.fingerprint} className={a.status === 'resolved' ? 'row-muted' : ''}>
                <td>{a.alertName}</td>
                <td>{a.severity}</td>
                <td>
                  <span className={`badge ${a.status === 'firing' ? 'badge-firing' : 'badge-ok'}`}>
                    {t(`admin.alertState.${a.status}`)}
                  </span>
                </td>
                <td>{a.summary}</td>
                <td>{a.startsAt ? new Date(a.startsAt).toLocaleString() : ''}</td>
                <td>
                  {a.runbookUrl && (
                    <a href={a.runbookUrl} target="_blank" rel="noreferrer">
                      {t('admin.alertRunbook')}
                    </a>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

// Known alert targets a silence can match. The first group matches a single
// alert by name; the last two mute everything at a severity.
const SILENCE_TARGETS = [
  { key: 'AirhostApiDown', matcher: { name: 'alertname', value: 'AirhostApiDown' } },
  { key: 'AirhostHighErrorRate', matcher: { name: 'alertname', value: 'AirhostHighErrorRate' } },
  { key: 'AirhostWebhookRejectionSpike', matcher: { name: 'alertname', value: 'AirhostWebhookRejectionSpike' } },
  { key: 'AirhostRateLimitingSustained', matcher: { name: 'alertname', value: 'AirhostRateLimitingSustained' } },
  { key: 'AirhostWebhookProcessingErrors', matcher: { name: 'alertname', value: 'AirhostWebhookProcessingErrors' } },
  { key: 'allCritical', matcher: { name: 'severity', value: 'critical' } },
  { key: 'allWarnings', matcher: { name: 'severity', value: 'warning' } },
];

function SilencesPanel() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [target, setTarget] = useState(SILENCE_TARGETS[0].key);
  const [minutes, setMinutes] = useState(60);
  const [comment, setComment] = useState('');
  const [saving, setSaving] = useState(false);

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListSilences();
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function create(e) {
    e.preventDefault();
    const chosen = SILENCE_TARGETS.find((x) => x.key === target);
    if (!chosen || !comment.trim()) {
      setError(t('admin.silenceNeedsComment'));
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await api.adminCreateSilence({
        matchers: [chosen.matcher],
        durationMinutes: Number(minutes) || 0,
        comment: comment.trim(),
      });
      setComment('');
      load();
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  }

  async function remove(id) {
    setError(null);
    try {
      await api.adminDeleteSilence(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  function describeMatchers(matchers) {
    return (matchers || [])
      .map((m) => `${m.name}${m.isRegex ? '=~' : '='}"${m.value}"`)
      .join(', ');
  }

  return (
    <section>
      <h2>{t('admin.silencesTitle')}</h2>
      <p className="muted">{t('admin.silencesHelp')}</p>
      {error && <p className="error">{error}</p>}

      <form className="silence-form" onSubmit={create}>
        <label>
          {t('admin.silenceTarget')}
          <select value={target} onChange={(e) => setTarget(e.target.value)}>
            {SILENCE_TARGETS.map((x) => (
              <option key={x.key} value={x.key}>
                {t(`admin.silenceTargets.${x.key}`)}
              </option>
            ))}
          </select>
        </label>
        <label>
          {t('admin.silenceDuration')}
          <input
            type="number"
            min="1"
            value={minutes}
            onChange={(e) => setMinutes(e.target.value)}
          />
        </label>
        <label>
          {t('admin.silenceComment')}
          <input
            type="text"
            value={comment}
            placeholder={t('admin.silenceCommentPlaceholder')}
            onChange={(e) => setComment(e.target.value)}
          />
        </label>
        <button className="btn btn-primary" type="submit" disabled={saving}>
          {t('admin.silenceCreate')}
        </button>
      </form>

      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.silencesEmpty')}</p>
      ) : (
        <table className="admin-table">
          <thead>
            <tr>
              <th>{t('admin.silenceMatchers')}</th>
              <th>{t('admin.silenceStatus')}</th>
              <th>{t('admin.silenceEnds')}</th>
              <th>{t('admin.silenceBy')}</th>
              <th>{t('admin.note')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((s) => (
              <tr key={s.id}>
                <td>{describeMatchers(s.matchers)}</td>
                <td>{t(`admin.silenceState.${s.status}`)}</td>
                <td>{new Date(s.endsAt).toLocaleString()}</td>
                <td>{s.createdBy}</td>
                <td>{s.comment}</td>
                <td className="admin-actions">
                  <button className="btn btn-ghost" onClick={() => remove(s.id)}>
                    {t('admin.silenceDelete')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function VerificationQueue() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListVerifications();
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function approve(id) {
    setError(null);
    try {
      await api.adminApproveVerification(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function reject(id) {
    const reason = window.prompt(t('admin.rejectPrompt'));
    if (!reason) return;
    setError(null);
    try {
      await api.adminRejectVerification(id, reason);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <section>
      <h2>{t('admin.verifTitle')}</h2>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.verifEmpty')}</p>
      ) : (
        <table className="admin-table">
          <thead>
            <tr>
              <th>{t('admin.legalName')}</th>
              <th>{t('admin.document')}</th>
              <th>{t('admin.submitted')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((v) => (
              <tr key={v.id}>
                <td>{v.legalName}</td>
                <td>{t(`verify.doc.${v.documentType}`)}</td>
                <td>{new Date(v.createdAt).toLocaleDateString()}</td>
                <td className="admin-actions">
                  <button className="btn btn-primary" onClick={() => approve(v.id)}>
                    {t('admin.approve')}
                  </button>
                  <button className="btn btn-ghost" onClick={() => reject(v.id)}>
                    {t('admin.reject')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function ReportQueue() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListReports();
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function act(fn, id, withNote) {
    setError(null);
    let note = '';
    if (withNote) {
      note = window.prompt(t('admin.resolvePrompt')) || '';
    }
    try {
      await fn(id, note);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function suspend(propertyId) {
    setError(null);
    try {
      await api.adminSuspendProperty(propertyId);
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <section>
      <h2>{t('admin.reportsTitle')}</h2>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.reportsEmpty')}</p>
      ) : (
        <table className="admin-table">
          <thead>
            <tr>
              <th>{t('admin.listing')}</th>
              <th>{t('admin.reason')}</th>
              <th>{t('admin.note')}</th>
              <th>{t('admin.submitted')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((r) => (
              <tr key={r.id}>
                <td>
                  {r.targetType === 'review' && <span className="badge">{t('admin.reportTargetReview')}</span>}{' '}
                  <Link to={`/properties/${r.propertyId}`}>{r.propertyTitle || r.propertyId}</Link>
                </td>
                <td>{t(`report.reason.${r.reason}`)}</td>
                <td>{r.note}</td>
                <td>{new Date(r.createdAt).toLocaleDateString()}</td>
                <td className="admin-actions">
                  <button className="btn btn-ghost" onClick={() => suspend(r.propertyId)}>
                    {t('admin.suspend')}
                  </button>
                  <button className="btn btn-primary" onClick={() => act(api.adminResolveReport, r.id, true)}>
                    {t('admin.resolve')}
                  </button>
                  <button className="btn btn-ghost" onClick={() => act(api.adminDismissReport, r.id, false)}>
                    {t('admin.dismiss')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

// AUDIT_ACTIONS (S130) — the closed enum the backend audit BC accepts on
// /admin/audit?action=…. Mirrors `audit.Action` from the Go domain (S45 + S54
// + S61 + S90 + S120 + S124). Kept as a local constant rather than fetched
// from the server because the set is small, slow-moving, and we already
// commit to it on the backend; the cost of round-tripping for a select-box
// option list isn't worth it. New action constants land here when the
// backend enum is extended.
const AUDIT_ACTIONS = [
  'property.suspend',
  'property.unsuspend',
  'property.suspended',
  'property.unsuspended',
  'identity.approve',
  'identity.reject',
  'report.resolve',
  'report.dismiss',
  'dispute.resolve',
  'dispute.reject',
  'coupon.deactivate',
  'tax_rule.create',
  'tax_rule.delete',
  'user.suspend',
  'user.unsuspend',
  'gdpr_erase',
  'cohost.invited',
];

// AUDIT_TARGET_TYPES (S130) — mirrors `audit.TargetType`. Same rationale as
// AUDIT_ACTIONS above: stable, server-side closed enum, surface it locally
// so the dropdown renders without a fetch.
const AUDIT_TARGET_TYPES = ['property', 'identity', 'report', 'dispute', 'coupon', 'tax_rule', 'user'];

// AuditLogPanel (S130) — read-only viewer over the S45 audit BC. Two filters
// (Action + TargetType) map directly onto the backend's AND-combined Filter,
// and a Refresh button re-fetches without changing the filter (handy when an
// admin acts in another tab and wants to see the trail row land here). Each
// row is a compact card with timestamp, action, actor short-id, target, and
// a Metadata toggle that pretty-prints the JSON-shaped context map — empty
// objects collapse so we don't show "{}" noise.
function AuditLogPanel() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [action, setAction] = useState('');
  const [targetType, setTargetType] = useState('');
  // Per-row metadata toggle — keyed by event id so opening one row doesn't
  // affect the others, and the state survives a re-fetch as long as the
  // row sticks around.
  const [expanded, setExpanded] = useState({});

  async function load(nextAction = action, nextTarget = targetType) {
    setLoading(true);
    setError(null);
    try {
      const res = await api.adminListAuditEvents({
        action: nextAction,
        targetType: nextTarget,
        limit: 50,
      });
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { load(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  function onActionChange(e) {
    const v = e.target.value;
    setAction(v);
    load(v, targetType);
  }
  function onTargetChange(e) {
    const v = e.target.value;
    setTargetType(v);
    load(action, v);
  }
  function toggle(id) {
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }));
  }

  // Short-form an actor / target UUID to the first 8 chars so a long row
  // stays scannable; the full id is in the title attribute for copy-paste.
  function shortId(id) {
    if (!id) return '';
    return String(id).slice(0, 8);
  }
  function hasMetadata(meta) {
    return meta && typeof meta === 'object' && Object.keys(meta).length > 0;
  }

  return (
    <section className="admin-panel" aria-label={t('admin.audit.title')} aria-busy={loading ? 'true' : 'false'}>
      <h2>{t('admin.audit.title')}</h2>
      <div className="admin-filters">
        <label>
          {t('admin.audit.filterAction')}
          <select value={action} onChange={onActionChange} aria-label={t('admin.audit.filterAction')}>
            <option value="">{t('admin.audit.filterAll')}</option>
            {AUDIT_ACTIONS.map((a) => (
              <option key={a} value={a}>{a}</option>
            ))}
          </select>
        </label>
        <label>
          {t('admin.audit.filterTargetType')}
          <select value={targetType} onChange={onTargetChange} aria-label={t('admin.audit.filterTargetType')}>
            <option value="">{t('admin.audit.filterAll')}</option>
            {AUDIT_TARGET_TYPES.map((tt) => (
              <option key={tt} value={tt}>{tt}</option>
            ))}
          </select>
        </label>
        <button
          type="button"
          className="btn btn-ghost"
          onClick={() => load()}
          disabled={loading}
        >
          {t('admin.audit.refresh')}
        </button>
      </div>
      {error && <p className="error" role="alert">{t('admin.audit.error')}: {error}</p>}
      {loading ? (
        <p role="status">{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p className="muted">{t('admin.audit.empty')}</p>
      ) : (
        <ul className="admin-list">
          {items.map((e) => (
            <li key={e.id} className="admin-item">
              <div className="admin-item-head">
                <span>{new Date(e.createdAt).toLocaleString()}</span>
                <strong>{e.action}</strong>
                <span className="muted" title={e.actorId}>
                  {t('admin.audit.colActor')}: <code className="muted-text">{shortId(e.actorId)}</code>
                </span>
                <span className="muted" title={e.targetId}>
                  {t('admin.audit.colTarget')}: {e.targetType} · <code className="muted-text">{shortId(e.targetId)}</code>
                </span>
              </div>
              {hasMetadata(e.metadata) && (
                <div>
                  <button
                    type="button"
                    className="btn btn-ghost"
                    onClick={() => toggle(e.id)}
                    aria-expanded={expanded[e.id] ? 'true' : 'false'}
                  >
                    {expanded[e.id] ? t('admin.audit.hideMetadata') : t('admin.audit.showMetadata')}
                  </button>
                  {expanded[e.id] && (
                    <pre className="muted-text" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
                      {JSON.stringify(e.metadata, null, 2)}
                    </pre>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
