import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useFavorites } from '../context/FavoritesContext';
import AvailabilityCalendar from '../components/AvailabilityCalendar';

function bookedDaySet(booked) {
  const set = new Set();
  for (const r of booked) {
    const start = new Date(`${r.checkIn}T00:00:00Z`);
    const end = new Date(`${r.checkOut}T00:00:00Z`);
    for (let d = new Date(start); d < end; d.setUTCDate(d.getUTCDate() + 1)) {
      set.add(d.toISOString().slice(0, 10));
    }
  }
  return set;
}

function rangeHitsBooked(checkIn, checkOut, set) {
  const start = new Date(`${checkIn}T00:00:00Z`);
  const end = new Date(`${checkOut}T00:00:00Z`);
  for (let d = new Date(start); d < end; d.setUTCDate(d.getUTCDate() + 1)) {
    if (set.has(d.toISOString().slice(0, 10))) return true;
  }
  return false;
}

export default function PropertyDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { authenticated, profile, login } = useAuth();
  const { isFavorite, toggle } = useFavorites();
  const [property, setProperty] = useState(null);
  const [reviews, setReviews] = useState([]);
  const [summary, setSummary] = useState(null);
  const [booked, setBooked] = useState([]);
  const [form, setForm] = useState({ checkIn: '', checkOut: '', guests: 1 });
  const [message, setMessage] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.getProperty(id).then(setProperty).catch((e) => setError(e.message));
    api.getReviews(id).then((r) => setReviews(r.items || [])).catch(() => {});
    api.getReviewSummary(id).then(setSummary).catch(() => {});
    api.availability(id).then((a) => setBooked(a.booked || [])).catch(() => {});
  }, [id]);

  async function book(e) {
    e.preventDefault();
    setError(null);
    setMessage(null);
    if (!authenticated) {
      login();
      return;
    }
    if (rangeHitsBooked(form.checkIn, form.checkOut, bookedDaySet(booked))) {
      setError('Some of those dates are already booked. Please pick another range.');
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
      api.availability(id).then((a) => setBooked(a.booked || [])).catch(() => {});
    } catch (e) {
      setError(e.message);
    }
  }

  async function contactHost() {
    setError(null);
    if (!authenticated) {
      login();
      return;
    }
    try {
      const conv = await api.startConversation(id);
      navigate(`/messages?conversation=${conv.id}`);
    } catch (e) {
      setError(e.message);
    }
  }

  if (error && !property) return <div className="container"><p className="error">{error}</p></div>;
  if (!property) return <div className="container"><p>Loading…</p></div>;

  const isOwnListing = profile?.id === property.hostId;

  const currency = property.pricePerNight.currency;
  const fmt = (cents) => `${(cents / 100).toFixed(2)} ${currency}`;
  const nights = (() => {
    if (!form.checkIn || !form.checkOut) return 0;
    const ms = new Date(form.checkOut) - new Date(form.checkIn);
    return ms > 0 ? Math.round(ms / 86400000) : 0;
  })();
  const subtotalCents = nights * property.pricePerNight.amountCents;
  const cleaningCents = property.cleaningFee?.amountCents || 0;

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

          <h3>Availability</h3>
          <AvailabilityCalendar booked={booked} months={2} />

          <h3>Cancellation policy</h3>
          <p className="policy-line">
            {{
              flexible: 'Flexible — full refund up to 1 day before check-in.',
              moderate: 'Moderate — full refund up to 5 days before; 50% after.',
              strict: 'Strict — full refund up to 7 days before; 50% up to 2 days; none after.',
            }[property.cancellationPolicy] || property.cancellationPolicy}
          </p>

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
            {nights > 0 && (
              <div className="price-breakdown">
                <div><span>{property.pricePerNight.display} × {nights} night(s)</span><span>{fmt(subtotalCents)}</span></div>
                {cleaningCents > 0 && <div><span>Cleaning fee</span><span>{fmt(cleaningCents)}</span></div>}
                <div className="muted">Service fee added at checkout</div>
                <div className="bd-total"><span>Before service fee</span><span>{fmt(subtotalCents + cleaningCents)}</span></div>
              </div>
            )}
            <button className="btn btn-primary block" type="submit">
              {authenticated ? 'Reserve' : 'Sign in to reserve'}
            </button>
          </form>
          {!isOwnListing && (
            <button className="btn btn-ghost block" onClick={contactHost}>Contact host</button>
          )}
          {authenticated && !isOwnListing && (
            <button className="btn btn-ghost block" onClick={() => toggle(property.id)}>
              {isFavorite(property.id) ? '♥ Saved' : '♡ Save to wishlist'}
            </button>
          )}
          {message && <p className="success">{message}</p>}
          {error && <p className="error">{error}</p>}
        </aside>
      </div>
    </div>
  );
}
