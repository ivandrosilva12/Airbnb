import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import PropertyCard from '../components/PropertyCard';
import MapView from '../components/MapView';
import { useAuth } from '../context/AuthContext';
import { useT } from '../i18n/I18nContext';
import { AMENITY_CODES } from '../amenities';

const PAGE_SIZE = 12;

// Serialise/parse a cleaned search-params object to/from a URL query string,
// used to store and replay saved searches (amenity may be a repeated key).
function paramsToQuery(params) {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (Array.isArray(v)) v.forEach((x) => sp.append(k, x));
    else if (v !== '' && v != null) sp.append(k, v);
  }
  return sp.toString();
}
function queryToParams(query) {
  const sp = new URLSearchParams(query);
  const out = {};
  for (const k of new Set(sp.keys())) {
    const all = sp.getAll(k);
    out[k] = all.length > 1 ? all : all[0];
  }
  return out;
}

export default function Home() {
  const { t } = useT();
  const { authenticated } = useAuth();
  const navigate = useNavigate();
  const [view, setView] = useState('list');
  const [saved, setSaved] = useState([]);
  const [amenityOptions, setAmenityOptions] = useState(AMENITY_CODES);
  // sort default: "ranked" surfaces the composite-score ordering (S63) —
  // rating volume-weighted, superhost boost, photo richness, cold-start
  // bonus. Explicit "" still works (server falls back to newest) so users
  // can revert. Persisted in component state, not in the URL, on purpose:
  // the URL stays cleaner and the ranking is the default people expect.
  const [filters, setFilters] = useState({ q: '', city: '', type: '', minGuests: '', minPrice: '', maxPrice: '', bedrooms: '', instantBook: false, checkIn: '', checkOut: '', sort: 'ranked', amenities: [] });
  const [showMore, setShowMore] = useState(false);
  const [geo, setGeo] = useState(null); // { lat, lng, radiusKm } | null
  const [results, setResults] = useState({ items: [], total: 0 });
  const [page, setPage] = useState(0);
  const [lastParams, setLastParams] = useState({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // load runs a search. Changing the filters/geo starts a new query at page 0;
  // the pagination controls re-run the last query with an explicit page index.
  async function load(params = {}, p = 0) {
    setLoading(true);
    setError(null);
    try {
      const cleaned = Object.fromEntries(Object.entries(params).filter(([, v]) => v !== '' && v != null));
      const res = await api.searchProperties({ ...cleaned, limit: PAGE_SIZE, offset: p * PAGE_SIZE });
      setResults(res);
      setLastParams(cleaned);
      setPage(p);
      if (p !== 0) window.scrollTo({ top: 0, behavior: 'smooth' });
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  // goToPage moves within the current result set, preserving the active filters.
  function goToPage(p) {
    load(lastParams, p);
  }

  useEffect(() => {
    load();
    api.listAmenities().then((r) => r?.amenities?.length && setAmenityOptions(r.amenities)).catch(() => {});
  }, []);

  function loadSaved() {
    if (!authenticated) return;
    api.listSavedSearches().then((r) => setSaved(r.items || [])).catch(() => {});
  }
  useEffect(loadSaved, [authenticated]);

  async function saveCurrent() {
    const name = window.prompt(t('saved.namePrompt'));
    if (!name || !name.trim()) return;
    try {
      await api.saveSearch({ name: name.trim(), query: paramsToQuery(lastParams), alertsEnabled: true });
      loadSaved();
    } catch (e) {
      setError(e.message);
    }
  }

  function applySaved(s) {
    load(queryToParams(s.query));
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  async function toggleAlerts(s) {
    try {
      await api.setSearchAlerts(s.id, !s.alertsEnabled);
      loadSaved();
    } catch (e) {
      setError(e.message);
    }
  }

  async function removeSaved(s) {
    try {
      await api.deleteSavedSearch(s.id);
      loadSaved();
    } catch (e) {
      setError(e.message);
    }
  }

  function buildParams(f = filters, g = geo) {
    const { amenities, instantBook, minPrice, maxPrice, ...rest } = f;
    const toCents = (v) => (v === '' || v == null ? '' : Math.round(Number(v) * 100));
    return {
      ...rest,
      minPrice: toCents(minPrice),
      maxPrice: toCents(maxPrice),
      ...(instantBook ? { instantBook: 'true' } : {}),
      ...(amenities.length ? { amenity: amenities } : {}),
      ...(g ? { lat: g.lat, lng: g.lng, radiusKm: g.radiusKm } : {}),
    };
  }

  function toggleAmenity(a) {
    const next = filters.amenities.includes(a)
      ? filters.amenities.filter((x) => x !== a)
      : [...filters.amenities, a];
    const updated = { ...filters, amenities: next };
    setFilters(updated);
    load(buildParams(updated));
  }

  function onSearch(e) {
    e.preventDefault();
    load(buildParams());
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
        load(buildParams(filters, g));
      },
      () => setError(t('home.geoDenied')),
    );
  }

  function clearGeo() {
    setGeo(null);
    load(buildParams(filters, null));
  }

  return (
    <div className="container">
      <section className="hero">
        <h1>{t('home.title')}</h1>
        <form className="search-bar" onSubmit={onSearch}>
          <input
            placeholder={t('home.keyword')}
            aria-label={t('home.keyword')}
            value={filters.q}
            onChange={(e) => setFilters({ ...filters, q: e.target.value })}
          />
          <input
            placeholder={t('home.city')}
            aria-label={t('home.city')}
            value={filters.city}
            onChange={(e) => setFilters({ ...filters, city: e.target.value })}
          />
          <select value={filters.type} aria-label={t('home.type')} onChange={(e) => setFilters({ ...filters, type: e.target.value })}>
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
            aria-label={t('common.guests')}
            value={filters.minGuests}
            onChange={(e) => setFilters({ ...filters, minGuests: e.target.value })}
          />
          <input
            type="date"
            title={t('common.checkIn')}
            aria-label={t('common.checkIn')}
            value={filters.checkIn}
            onChange={(e) => setFilters({ ...filters, checkIn: e.target.value })}
          />
          <input
            type="date"
            title={t('common.checkOut')}
            aria-label={t('common.checkOut')}
            value={filters.checkOut}
            onChange={(e) => setFilters({ ...filters, checkOut: e.target.value })}
          />
          <select value={filters.sort} onChange={(e) => setFilters({ ...filters, sort: e.target.value })} aria-label={t('home.sortLabel')}>
            <option value="ranked">{t('home.sort.ranked')}</option>
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
              <button type="button" onClick={clearGeo} aria-label={t('a11y.clearLocation')}><span aria-hidden="true">✕</span></button>
            </span>
          ) : (
            <button type="button" className="btn btn-ghost" onClick={nearMe}>{t('home.nearMe')}</button>
          )}
          <button type="button" className="btn btn-ghost" onClick={() => setShowMore((v) => !v)} aria-expanded={showMore}>
            {t('home.moreFilters')}
          </button>
        </div>
        {showMore && (
          <form className="more-filters" onSubmit={onSearch}>
            <label>
              {t('home.minPrice')}
              <input type="number" min="0" value={filters.minPrice} onChange={(e) => setFilters({ ...filters, minPrice: e.target.value })} />
            </label>
            <label>
              {t('home.maxPrice')}
              <input type="number" min="0" value={filters.maxPrice} onChange={(e) => setFilters({ ...filters, maxPrice: e.target.value })} />
            </label>
            <label>
              {t('home.minBedrooms')}
              <select value={filters.bedrooms} onChange={(e) => setFilters({ ...filters, bedrooms: e.target.value })}>
                <option value="">{t('home.any')}</option>
                {[1, 2, 3, 4, 5].map((n) => <option key={n} value={n}>{n}+</option>)}
              </select>
            </label>
            <label className="more-filters-check">
              <input type="checkbox" checked={filters.instantBook} onChange={(e) => setFilters({ ...filters, instantBook: e.target.checked })} />
              <span>{t('detail.instantBook')}</span>
            </label>
            <button className="btn btn-primary" type="submit">{t('home.apply')}</button>
          </form>
        )}
        <div className="amenity-filter">
          <span className="amenity-filter-label">{t('home.amenities')}:</span>
          {amenityOptions.map((a) => (
            <button
              key={a}
              type="button"
              className={`amenity-chip${filters.amenities.includes(a) ? ' on' : ''}`}
              onClick={() => toggleAmenity(a)}
            >
              {t(`amenity.${a}`)}
            </button>
          ))}
        </div>
      </section>

      {authenticated && (
        <div className="saved-searches">
          <button type="button" className="btn btn-ghost btn-sm" onClick={saveCurrent}>★ {t('saved.save')}</button>
          {saved.map((s) => (
            <span key={s.id} className="saved-chip">
              <button type="button" className="saved-chip-name" onClick={() => applySaved(s)}>{s.name}</button>
              <button
                type="button"
                className="saved-chip-bell"
                title={s.alertsEnabled ? t('saved.alertsOn') : t('saved.alertsOff')}
                aria-label={s.alertsEnabled ? t('saved.alertsOn') : t('saved.alertsOff')}
                onClick={() => toggleAlerts(s)}
              >
                {s.alertsEnabled ? '🔔' : '🔕'}
              </button>
              <button type="button" className="saved-chip-x" aria-label={t('saved.remove')} onClick={() => removeSaved(s)}>✕</button>
            </span>
          ))}
        </div>
      )}

      <div className="results-bar">
        {!loading && <span className="results-count">{t('home.results', { n: results.total })}</span>}
        <div className="view-toggle">
          <button type="button" className={view === 'list' ? 'on' : ''} onClick={() => setView('list')}>{t('home.viewList')}</button>
          <button type="button" className={view === 'map' ? 'on' : ''} onClick={() => setView('map')}>{t('home.viewMap')}</button>
        </div>
      </div>

      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('home.loading')}</p>
      ) : results.items.length === 0 ? (
        <p>{t('home.empty')}</p>
      ) : view === 'map' ? (
        <MapView properties={results.items} onSelect={(id) => navigate(`/properties/${id}`)} />
      ) : (
        <div className="grid">
          {results.items.map((p) => (
            <PropertyCard key={p.id} property={p} />
          ))}
        </div>
      )}

      {!loading && !error && results.total > PAGE_SIZE && (
        <nav className="pagination" aria-label={t('home.page', { page: page + 1, pages: Math.ceil(results.total / PAGE_SIZE) })}>
          <button type="button" className="btn btn-ghost" disabled={page === 0} onClick={() => goToPage(page - 1)}>
            ‹ {t('home.prev')}
          </button>
          <span className="pagination-info">
            {t('home.page', { page: page + 1, pages: Math.ceil(results.total / PAGE_SIZE) })}
          </span>
          <button
            type="button"
            className="btn btn-ghost"
            disabled={(page + 1) * PAGE_SIZE >= results.total}
            onClick={() => goToPage(page + 1)}
          >
            {t('home.next')} ›
          </button>
        </nav>
      )}
    </div>
  );
}
