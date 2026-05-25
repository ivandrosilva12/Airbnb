import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import PropertyCard from '../components/PropertyCard';
import { useFavorites } from '../context/FavoritesContext';
import { useT } from '../i18n/I18nContext';

export default function Favorites() {
  const { t } = useT();
  const { count } = useFavorites();
  const [collections, setCollections] = useState([]);
  const [filter, setFilter] = useState('all'); // 'all' | 'none' | <collectionId>
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [newName, setNewName] = useState('');

  const loadCollections = useCallback(async () => {
    try {
      const res = await api.listCollections();
      setCollections(res.items || []);
    } catch {
      /* non-fatal */
    }
  }, []);

  const loadItems = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const collectionId = filter === 'all' ? undefined : filter;
      const res = await api.listFavorites(collectionId);
      setItems(res.items || []);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [filter]);

  useEffect(() => {
    loadCollections();
  }, [loadCollections, count]);

  // Reload listings when the filter or the favorite set changes.
  useEffect(() => {
    loadItems();
  }, [loadItems, count]);

  async function createCollection(e) {
    e.preventDefault();
    const name = newName.trim();
    if (!name) return;
    try {
      await api.createCollection(name);
      setNewName('');
      loadCollections();
    } catch (e) {
      setError(e.message);
    }
  }

  async function removeCollection(id) {
    if (!window.confirm(t('fav.deleteConfirm'))) return;
    try {
      await api.deleteCollection(id);
      if (filter === id) setFilter('all');
      loadCollections();
      loadItems();
    } catch (e) {
      setError(e.message);
    }
  }

  async function move(propertyId, collectionId) {
    try {
      await api.moveFavorite(propertyId, collectionId);
      loadCollections();
      loadItems();
    } catch (e) {
      setError(e.message);
    }
  }

  const chip = (key, label, extra) => (
    <button
      key={key}
      type="button"
      className={`chip${filter === key ? ' chip-on' : ''}`}
      onClick={() => setFilter(key)}
    >
      {label}
      {extra}
    </button>
  );

  return (
    <div className="container">
      <h1>{t('fav.title')}</h1>

      <div className="wishlist-chips">
        {chip('all', t('fav.all'))}
        {chip('none', t('fav.unsorted'))}
        {collections.map((c) =>
          chip(
            c.id,
            `${c.name} (${c.count})`,
            <span
              role="button"
              tabIndex={0}
              className="chip-x"
              title={t('fav.deleteCollection')}
              onClick={(e) => {
                e.stopPropagation();
                removeCollection(c.id);
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.stopPropagation();
                  removeCollection(c.id);
                }
              }}
            >
              ×
            </span>,
          ),
        )}
      </div>

      <form className="wishlist-new" onSubmit={createCollection}>
        <input
          type="text"
          maxLength={60}
          placeholder={t('fav.collectionName')}
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          aria-label={t('fav.collectionName')}
        />
        <button className="btn btn-ghost" type="submit">{t('fav.create')}</button>
      </form>

      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>{t('common.loading')}</p>
      ) : items.length === 0 ? (
        <p>{t('fav.none')}</p>
      ) : (
        <div className="grid">
          {items.map((p) => (
            <div key={p.id} className="wishlist-item">
              <PropertyCard property={p} />
              <select
                className="wishlist-move"
                value=""
                onChange={(e) => move(p.id, e.target.value)}
                aria-label={t('fav.moveTo')}
              >
                <option value="" disabled>{t('fav.moveTo')}</option>
                <option value="">{t('fav.unsorted')}</option>
                {collections.map((c) => (
                  <option key={c.id} value={c.id}>{c.name}</option>
                ))}
              </select>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
