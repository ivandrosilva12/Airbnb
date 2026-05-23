import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';

export default function PropertyDetail() {
  const { id } = useParams();
  const { authenticated, login } = useAuth();
  const [property, setProperty] = useState(null);
  const [reviews, setReviews] = useState([]);
  const [summary, setSummary] = useState(null);
  const [form, setForm] = useState({ checkIn: '', checkOut: '', guests: 1 });
  const [message, setMessage] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.getProperty(id).then(setProperty).catch((e) => setError(e.message));
    api.getReviews(id).then((r) => setReviews(r.items || [])).catch(() => {});
    api.getReviewSummary(id).then(setSummary).catch(() => {});
  }, [id]);

  async function book(e) {
    e.preventDefault();
    setError(null);
    setMessage(null);
    if (!authenticated) {
      login();
      return;
    }
    try {
      const booking = await api.createBooking({
        propertyId: id,
        checkIn: form.checkIn,
        checkOut: form.checkOut,
        guests: Number(form.guests),
      });
      setMessage(`Booked! ${booking.nights} night(s) for ${booking.totalPrice.display}. Status: ${booking.status}.`);
    } catch (e) {
      setError(e.message);
    }
  }

  if (error && !property) return <div className="container"><p className="error">{error}</p></div>;
  if (!property) return <div className="container"><p>Loading…</p></div>;

  return (
    <div className="container detail">
      <h1>{property.title}</h1>
      <p className="card-meta">
        {property.address.city}, {property.address.country} · {property.type} · up to {property.maxGuests} guests
        {summary && summary.count > 0 && ` · ★ ${summary.averageRating.toFixed(1)} (${summary.count})`}
      </p>

      <div className="gallery">
        {property.photos.length === 0 && <div className="card-photo-placeholder">No photos yet</div>}
        {property.photos.map((ph) => (
          <img key={ph.id} src={ph.url} alt="" />
        ))}
      </div>

      <div className="detail-grid">
        <div>
          <p>{property.description || 'No description provided.'}</p>
          <h3>Amenities</h3>
          <ul className="amenities">
            {property.amenities.length === 0 && <li>None listed</li>}
            {property.amenities.map((a) => (
              <li key={a}>{a}</li>
            ))}
          </ul>

          <h3>Reviews</h3>
          {reviews.length === 0 && <p>No reviews yet.</p>}
          {reviews.map((r) => (
            <div key={r.id} className="review">
              <strong>★ {r.rating}</strong>
              <p>{r.comment}</p>
            </div>
          ))}
        </div>

        <aside className="booking-box">
          <div className="booking-price">
            <strong>{property.pricePerNight.display}</strong> / night
          </div>
          <form onSubmit={book}>
            <label>
              Check in
              <input type="date" required value={form.checkIn} onChange={(e) => setForm({ ...form, checkIn: e.target.value })} />
            </label>
            <label>
              Check out
              <input type="date" required value={form.checkOut} onChange={(e) => setForm({ ...form, checkOut: e.target.value })} />
            </label>
            <label>
              Guests
              <input type="number" min="1" max={property.maxGuests} value={form.guests} onChange={(e) => setForm({ ...form, guests: e.target.value })} />
            </label>
            <button className="btn btn-primary block" type="submit">
              {authenticated ? 'Reserve' : 'Sign in to reserve'}
            </button>
          </form>
          {message && <p className="success">{message}</p>}
          {error && <p className="error">{error}</p>}
        </aside>
      </div>
    </div>
  );
}
