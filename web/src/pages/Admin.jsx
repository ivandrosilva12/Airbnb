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
      <ActiveAlertsPanel />
      <VerificationQueue />
      <ReportQueue />
      <DisputeQueue />
      <CouponsPanel />
      <SilencesPanel />
    </div>
  );
}

// DisputeQueue lists open Resolution Center cases and lets an admin record a
// public decision (resolve / reject) per case.
function DisputeQueue() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [decisions, setDecisions] = useState({}); // disputeId -> text

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

  async function decide(disputeId, fn) {
    const resolution = (decisions[disputeId] || '').trim();
    if (!resolution) {
      setError(t('admin.dispute.needResolution'));
      return;
    }
    setError(null);
    try {
      await fn(disputeId, resolution);
      setDecisions((d) => ({ ...d, [disputeId]: '' }));
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
                value={decisions[d.id] || ''}
                onChange={(e) => setDecisions((m) => ({ ...m, [d.id]: e.target.value }))}
              />
              <div className="admin-actions">
                <button className="btn btn-primary btn-sm" onClick={() => decide(d.id, api.adminResolveDispute)}>{t('admin.dispute.resolve')}</button>
                <button className="btn btn-ghost btn-sm" onClick={() => decide(d.id, api.adminRejectDispute)}>{t('admin.dispute.reject')}</button>
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
