import { useEffect, useState } from 'react';
import { api } from '../api/client';
import PropertyCard from '../components/PropertyCard';
import { useT } from '../i18n/I18nContext';

export default function Home() {
  const { t } = useT();
  const [filters, setFilters] = useState({ city: '', type: '', minGuests: '', checkIn: '', checkOut: '', sort: '' });
  const [geo, setGeo] = useState(null); // { lat, lng, radiusKm } | null
  const [results, setResults] = useState({ items: [], total: 0 });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function load(params = {}) {
    setLoading(true);
    setError(null);
    try {
      const cleaned = Object.fromEntries(Object.entries(params).filter(([, v]) => v !== '' && v != null));
      setResults(await api.searchProperties(cleaned));
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  function searchParams(extraGeo) {
    const g = extraGeo !== undefined ? extraGeo : geo;
    return { ...filters, ...(g ? { lat: g.lat, lng: g.lng, radiusKm: g.radiusKm } : {}) };
  }

  function onSearch(e) {
    e.preventDefault();
    load(searchParams());
  }

  function nearMe() {
    setError(null);
    if (!navigator.geolocation) {
      setError(t('home.geoUnavailable'));
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        const g = { lat: pos.coords.latitude, lng: pos.coords.longitude, radiusKm: 25 };
        setGeo(g);
        load(searchParams(g));
      },
      () => setError(t('home.geoDenied')),
    );
  }

  function clearGeo() {
    setGeo(null);
    load(searchParams(null));
  }

  return (
    <div className="container">
      <section className="hero">
        <h1>{t('home.title')}</h1>
        <form className="search-bar" onSubmit={onSearch}>
          <input
            placeholder={t('home.city')}
            value={filters.city}
            onChange={(e) => setFilters({ ...filters, city: e.target.value })}
          />
          <select value={filters.type} onChange={(e) => setFilters({ ...filters, type: e.target.value })}>
            <option value="">{t('type.any')}</option>
            <option value="apartment">{t('type.apartment')}</option>
            <option value="house">{t('type.house')}</option>
            <option value="room">{t('type.room')}</option>
            <option value="villa">{t('type.villa')}</option>
            <option value="cabin">{t('type.cabin')}</option>
          </select>
          <input
            type="number"
            min="1"
            placeholder={t('common.guests')}
            value={filters.minGuests}
            onChange={(e) => setFilters({ ...filters, minGuests: e.target.value })}
          />
          <input
            type="date"
            title={t('common.checkIn')}
            value={filters.checkIn}
            onChange={(e) => setFilters({ ...filters, checkIn: e.target.value })}
          />
          <input
            type="date"
            title={t('common.checkOut')}
            value={filters.checkOut}
            onChange={(e) => setFilters({ ...filters, checkOut: e.target.value })}
          />
          <select value={filters.sort} onChange={(e) => setFilters({ ...filters, sort: e.target.value })} title="Sort">
            <option value="">{t('home.sort.newest')}</option>
            <option value="price_asc">{t('home.sort.priceAsc')}</option>
            <option value="price_desc">{t('home.sort.priceDesc')}</option>
            <option value="rating">{t('home.sort.rating')}</option>
          </select>
          <button className="btn btn-primary" type="submit">{t('home.search')}</button>
        </form>
        <div className="geo-row">
          {geo ? (
            <span className="geo-chip">
              {t('home.nearYou', { km: geo.radiusKm })}
              <button type="button" onClick={clearGeo} aria-label="Clear location filter">✕</button>
            </span>
          ) : (
            <button type="button" className="btn btn-ghost" onClick={nearMe}>{t('home.nearMe')}</button>
          )}
        </div>
      </section>

      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('home.loading')}</p>
      ) : results.items.length === 0 ? (
        <p>{t('home.empty')}</p>
      ) : (
        <div className="grid">
          {results.items.map((p) => (
            <PropertyCard key={p.id} property={p} />
          ))}
        </div>
      )}
    </div>
  );
}
