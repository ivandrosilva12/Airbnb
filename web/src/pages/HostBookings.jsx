import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api } from '../api/client';

export default function HostBookings() {
  const { id } = useParams();
  const [property, setProperty] = useState(null);
  const [bookings, setBookings] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

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
      <p><Link to="/host">← Back to dashboard</Link></p>
      <h1>Bookings{property ? ` · ${property.title}` : ''}</h1>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>Loading…</p>
      ) : bookings.length === 0 ? (
        <p>No bookings for this listing yet.</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>Dates</th><th>Guests</th><th>Total</th><th>Status</th><th>Actions</th></tr>
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
                    <button className="btn btn-ghost" onClick={() => act(api.confirmBooking, b.id)}>Confirm</button>
                  )}
                  {b.status === 'confirmed' && (
                    <button className="btn btn-ghost" onClick={() => act(api.completeBooking, b.id)}>Mark completed</button>
                  )}
                  {(b.status === 'pending' || b.status === 'confirmed') && (
                    <button className="btn btn-ghost" onClick={() => act(api.cancelBooking, b.id)}>Cancel</button>
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
