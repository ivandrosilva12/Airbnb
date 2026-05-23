import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useT } from '../i18n/I18nContext';

export default function HostDashboard() {
  const { t } = useT();
  const { isHost, becomeHost, refreshProfile } = useAuth();
  const [properties, setProperties] = useState([]);
  const [metrics, setMetrics] = useState(null);
  const [earnings, setEarnings] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  async function load() {
    setLoading(true);
    try {
      const [props, m, e] = await Promise.all([api.myProperties(), api.hostMetrics(), api.hostEarnings()]);
      setProperties(props.items || []);
      setMetrics(m);
      setEarnings((e.balances || [])[0] || null);
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
    if (!confirm(t('host.confirmDelete'))) return;
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
        <h1>{t('host.becomeTitle')}</h1>
        <p>{t('host.becomeText')}</p>
        {error && <p className="error">{error}</p>}
        <button className="btn btn-primary" onClick={upgrade}>{t('host.becomeBtn')}</button>
      </div>
    );
  }

  return (
    <div className="container">
      <div className="row-between">
        <h1>{t('host.listings')}</h1>
        <Link to="/host/new" className="btn btn-primary">{t('host.newListing')}</Link>
      </div>
      {metrics && (
        <div className="metrics-grid">
          {earnings && <div className="metric"><div className="metric-value">{earnings.net.display}</div><div className="metric-label">{t('host.metric.earnings')}</div></div>}
          <div className="metric"><div className="metric-value">{metrics.capturedRevenue.display}</div><div className="metric-label">{t('host.metric.revenue')}</div></div>
          <div className="metric"><div className="metric-value">{metrics.pendingRevenue.display}</div><div className="metric-label">{t('host.metric.pending')}</div></div>
          <div className="metric"><div className="metric-value">{metrics.bookings}</div><div className="metric-label">{t('host.metric.bookings', { confirmed: metrics.confirmed })}</div></div>
          <div className="metric"><div className="metric-value">{metrics.upcomingCheckins}</div><div className="metric-label">{t('host.metric.upcoming')}</div></div>
          <div className="metric"><div className="metric-value">{metrics.nightsBooked}</div><div className="metric-label">{t('host.metric.nights')}</div></div>
          <div className="metric"><div className="metric-value">{metrics.reviewCount > 0 ? `★ ${metrics.averageRating.toFixed(1)}` : '—'}</div><div className="metric-label">{t('host.metric.rating', { count: metrics.reviewCount })}</div></div>
        </div>
      )}
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : properties.length === 0 ? (
        <p>{t('host.noListings')}</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>{t('host.mTitle')}</th><th>{t('host.mCity')}</th><th>{t('host.mPrice')}</th><th>{t('trips.status')}</th><th>{t('host.mPhotos')}</th><th></th></tr>
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
                  {p.status !== 'published' && <button className="btn btn-ghost" onClick={() => publish(p.id)}>{t('host.publish')}</button>}
                  <Link className="btn btn-ghost" to={`/host/properties/${p.id}/bookings`}>{t('host.bookings')}</Link>
                  <button className="btn btn-ghost" onClick={() => remove(p.id)}>{t('host.delete')}</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
