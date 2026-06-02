import { useCallback, useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useT } from '../i18n/I18nContext';

// DisputeDetail is the web counterpart of the mobile DisputeScreen: the per-case
// surface of the Resolution Center for the two parties (opener + host). Admin
// decision controls stay in Admin.jsx; this page is read+evidence+host-response.
//
// The "can host respond" gate is client-side hygiene only — the backend
// authoritatively checks listing ownership and 403s otherwise; we surface the
// error inline rather than re-deriving ownership in the browser.
export default function DisputeDetail() {
  const { id } = useParams();
  const { profile } = useAuth();
  const { t } = useT();
  const [d, setD] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [evDraft, setEvDraft] = useState({ url: '', note: '' });
  const [hostDraft, setHostDraft] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setD(await api.getDispute(id));
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    load();
  }, [load]);

  async function submitEvidence(e) {
    e.preventDefault();
    if (!evDraft.url.trim() && !evDraft.note.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const next = await api.addDisputeEvidence(id, {
        url: evDraft.url.trim(),
        note: evDraft.note.trim(),
      });
      setD(next);
      setEvDraft({ url: '', note: '' });
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function submitHostResponse(e) {
    e.preventDefault();
    if (!hostDraft.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const next = await api.hostRespondDispute(id, hostDraft.trim());
      setD(next);
      setHostDraft('');
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  if (loading && !d) return <div className="container"><p>{t('common.loading')}</p></div>;
  if (!d) return <div className="container"><p className="error">{error || t('common.loading')}</p></div>;

  const isOpener = profile && profile.id === d.openerId;
  const isHostRole = profile && (profile.role === 'host' || profile.role === 'admin');
  const isTerminal = d.status === 'resolved' || d.status === 'rejected';
  const canAddEvidence = !isTerminal;
  const canHostRespond = isHostRole && !isOpener && !isTerminal && !d.hostResponse;
  const amountStr = d.requestedAmountCents > 0
    ? `${(d.requestedAmountCents / 100).toFixed(2)} ${d.currency}`
    : '—';

  return (
    <div className="container dispute-detail">
      <header className="dispute-detail-head">
        <h1>{t('dispute.detail.title', { kind: t(`dispute.kind.${d.kind}`) })}</h1>
        <span className={`badge badge-dispute-${d.status}`}>{t(`dispute.status.${d.status}`)}</span>
        {d.overdue && (
          <span className="badge badge-overdue" title={d.dueAt ? new Date(d.dueAt).toLocaleString() : ''}>
            {t('dispute.detail.overdue')}
          </span>
        )}
      </header>

      {error && <p className="error">{error}</p>}

      <section className="dispute-section">
        <div className="dispute-row">
          <span className="dispute-label">{t('dispute.detail.opened')}</span>
          <span>{new Date(d.openedAt).toLocaleString()}</span>
        </div>
        <div className="dispute-row">
          <span className="dispute-label">{t('dispute.detail.slaDeadline')}</span>
          <span style={d.overdue ? { color: '#991b1b', fontWeight: 700 } : undefined}>
            {new Date(d.dueAt).toLocaleString()}
          </span>
        </div>
        <div className="dispute-row">
          <span className="dispute-label">{t('dispute.detail.requestedAmount')}</span>
          <span>{amountStr}</span>
        </div>
        <div className="dispute-row">
          <span className="dispute-label">{t('dispute.detail.reasonLabel')}</span>
          <span style={{ whiteSpace: 'pre-wrap' }}>{d.reason}</span>
        </div>
        {d.hostResponse && (
          <div className="dispute-row">
            <span className="dispute-label">{t('dispute.detail.hostResponseLabel')}</span>
            <span style={{ whiteSpace: 'pre-wrap' }}>{d.hostResponse}</span>
          </div>
        )}
        {d.resolution && (
          <div className="dispute-row">
            <span className="dispute-label">{t('dispute.detail.resolutionLabel')}</span>
            <span style={{ whiteSpace: 'pre-wrap' }}>{d.resolution}</span>
          </div>
        )}
      </section>

      <section className="dispute-section">
        <h2>{t('dispute.detail.evidence')}</h2>
        {d.evidence && d.evidence.length > 0 ? (
          <ul className="evidence-list">
            {d.evidence.map((e) => (
              <li key={e.id} className="evidence-item">
                {e.url && (
                  <div>
                    <a href={e.url} target="_blank" rel="noopener noreferrer">{e.url}</a>
                  </div>
                )}
                {e.note && <div style={{ whiteSpace: 'pre-wrap' }}>{e.note}</div>}
                <div className="muted-text" style={{ fontSize: '.78rem' }}>
                  {new Date(e.addedAt).toLocaleString()}
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="muted">{t('dispute.detail.noEvidence')}</p>
        )}
      </section>

      {canAddEvidence && (
        <form className="dispute-section inline-modify" onSubmit={submitEvidence}>
          <h2>{t('dispute.detail.addEvidence')}</h2>
          <label>
            <input
              type="url"
              placeholder={t('dispute.detail.urlPlaceholder')}
              value={evDraft.url}
              onChange={(e) => setEvDraft((s) => ({ ...s, url: e.target.value }))}
            />
          </label>
          <label>
            <textarea
              placeholder={t('dispute.detail.notePlaceholder')}
              value={evDraft.note}
              maxLength={2000}
              onChange={(e) => setEvDraft((s) => ({ ...s, note: e.target.value }))}
            />
          </label>
          <button className="btn btn-primary btn-sm" type="submit" disabled={busy}>
            {busy ? t('dispute.detail.saving') : t('dispute.detail.addEvidence')}
          </button>
        </form>
      )}

      {canHostRespond && (
        <form className="dispute-section inline-modify" onSubmit={submitHostResponse}>
          <h2>{t('dispute.detail.postHostResponse')}</h2>
          <label>
            <textarea
              placeholder={t('dispute.detail.hostResponsePlaceholder')}
              value={hostDraft}
              maxLength={2000}
              onChange={(e) => setHostDraft(e.target.value)}
            />
          </label>
          <button className="btn btn-primary btn-sm" type="submit" disabled={busy}>
            {busy ? t('dispute.detail.saving') : t('dispute.detail.postHostResponse')}
          </button>
        </form>
      )}

      <p>
        <Link to="/trips" className="btn-link">{t('dispute.detail.back')}</Link>
      </p>
    </div>
  );
}
