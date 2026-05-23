import { useEffect, useState } from 'react';
import { api } from '../api/client';
import PropertyCard from '../components/PropertyCard';

export default function Home() {
  const [filters, setFilters] = useState({ city: '', type: '', minGuests: '', checkIn: '', checkOut: '' });
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

  function onSearch(e) {
    e.preventDefault();
    load(filters);
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
