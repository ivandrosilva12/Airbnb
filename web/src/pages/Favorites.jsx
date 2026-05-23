import { useEffect, useState } from 'react';
import { api } from '../api/client';
import PropertyCard from '../components/PropertyCard';
import { useFavorites } from '../context/FavoritesContext';
import { useT } from '../i18n/I18nContext';

export default function Favorites() {
  const { t } = useT();
  const { count } = useFavorites();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  async function load() {
    setLoading(true);
    try {
      const res = await api.listFavorites();
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }

  // Reload when the favorite set changes (e.g. unsaving from this page).
  useEffect(() => {
    load();
  }, [count]);

  return (
    <div className="container">
      <h1>{t('fav.title')}</h1>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p>{t('fav.none')}</p>
      ) : (
        <div className="grid">
          {items.map((p) => (
            <PropertyCard key={p.id} property={p} />
          ))}
        </div>
      )}
    </div>
  );
}
