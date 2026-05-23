import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api/client';

const initial = {
  title: '',
  description: '',
  type: 'apartment',
  city: '',
  country: '',
  addressLine1: '',
  postalCode: '',
  latitude: 0,
  longitude: 0,
  price: '',
  currency: 'EUR',
  maxGuests: 2,
  bedrooms: 1,
  beds: 1,
  bathrooms: 1,
  amenities: '',
};

export default function CreateListing() {
  const navigate = useNavigate();
  const [form, setForm] = useState(initial);
  const [photo, setPhoto] = useState(null);
  const [error, setError] = useState(null);
  const [submitting, setSubmitting] = useState(false);

  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });

  async function submit(e) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const created = await api.createProperty({
        title: form.title,
        description: form.description,
        type: form.type,
        addressLine1: form.addressLine1,
        city: form.city,
        country: form.country,
        postalCode: form.postalCode,
        latitude: Number(form.latitude),
        longitude: Number(form.longitude),
        priceCents: Math.round(Number(form.price) * 100),
        currency: form.currency,
        maxGuests: Number(form.maxGuests),
        bedrooms: Number(form.bedrooms),
        beds: Number(form.beds),
        bathrooms: Number(form.bathrooms),
        amenities: form.amenities.split(',').map((a) => a.trim()).filter(Boolean),
      });
      if (photo) {
        await api.uploadPhoto(created.id, photo);
      }
      navigate('/host');
    } catch (e) {
      setError(e.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="container">
      <h1>New listing</h1>
      {error && <p className="error">{error}</p>}
      <form className="form-grid" onSubmit={submit}>
        <label>Title<input required value={form.title} onChange={set('title')} /></label>
        <label>Type
          <select value={form.type} onChange={set('type')}>
            <option value="apartment">Apartment</option>
            <option value="house">House</option>
            <option value="room">Room</option>
            <option value="villa">Villa</option>
            <option value="cabin">Cabin</option>
          </select>
        </label>
        <label className="full">Description<textarea value={form.description} onChange={set('description')} /></label>
        <label>Address<input value={form.addressLine1} onChange={set('addressLine1')} /></label>
        <label>City<input required value={form.city} onChange={set('city')} /></label>
        <label>Country<input required value={form.country} onChange={set('country')} /></label>
        <label>Postal code<input value={form.postalCode} onChange={set('postalCode')} /></label>
        <label>Latitude<input type="number" step="any" value={form.latitude} onChange={set('latitude')} /></label>
        <label>Longitude<input type="number" step="any" value={form.longitude} onChange={set('longitude')} /></label>
        <label>Price / night<input required type="number" min="0" step="0.01" value={form.price} onChange={set('price')} /></label>
        <label>Currency<input required maxLength="3" value={form.currency} onChange={set('currency')} /></label>
        <label>Max guests<input type="number" min="1" value={form.maxGuests} onChange={set('maxGuests')} /></label>
        <label>Bedrooms<input type="number" min="0" value={form.bedrooms} onChange={set('bedrooms')} /></label>
        <label>Beds<input type="number" min="0" value={form.beds} onChange={set('beds')} /></label>
        <label>Bathrooms<input type="number" min="0" value={form.bathrooms} onChange={set('bathrooms')} /></label>
        <label className="full">Amenities (comma separated)<input value={form.amenities} onChange={set('amenities')} placeholder="wifi, kitchen, parking" /></label>
        <label className="full">Cover photo<input type="file" accept="image/*" onChange={(e) => setPhoto(e.target.files[0])} /></label>
        <div className="full">
          <button className="btn btn-primary" type="submit" disabled={submitting}>
            {submitting ? 'Creating…' : 'Create listing'}
          </button>
        </div>
      </form>
    </div>
  );
}
