import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

function GuestReviewForm({ bookingId, onDone, onError }) {
  const { t } = useT();
  const [rating, setRating] = useState(5);
  const [comment, setComment] = useState('');
  const [submitting, setSubmitting] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setSubmitting(true);
    try {
      await api.createGuestReview({ bookingId, rating: Number(rating), comment });
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
      <input placeholder={t('host.howWasGuest')} value={comment} onChange={(e) => setComment(e.target.value)} />
      <button className="btn btn-primary" type="submit" disabled={submitting}>{submitting ? '…' : t('common.submit')}</button>
    </form>
  );
}

export default function HostBookings() {
  const { t } = useT();
  const { id } = useParams();
  const [property, setProperty] = useState(null);
  const [bookings, setBookings] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [reviewing, setReviewing] = useState(null);
  const [reviewed, setReviewed] = useState({});

  async function load() {
    setLoading(true);
    setError(null);
    try {
      const [prop, res] = await Promise.all([api.getProperty(id), api.propertyBookings(id)]);
      setProperty(prop);
      setBookings(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, [id]);

  async function act(fn, bookingId) {
    setError(null);
    try {
      await fn(bookingId);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <div className="container">
      <p><Link to="/host">{t('host.backDashboard')}</Link></p>
      <h1>{t('host.bookings')}{property ? ` · ${property.title}` : ''}</h1>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : bookings.length === 0 ? (
        <p>{t('host.noBookings')}</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>{t('trips.dates')}</th><th>{t('common.guests')}</th><th>{t('trips.total')}</th><th>{t('trips.status')}</th><th></th></tr>
          </thead>
          <tbody>
            {bookings.map((b) => (
              <tr key={b.id}>
                <td>{b.checkIn} → {b.checkOut} ({b.nights}n)</td>
                <td>{b.guests}</td>
                <td>{b.totalPrice.display}</td>
                <td><span className={`badge badge-${b.status}`}>{b.status}</span></td>
                <td className="actions">
                  {b.status === 'pending' && (
                    <button className="btn btn-ghost" onClick={() => act(api.confirmBooking, b.id)}>{t('host.confirm')}</button>
                  )}
                  {b.status === 'confirmed' && (
                    <button className="btn btn-ghost" onClick={() => act(api.completeBooking, b.id)}>{t('host.markCompleted')}</button>
                  )}
                  {(b.status === 'pending' || b.status === 'confirmed') && (
                    <button className="btn btn-ghost" onClick={() => act(api.cancelBooking, b.id)}>{t('common.cancel')}</button>
                  )}
                  {b.status === 'completed' && !reviewed[b.id] && reviewing !== b.id && (
                    <button className="btn btn-ghost" onClick={() => { setError(null); setReviewing(b.id); }}>{t('host.reviewGuest')}</button>
                  )}
                  {reviewed[b.id] && <span className="success">{t('host.guestReviewed')}</span>}
                  {reviewing === b.id && (
                    <GuestReviewForm
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
