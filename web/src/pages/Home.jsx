import { useEffect, useState } from 'react';
import { api } from '../api/client';
import PropertyCard from '../components/PropertyCard';

export default function Home() {
  const [filters, setFilters] = useState({ city: '', type: '', minGuests: '', checkIn: '', checkOut: '' });
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
      setError('Geolocation is not available in this browser.');
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        const g = { lat: pos.coords.latitude, lng: pos.coords.longitude, radiusKm: 25 };
        setGeo(g);
        load(searchParams(g));
      },
      () => setError('Could not get your location. Please allow location access.'),
    );
  }

  function clearGeo() {
    setGeo(null);
    load(searchParams(null));
  }

  return (
    <div className="container">
      <section className="hero">
        <h1>Find your next stay</h1>
        <form className="search-bar" onSubmit={onSearch}>
          <input
            placeholder="City"
            value={filters.city}
            onChange={(e) => setFilters({ ...filters, city: e.target.value })}
          />
          <select value={filters.type} onChange={(e) => setFilters({ ...filters, type: e.target.value })}>
            <option value="">Any type</option>
            <option value="apartment">Apartment</option>
            <option value="house">House</option>
            <option value="room">Room</option>
            <option value="villa">Villa</option>
            <option value="cabin">Cabin</option>
          </select>
          <input
            type="number"
            min="1"
            placeholder="Guests"
            value={filters.minGuests}
            onChange={(e) => setFilters({ ...filters, minGuests: e.target.value })}
          />
          <input
            type="date"
            title="Check in"
            value={filters.checkIn}
            onChange={(e) => setFilters({ ...filters, checkIn: e.target.value })}
          />
          <input
            type="date"
            title="Check out"
            value={filters.checkOut}
            onChange={(e) => setFilters({ ...filters, checkOut: e.target.value })}
          />
          <button className="btn btn-primary" type="submit">Search</button>
        </form>
        <div className="geo-row">
          {geo ? (
            <span className="geo-chip">
              Near you · {geo.radiusKm} km
              <button type="button" onClick={clearGeo} aria-label="Clear location filter">✕</button>
            </span>
          ) : (
            <button type="button" className="btn btn-ghost" onClick={nearMe}>📍 Near me</button>
          )}
        </div>
      </section>

      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>Loading listings…</p>
      ) : results.items.length === 0 ? (
        <p>No listings found. Try widening your search.</p>
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
