import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

const PAGE_SIZE = 20;
const CATEGORIES = ['cooking', 'tour', 'sport', 'art', 'wellness', 'other'];
const LANGUAGES = ['en', 'pt', 'es'];

// Experiences is the public catalogue page that lists every published
// experience and lets visitors filter by category, city, and language. It
// mirrors Home.jsx: filters drive a server-side query (limit/offset) and the
// Prev/Next controls re-run the last query with a new page index.
export default function Experiences() {
  const { t } = useT();
  const [filters, setFilters] = useState({ category: '', city: '', language: '' });
  const [results, setResults] = useState({ items: [], total: 0 });
  const [page, setPage] = useState(0);
  const [lastParams, setLastParams] = useState({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function load(params = {}, p = 0) {
    setLoading(true);
    setError(null);
    try {
      const cleaned = Object.fromEntries(Object.entries(params).filter(([, v]) => v !== '' && v != null));
      const res = await api.searchExperiences({ ...cleaned, limit: PAGE_SIZE, offset: p * PAGE_SIZE });
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

  function goToPage(p) {
    load(lastParams, p);
  }

  useEffect(() => {
    load();
  }, []);

  function onSearch(e) {
    e.preventDefault();
    load(filters);
  }

  return (
    <div className="container">
      <section className="hero">
        <h1>{t('experiences.title')}</h1>
        <form className="search-bar" onSubmit={onSearch}>
          <select
            value={filters.category}
            aria-label={t('experiences.filter.category')}
            onChange={(e) => setFilters({ ...filters, category: e.target.value })}
          >
            <option value="">{t('experiences.filter.categoryAny')}</option>
            {CATEGORIES.map((c) => (
              <option key={c} value={c}>{t(`experiences.category.${c}`)}</option>
            ))}
          </select>
          <input
            placeholder={t('experiences.filter.city')}
            aria-label={t('experiences.filter.city')}
            value={filters.city}
            onChange={(e) => setFilters({ ...filters, city: e.target.value })}
          />
          <select
            value={filters.language}
            aria-label={t('experiences.filter.language')}
            onChange={(e) => setFilters({ ...filters, language: e.target.value })}
          >
            <option value="">{t('experiences.filter.languageAny')}</option>
            {LANGUAGES.map((l) => (
              <option key={l} value={l}>{t(`experiences.language.${l}`)}</option>
            ))}
          </select>
          <button className="btn btn-primary" type="submit">{t('experiences.filter.apply')}</button>
        </form>
      </section>

      <div className="results-bar">
        {!loading && <span className="results-count">{t('experiences.results', { n: results.total })}</span>}
      </div>

      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('home.loading')}</p>
      ) : results.items.length === 0 ? (
        <p>{t('experiences.empty')}</p>
      ) : (
        <div className="grid">
          {results.items.map((e) => (
            <ExperienceCard key={e.id} experience={e} />
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

// ExperienceCard renders a single catalogue tile. It links to /experiences/:id
// and surfaces the first photo, the title, the city, the duration in minutes,
// the per-guest price, and a small category badge.
function ExperienceCard({ experience: e }) {
  const { t } = useT();
  const photo = e.photos?.[0]?.url;
  return (
    <Link to={`/experiences/${e.id}`} className="card">
      <div className="card-photo">
        {photo ? <img src={photo} alt={e.title} /> : <div className="card-photo-placeholder">{t('common.noPhoto')}</div>}
        <span className="superhost-badge">{t(`experiences.category.${e.category}`)}</span>
      </div>
      <div className="card-body">
        <div className="card-title-row">
          <span className="card-title">{e.title}</span>
        </div>
        <div className="card-meta">
          {e.address.city}, {e.address.country}
        </div>
        <div className="card-meta">
          {t('experiences.card.duration', { n: e.durationMinutes })}
        </div>
        <div className="card-price">
          <strong>{e.pricePerGuest.display}</strong> {t('experiences.card.perGuest')}
        </div>
      </div>
    </Link>
  );
}
