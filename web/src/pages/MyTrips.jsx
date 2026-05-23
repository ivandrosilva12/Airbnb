import { useEffect, useState } from 'react';
import { api } from '../api/client';

export default function MyTrips() {
  const [bookings, setBookings] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

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
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
