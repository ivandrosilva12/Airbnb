import { useEffect, useState } from 'react';
import { api } from '../api/client';
import PropertyCard from '../components/PropertyCard';
import { useFavorites } from '../context/FavoritesContext';

export default function Favorites() {
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
      <h1>Saved listings</h1>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>Loading…</p>
      ) : items.length === 0 ? (
        <p>No saved listings yet. Tap the heart on any listing to save it.</p>
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
