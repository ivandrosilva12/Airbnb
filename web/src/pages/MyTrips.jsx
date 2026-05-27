import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// The optional per-aspect sub-ratings a guest can give a property, in display order.
export const REVIEW_CATEGORIES = ['cleanliness', 'accuracy', 'communication', 'location', 'checkIn', 'value'];

function ReviewForm({ bookingId, onDone, onError }) {
  const { t } = useT();
  const [rating, setRating] = useState(5);
  const [comment, setComment] = useState('');
  const [cats, setCats] = useState({});
  const [submitting, setSubmitting] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setSubmitting(true);
    try {
      const categories = {};
      let any = false;
      for (const k of REVIEW_CATEGORIES) {
        const v = Number(cats[k] || 0);
        categories[k] = v;
        if (v > 0) any = true;
      }
      await api.createReview({ bookingId, rating: Number(rating), comment, categories: any ? categories : undefined });
      onDone();
    } catch (err) {
      onError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="inline-review" onSubmit={submit}>
      <select value={rating} aria-label={t('review.overall')} onChange={(e) => setRating(e.target.value)}>
        {[5, 4, 3, 2, 1].map((n) => (
          <option key={n} value={n}>{'★'.repeat(n)}</option>
        ))}
      </select>
      <input placeholder={t('trips.shareExperience')} value={comment} onChange={(e) => setComment(e.target.value)} />
      <div className="review-cats">
        {REVIEW_CATEGORIES.map((k) => (
          <label key={k} className="review-cat">
            <span>{t(`review.cat.${k}`)}</span>
            <select value={cats[k] || 0} onChange={(e) => setCats((c) => ({ ...c, [k]: Number(e.target.value) }))}>
              <option value={0}>—</option>
              {[1, 2, 3, 4, 5].map((n) => (
                <option key={n} value={n}>{n}</option>
              ))}
            </select>
          </label>
        ))}
      </div>
      <button className="btn btn-primary" type="submit" disabled={submitting}>{submitting ? '…' : t('common.submit')}</button>
    </form>
  );
}

// ModifyForm lets a guest change the dates and/or party size of a still-pending
// booking. The backend re-validates availability and re-prices the stay.
function ModifyForm({ booking, onDone, onError }) {
  const { t } = useT();
  const [checkIn, setCheckIn] = useState(booking.checkIn);
  const [checkOut, setCheckOut] = useState(booking.checkOut);
  const [guests, setGuests] = useState(booking.guests);
  const [submitting, setSubmitting] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setSubmitting(true);
    try {
      await api.modifyBooking(booking.id, { checkIn, checkOut, guests: Number(guests) });
      onDone();
    } catch (err) {
      onError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="inline-modify" onSubmit={submit}>
      <label>{t('trips.checkIn')}<input type="date" value={checkIn} onChange={(e) => setCheckIn(e.target.value)} required /></label>
      <label>{t('trips.checkOut')}<input type="date" value={checkOut} onChange={(e) => setCheckOut(e.target.value)} required /></label>
      <label>{t('common.guests')}<input type="number" min="1" value={guests} onChange={(e) => setGuests(e.target.value)} required /></label>
      <small className="muted-text">{t('trips.modifyNote')}</small>
      <button className="btn btn-primary btn-sm" type="submit" disabled={submitting}>{submitting ? '…' : t('trips.saveChanges')}</button>
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
  const [modifying, setModifying] = useState(null);
  const [offers, setOffers] = useState([]);

  async function load() {
    setLoading(true);
    try {
      const [bookingsRes, paymentsRes, guestReviews, pendingRes, offersRes] = await Promise.all([
        api.myBookings(),
        api.listPayments(),
        api.myGuestReviews(),
        api.myPendingReviews(),
        api.myOffers().catch(() => ({ items: [] })),
      ]);
      setBookings(bookingsRes.items || []);
      const byBooking = {};
      for (const p of paymentsRes.items || []) byBooking[p.bookingId] = p;
      setPayments(byBooking);
      setGuestRating(guestReviews?.summary || null);
      setPending(pendingRes?.items || []);
      setOffers((offersRes?.items || []).filter((o) => o.status === 'pending'));
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  async function acceptOffer(id) {
    setError(null);
    try {
      await api.acceptOffer(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function declineOffer(id) {
    setError(null);
    try {
      await api.declineOffer(id);
      load();
    } catch (e) {
      setError(e.message);
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
      {offers.length > 0 && (
        <div className="review-prompt">
          <strong>{t('offers.title', { count: offers.length })}</strong>
          <ul>
            {offers.map((o) => (
              <li key={o.id}>
                <span>
                  {o.kind === 'special_offer'
                    ? t('offers.special', { price: (o.priceCents / 100).toFixed(2), currency: o.currency })
                    : t('offers.preApproval')}
                  {' · '}{o.checkIn} → {o.checkOut} · {t('common.guests')}: {o.guests}
                  {o.message ? ` · “${o.message}”` : ''}
                </span>
                <span className="offer-actions">
                  <button className="btn btn-primary btn-sm" onClick={() => acceptOffer(o.id)}>{t('offers.accept')}</button>
                  <button className="btn btn-ghost btn-sm" onClick={() => declineOffer(o.id)}>{t('offers.decline')}</button>
                </span>
              </li>
            ))}
          </ul>
        </div>
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
                  {b.status === 'pending' && modifying !== b.id && (
                    <button className="btn btn-ghost" onClick={() => { setError(null); setModifying(b.id); }}>{t('trips.modify')}</button>
                  )}
                  {(b.status === 'pending' || b.status === 'confirmed') && (
                    <button className="btn btn-ghost" onClick={() => cancel(b.id)}>{t('common.cancel')}</button>
                  )}
                  {modifying === b.id && (
                    <ModifyForm
                      booking={b}
                      onDone={() => { setModifying(null); load(); }}
                      onError={setError}
                    />
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
