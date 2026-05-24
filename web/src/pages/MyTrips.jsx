import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

function ReviewForm({ bookingId, onDone, onError }) {
  const { t } = useT();
  const [rating, setRating] = useState(5);
  const [comment, setComment] = useState('');
  const [submitting, setSubmitting] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setSubmitting(true);
    try {
      await api.createReview({ bookingId, rating: Number(rating), comment });
      onDone();
    } catch (err) {
      onError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="inline-review" onSubmit={submit}>
      <select value={rating} onChange={(e) => setRating(e.target.value)}>
        {[5, 4, 3, 2, 1].map((n) => (
          <option key={n} value={n}>{'★'.repeat(n)}</option>
        ))}
      </select>
      <input placeholder={t('trips.shareExperience')} value={comment} onChange={(e) => setComment(e.target.value)} />
      <button className="btn btn-primary" type="submit" disabled={submitting}>{submitting ? '…' : t('common.submit')}</button>
    </form>
  );
}

export default function MyTrips() {
  const { t } = useT();
  const [bookings, setBookings] = useState([]);
  const [payments, setPayments] = useState({});
  const [guestRating, setGuestRating] = useState(null);
  const [pending, setPending] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [reviewing, setReviewing] = useState(null);
  const [reviewed, setReviewed] = useState({});

  async function load() {
    setLoading(true);
    try {
      const [bookingsRes, paymentsRes, guestReviews, pendingRes] = await Promise.all([
        api.myBookings(),
        api.listPayments(),
        api.myGuestReviews(),
        api.myPendingReviews(),
      ]);
      setBookings(bookingsRes.items || []);
      const byBooking = {};
      for (const p of paymentsRes.items || []) byBooking[p.bookingId] = p;
      setPayments(byBooking);
      setGuestRating(guestReviews?.summary || null);
      setPending(pendingRes?.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function cancel(id) {
    try {
      await api.cancelBooking(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function downloadReceipt(id) {
    try {
      await api.downloadReceipt(id);
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <div className="container">
      <h1>{t('trips.title')}</h1>
      {guestRating && guestRating.count > 0 && (
        <p className="card-meta">{t('trips.guestRating', { rating: guestRating.averageRating.toFixed(1), count: guestRating.count })}</p>
      )}
      {pending.length > 0 && (
        <div className="review-prompt">
          <strong>{t('trips.pendingTitle', { count: pending.length })}</strong>
          <ul>
            {pending.map((p) => (
              <li key={p.bookingId}>
                <span>{p.propertyTitle} · {p.checkIn?.slice(0, 10)} → {p.checkOut?.slice(0, 10)}</span>
                {reviewed[p.bookingId] ? (
                  <span className="success">{t('trips.reviewed')}</span>
                ) : reviewing === p.bookingId ? (
                  <ReviewForm
                    bookingId={p.bookingId}
                    onDone={() => { setReviewing(null); setReviewed((r) => ({ ...r, [p.bookingId]: true })); }}
                    onError={setError}
                  />
                ) : (
                  <button className="btn btn-primary btn-sm" onClick={() => { setError(null); setReviewing(p.bookingId); }}>
                    {t('trips.leaveReview')}
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : bookings.length === 0 ? (
        <p>{t('trips.none')}</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>{t('trips.dates')}</th><th>{t('common.guests')}</th><th>{t('trips.total')}</th><th>{t('trips.status')}</th><th>{t('trips.payment')}</th><th></th></tr>
          </thead>
          <tbody>
            {bookings.map((b) => (
              <tr key={b.id}>
                <td>{b.checkIn} → {b.checkOut} ({b.nights}n)</td>
                <td>{b.guests}</td>
                <td>{b.totalPrice.display}</td>
                <td><span className={`badge badge-${b.status}`}>{t(`status.${b.status}`)}</span></td>
                <td>
                  {payments[b.id]
                    ? <span className={`badge badge-pay-${payments[b.id].status}`}>{t(`pay.${payments[b.id].status}`)}</span>
                    : <span className="muted-text">—</span>}
                  {payments[b.id]?.refundedCents > 0 && (
                    <div className="muted-text" style={{ fontSize: '.78rem' }}>
                      {t('trips.refunded', { amount: `${(payments[b.id].refundedCents / 100).toFixed(2)} ${payments[b.id].amount.currency}` })}
                    </div>
                  )}
                  {payments[b.id] && (
                    <button className="link-btn" onClick={() => downloadReceipt(b.id)}>{t('trips.receipt')}</button>
                  )}
                </td>
                <td>
                  {(b.status === 'pending' || b.status === 'confirmed') && (
                    <button className="btn btn-ghost" onClick={() => cancel(b.id)}>{t('common.cancel')}</button>
                  )}
                  {b.status === 'completed' && !reviewed[b.id] && reviewing !== b.id && (
                    <button className="btn btn-ghost" onClick={() => { setError(null); setReviewing(b.id); }}>{t('trips.leaveReview')}</button>
                  )}
                  {reviewed[b.id] && <span className="success">{t('trips.reviewed')}</span>}
                  {reviewing === b.id && (
                    <ReviewForm
                      bookingId={b.id}
                      onDone={() => { setReviewing(null); setReviewed((r) => ({ ...r, [b.id]: true })); }}
                      onError={setError}
                    />
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
