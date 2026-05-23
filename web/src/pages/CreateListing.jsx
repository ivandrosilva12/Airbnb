import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

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
  cleaningFee: '',
  currency: 'EUR',
  maxGuests: 2,
  bedrooms: 1,
  beds: 1,
  bathrooms: 1,
  amenities: '',
  cancellationPolicy: 'flexible',
};

export default function CreateListing() {
  const { t } = useT();
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
        cleaningFeeCents: Math.round(Number(form.cleaningFee || 0) * 100),
        currency: form.currency,
        maxGuests: Number(form.maxGuests),
        bedrooms: Number(form.bedrooms),
        beds: Number(form.beds),
        bathrooms: Number(form.bathrooms),
        amenities: form.amenities.split(',').map((a) => a.trim()).filter(Boolean),
        cancellationPolicy: form.cancellationPolicy,
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
      <h1>{t('create.title')}</h1>
      {error && <p className="error">{error}</p>}
      <form className="form-grid" onSubmit={submit}>
        <label>{t('create.fTitle')}<input required value={form.title} onChange={set('title')} /></label>
        <label>{t('create.fType')}
          <select value={form.type} onChange={set('type')}>
            <option value="apartment">{t('type.apartment')}</option>
            <option value="house">{t('type.house')}</option>
            <option value="room">{t('type.room')}</option>
            <option value="villa">{t('type.villa')}</option>
            <option value="cabin">{t('type.cabin')}</option>
          </select>
        </label>
        <label className="full">{t('create.fDescription')}<textarea value={form.description} onChange={set('description')} /></label>
        <label>{t('create.fAddress')}<input value={form.addressLine1} onChange={set('addressLine1')} /></label>
        <label>{t('create.fCity')}<input required value={form.city} onChange={set('city')} /></label>
        <label>{t('create.fCountry')}<input required value={form.country} onChange={set('country')} /></label>
        <label>{t('create.fPostal')}<input value={form.postalCode} onChange={set('postalCode')} /></label>
        <label>{t('create.fLat')}<input type="number" step="any" value={form.latitude} onChange={set('latitude')} /></label>
        <label>{t('create.fLng')}<input type="number" step="any" value={form.longitude} onChange={set('longitude')} /></label>
        <label>{t('create.fPrice')}<input required type="number" min="0" step="0.01" value={form.price} onChange={set('price')} /></label>
        <label>{t('create.fCleaning')}<input type="number" min="0" step="0.01" value={form.cleaningFee} onChange={set('cleaningFee')} /></label>
        <label>{t('create.fCurrency')}<input required maxLength="3" value={form.currency} onChange={set('currency')} /></label>
        <label>{t('create.fMaxGuests')}<input type="number" min="1" value={form.maxGuests} onChange={set('maxGuests')} /></label>
        <label>{t('create.fBedrooms')}<input type="number" min="0" value={form.bedrooms} onChange={set('bedrooms')} /></label>
        <label>{t('create.fBeds')}<input type="number" min="0" value={form.beds} onChange={set('beds')} /></label>
        <label>{t('create.fBathrooms')}<input type="number" min="0" value={form.bathrooms} onChange={set('bathrooms')} /></label>
        <label>{t('create.fPolicy')}
          <select value={form.cancellationPolicy} onChange={set('cancellationPolicy')}>
            <option value="flexible">{t('create.policyFlexible')}</option>
            <option value="moderate">{t('create.policyModerate')}</option>
            <option value="strict">{t('create.policyStrict')}</option>
          </select>
        </label>
        <label className="full">{t('create.fAmenities')}<input value={form.amenities} onChange={set('amenities')} placeholder="wifi, kitchen, parking" /></label>
        <label className="full">{t('create.fCover')}<input type="file" accept="image/*" onChange={(e) => setPhoto(e.target.files[0])} /></label>
        <div className="full">
          <button className="btn btn-primary" type="submit" disabled={submitting}>
            {submitting ? t('create.submitting') : t('create.submit')}
          </button>
        </div>
      </form>
    </div>
  );
}
