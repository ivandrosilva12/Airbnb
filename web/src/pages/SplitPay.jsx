import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useT } from '../i18n/I18nContext';

// SplitPay is the per-payer view of a split-payment plan. It lists every
// share, marks the caller's own (matched by email) and offers an "Authorize
// my share" button while the plan is still pending. The organizer also gets
// a "Cancel split" action.
//
// Trust-mode for this slice: authorising does not call a real card gateway —
// the payer's confirmation IS the commitment. When every share is paid the
// backend confirms the booking automatically.
export default function SplitPay() {
  const { id } = useParams();
  const { profile } = useAuth();
  const { t } = useT();
  const [split, setSplit] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  function load() {
    setError(null);
    api
      .getSplit(id)
      .then(setSplit)
      .catch((e) => setError(e.message));
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  if (error && !split) return <div className="container"><p className="error">{error}</p></div>;
  if (!split) return <div className="container"><p>{t('common.loading')}</p></div>;

  const myEmail = (profile?.email || '').toLowerCase();
  const myShare = split.shares.find((s) => s.payerEmail === myEmail);
  const isOrganizer = profile?.id === split.organizerId;
  const fmt = (cents) => `${(cents / 100).toFixed(2)} ${split.currency}`;

  async function authorize() {
    if (!myShare) return;
    setBusy(true);
    setError(null);
    try {
      await api.authorizeShare(split.id, myShare.id);
      load();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function cancel() {
    if (!confirm(t('split.confirmCancel'))) return;
    setBusy(true);
    setError(null);
    try {
      await api.cancelSplit(split.id);
      load();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="container split-pay">
      <h1>{t('split.title')}</h1>
      <p className="muted">{t('split.hint')}</p>
      {error && <p className="error">{error}</p>}

      <section className="split-summary">
        <div><strong>{t('split.status')}:</strong> {t(`split.s.${split.status}`)}</div>
        <div><strong>{t('split.total')}:</strong> {fmt(split.totalCents)}</div>
        <div>
          <Link to={`/trips`} className="btn-link">{t('split.backToTrips')}</Link>
        </div>
      </section>

      <ul className="split-shares">
        {split.shares.map((s) => {
          const mine = s.payerEmail === myEmail;
          return (
            <li key={s.id} className={`split-share ${s.status}${mine ? ' mine' : ''}`}>
              <span className="share-email">{s.payerEmail}{mine ? ` (${t('split.you')})` : ''}</span>
              <span className="share-amount">{fmt(s.amountCents)}</span>
              <span className="share-status">{t(`split.share.${s.status}`)}</span>
            </li>
          );
        })}
      </ul>

      {myShare && myShare.status === 'pending' && split.status === 'pending' && (
        <button className="btn btn-primary" type="button" disabled={busy} onClick={authorize}>
          {busy ? t('split.authorising') : t('split.authorize', { amount: fmt(myShare.amountCents) })}
        </button>
      )}
      {isOrganizer && split.status === 'pending' && (
        <button className="btn btn-ghost" type="button" disabled={busy} onClick={cancel}>
          {t('split.cancel')}
        </button>
      )}
    </div>
  );
}
