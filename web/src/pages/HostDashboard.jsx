import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import { useAuth } from '../context/AuthContext';
import { useT } from '../i18n/I18nContext';

export default function HostDashboard() {
  const { t } = useT();
  const { isHost, becomeHost, refreshProfile } = useAuth();
  const navigate = useNavigate();
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

  // duplicate (S60) — clone an owned listing and jump straight to its edit
  // page so the host can tweak title/address/photos before publishing. We
  // navigate instead of reloading the table to surface that this is a new,
  // editable draft, not a side-effect on the row the host just acted on.
  async function duplicate(id) {
    try {
      const dup = await api.duplicateProperty(id);
      navigate(`/host/properties/${dup.id}/edit`);
    } catch (e) {
      setError(e.message);
    }
  }

  if (!isHost) {
    return (
      <main className="container" aria-label={t('host.becomeTitle')}>
        <h1>{t('host.becomeTitle')}</h1>
        <p>{t('host.becomeText')}</p>
        {error && <p className="error" role="alert">{error}</p>}
        <button className="btn btn-primary" onClick={upgrade}>{t('host.becomeBtn')}</button>
      </main>
    );
  }

  return (
    <main className="container" aria-label={t('host.listings')}>
      <div className="row-between">
        <h1>{t('host.listings')}</h1>
        <Link to="/host/new" className="btn btn-primary">{t('host.newListing')}</Link>
      </div>
      {metrics && (
        <div className="metrics-grid" role="list" aria-label="Host metrics summary">
          {earnings && (
            <Link
              to="/host/earnings"
              className="metric metric-link"
              role="listitem"
              aria-label={`${t('host.metric.earnings')}: ${earnings.net.display}`}
            >
              <div className="metric-value">{earnings.net.display}</div>
              <div className="metric-label">{t('host.metric.earnings')} ›</div>
            </Link>
          )}
          <div className="metric" role="listitem" aria-label={`${t('host.metric.revenue')}: ${metrics.capturedRevenue.display}`}>
            <div className="metric-value">{metrics.capturedRevenue.display}</div>
            <div className="metric-label">{t('host.metric.revenue')}</div>
          </div>
          <div className="metric" role="listitem" aria-label={`${t('host.metric.pending')}: ${metrics.pendingRevenue.display}`}>
            <div className="metric-value">{metrics.pendingRevenue.display}</div>
            <div className="metric-label">{t('host.metric.pending')}</div>
          </div>
          <div className="metric" role="listitem" aria-label={`${t('host.metric.bookings', { confirmed: metrics.confirmed })}: ${metrics.bookings}`}>
            <div className="metric-value">{metrics.bookings}</div>
            <div className="metric-label">{t('host.metric.bookings', { confirmed: metrics.confirmed })}</div>
          </div>
          <div className="metric" role="listitem" aria-label={`${t('host.metric.upcoming')}: ${metrics.upcomingCheckins}`}>
            <div className="metric-value">{metrics.upcomingCheckins}</div>
            <div className="metric-label">{t('host.metric.upcoming')}</div>
          </div>
          <div className="metric" role="listitem" aria-label={`${t('host.metric.nights')}: ${metrics.nightsBooked}`}>
            <div className="metric-value">{metrics.nightsBooked}</div>
            <div className="metric-label">{t('host.metric.nights')}</div>
          </div>
          <div className="metric" role="listitem" aria-label={metrics.reviewCount > 0 ? `${t('host.metric.rating', { count: metrics.reviewCount })}: ${metrics.averageRating.toFixed(1)} stars` : `${t('host.metric.rating', { count: metrics.reviewCount })}: no reviews yet`}>
            <div className="metric-value">{metrics.reviewCount > 0 ? `★ ${metrics.averageRating.toFixed(1)}` : '—'}</div>
            <div className="metric-label">{t('host.metric.rating', { count: metrics.reviewCount })}</div>
          </div>
        </div>
      )}
      {error && <p className="error" role="alert">{error}</p>}
      {loading ? (
        <p role="status">{t('common.loading')}</p>
      ) : properties.length === 0 ? (
        <p>{t('host.noListings')}</p>
      ) : (
        <table className="table">
          <thead>
            <tr><th>{t('host.mTitle')}</th><th>{t('host.mCity')}</th><th>{t('host.mPrice')}</th><th>{t('trips.status')}</th><th>{t('host.mPhotos')}</th><th>{t('host.actions') || 'Actions'}</th></tr>
          </thead>
          <tbody>
            {properties.map((p) => (
              <tr key={p.id}>
                <td><Link to={`/properties/${p.id}`}>{p.title}</Link></td>
                <td>{p.address.city}</td>
                <td>{p.pricePerNight.display}</td>
                <td><span className={`badge badge-${p.status}`} aria-label={`Status: ${p.status}`}>{p.status}</span></td>
                <td>{p.photos.length}</td>
                <td className="actions">
                  {/* Per-row actions disambiguated with the listing title so a
                      screen-reader user can tell which listing "Edit" / "Delete"
                      applies to. */}
                  {p.status !== 'published' && (
                    <button
                      className="btn btn-ghost"
                      onClick={() => publish(p.id)}
                      aria-label={`${t('host.publish')}: ${p.title}`}
                    >{t('host.publish')}</button>
                  )}
                  <Link
                    className="btn btn-ghost"
                    to={`/host/properties/${p.id}/edit`}
                    aria-label={`${t('host.edit')}: ${p.title}`}
                  >{t('host.edit')}</Link>
                  <Link
                    className="btn btn-ghost"
                    to={`/host/properties/${p.id}/bookings`}
                    aria-label={`${t('host.bookings')}: ${p.title}`}
                  >{t('host.bookings')}</Link>
                  <Link
                    className="btn btn-ghost"
                    to={`/host/properties/${p.id}/photos`}
                    aria-label={`${t('host.photos')}: ${p.title}`}
                  >{t('host.photos')}</Link>
                  <button
                    className="btn btn-ghost"
                    onClick={() => duplicate(p.id)}
                    aria-label={`${t('host.duplicate')}: ${p.title}`}
                  >{t('host.duplicate')}</button>
                  <button
                    className="btn btn-ghost"
                    onClick={() => remove(p.id)}
                    aria-label={`${t('host.delete')}: ${p.title}`}
                  >{t('host.delete')}</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </main>
  );
}
