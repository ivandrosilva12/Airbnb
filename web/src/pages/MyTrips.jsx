import { useEffect, useState } from 'react';
import { api } from '../api/client';

function ReviewForm({ bookingId, onDone, onError }) {
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
      <input placeholder="Share your experience" value={comment} onChange={(e) => setComment(e.target.value)} />
      <button className="btn btn-primary" type="submit" disabled={submitting}>{submitting ? '…' : 'Submit'}</button>
    </form>
  );
}

export default function MyTrips() {
  const [bookings, setBookings] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [reviewing, setReviewing] = useState(null);
  const [reviewed, setReviewed] = useState({});

  async function load() {
    setLoading(true);
    try {
      const res = await api.myBookings();
      setBookings(res.items || []);
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

  return (
    <div className="container">
      <h1>My trips</h1>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>Loading…</p>
      ) : bookings.length === 0 ? (
        <p>No trips yet.</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>Dates</th><th>Guests</th><th>Total</th><th>Status</th><th></th></tr>
          </thead>
          <tbody>
            {bookings.map((b) => (
              <tr key={b.id}>
                <td>{b.checkIn} → {b.checkOut} ({b.nights}n)</td>
                <td>{b.guests}</td>
                <td>{b.totalPrice.display}</td>
                <td><span className={`badge badge-${b.status}`}>{b.status}</span></td>
                <td>
                  {(b.status === 'pending' || b.status === 'confirmed') && (
                    <button className="btn btn-ghost" onClick={() => cancel(b.id)}>Cancel</button>
                  )}
                  {b.status === 'completed' && !reviewed[b.id] && reviewing !== b.id && (
                    <button className="btn btn-ghost" onClick={() => { setError(null); setReviewing(b.id); }}>Leave review</button>
                  )}
                  {reviewed[b.id] && <span className="success">Reviewed ✓</span>}
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
