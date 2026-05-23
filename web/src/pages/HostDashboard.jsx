import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';

export default function HostDashboard() {
  const { isHost, becomeHost, refreshProfile } = useAuth();
  const [properties, setProperties] = useState([]);
  const [metrics, setMetrics] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  async function load() {
    setLoading(true);
    try {
      const [props, m] = await Promise.all([api.myProperties(), api.hostMetrics()]);
      setProperties(props.items || []);
      setMetrics(m);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (isHost) load();
    else setLoading(false);
  }, [isHost]);

  async function upgrade() {
    try {
      await becomeHost();
      await refreshProfile();
    } catch (e) {
      setError(e.message);
    }
  }

  async function publish(id) {
    try {
      await api.publishProperty(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  async function remove(id) {
    if (!confirm('Delete this listing?')) return;
    try {
      await api.deleteProperty(id);
      load();
    } catch (e) {
      setError(e.message);
    }
  }

  if (!isHost) {
    return (
      <div className="container">
        <h1>Become a host</h1>
        <p>Upgrade your account to publish listings and manage bookings.</p>
        {error && <p className="error">{error}</p>}
        <button className="btn btn-primary" onClick={upgrade}>Become a host</button>
      </div>
    );
  }

  return (
    <div className="container">
      <div className="row-between">
        <h1>Your listings</h1>
        <Link to="/host/new" className="btn btn-primary">New listing</Link>
      </div>
      {metrics && (
        <div className="metrics-grid">
          <div className="metric"><div className="metric-value">{metrics.capturedRevenue.display}</div><div className="metric-label">Revenue captured</div></div>
          <div className="metric"><div className="metric-value">{metrics.pendingRevenue.display}</div><div className="metric-label">Pending</div></div>
          <div className="metric"><div className="metric-value">{metrics.bookings}</div><div className="metric-label">Bookings ({metrics.confirmed} confirmed)</div></div>
          <div className="metric"><div className="metric-value">{metrics.upcomingCheckins}</div><div className="metric-label">Upcoming check-ins</div></div>
          <div className="metric"><div className="metric-value">{metrics.nightsBooked}</div><div className="metric-label">Nights booked</div></div>
          <div className="metric"><div className="metric-value">{metrics.reviewCount > 0 ? `★ ${metrics.averageRating.toFixed(1)}` : '—'}</div><div className="metric-label">Avg rating ({metrics.reviewCount})</div></div>
        </div>
      )}
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>Loading…</p>
      ) : properties.length === 0 ? (
        <p>You have no listings yet.</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>Title</th><th>City</th><th>Price</th><th>Status</th><th>Photos</th><th></th></tr>
          </thead>
          <tbody>
            {properties.map((p) => (
              <tr key={p.id}>
                <td><Link to={`/properties/${p.id}`}>{p.title}</Link></td>
                <td>{p.address.city}</td>
                <td>{p.pricePerNight.display}</td>
                <td><span className={`badge badge-${p.status}`}>{p.status}</span></td>
                <td>{p.photos.length}</td>
                <td className="actions">
                  {p.status !== 'published' && <button className="btn btn-ghost" onClick={() => publish(p.id)}>Publish</button>}
                  <Link className="btn btn-ghost" to={`/host/properties/${p.id}/bookings`}>Bookings</Link>
                  <button className="btn btn-ghost" onClick={() => remove(p.id)}>Delete</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
