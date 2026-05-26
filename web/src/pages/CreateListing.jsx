import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';
import { AMENITY_CODES } from '../amenities';

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
  amenities: [],
  cancellationPolicy: 'flexible',
  weeklyDiscount: '',
  monthlyDiscount: '',
  taxRate: '',
  instantBook: false,
  minNights: 1,
  maxNights: '',
  guestsIncluded: '',
  extraGuestFee: '',
  securityDeposit: '',
};

export default function CreateListing() {
  const { t } = useT();
  const navigate = useNavigate();
  const [form, setForm] = useState(initial);
  const [amenityOptions, setAmenityOptions] = useState(AMENITY_CODES);
  const [photo, setPhoto] = useState(null);
  const [error, setError] = useState(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    api.listAmenities().then((r) => r?.amenities?.length && setAmenityOptions(r.amenities)).catch(() => {});
  }, []);

  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });

  function toggleAmenity(a) {
    setForm((f) => ({
      ...f,
      amenities: f.amenities.includes(a) ? f.amenities.filter((x) => x !== a) : [...f.amenities, a],
    }));
  }

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
        amenities: form.amenities,
        cancellationPolicy: form.cancellationPolicy,
        weeklyDiscountPct: Number(form.weeklyDiscount || 0) / 100,
        monthlyDiscountPct: Number(form.monthlyDiscount || 0) / 100,
        taxRatePct: Number(form.taxRate || 0) / 100,
        instantBook: form.instantBook,
        minNights: Number(form.minNights) || 1,
        maxNights: Number(form.maxNights) || 0,
        guestsIncluded: Number(form.guestsIncluded) || 0,
        extraGuestFeeCents: Math.round(Number(form.extraGuestFee || 0) * 100),
        securityDepositCents: Math.round(Number(form.securityDeposit || 0) * 100),
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
        <label>{t('create.fWeeklyDiscount')}<input type="number" min="0" max="100" step="1" value={form.weeklyDiscount} onChange={set('weeklyDiscount')} /></label>
        <label>{t('create.fMonthlyDiscount')}<input type="number" min="0" max="100" step="1" value={form.monthlyDiscount} onChange={set('monthlyDiscount')} /></label>
        <label>{t('create.fTaxRate')}<input type="number" min="0" max="100" step="0.1" value={form.taxRate} onChange={set('taxRate')} /></label>
        <label>{t('create.fMinNights')}<input type="number" min="1" value={form.minNights} onChange={set('minNights')} /></label>
        <label>{t('create.fMaxNights')}<input type="number" min="0" value={form.maxNights} onChange={set('maxNights')} /></label>
        <label>{t('create.fGuestsIncluded')}<input type="number" min="1" value={form.guestsIncluded} onChange={set('guestsIncluded')} /></label>
        <label>{t('create.fExtraGuestFee')}<input type="number" min="0" step="0.01" value={form.extraGuestFee} onChange={set('extraGuestFee')} /></label>
        <label>{t('create.fSecurityDeposit')}<input type="number" min="0" step="0.01" value={form.securityDeposit} onChange={set('securityDeposit')} /></label>
        <div className="full">
          <span className="form-label">{t('home.amenities')}</span>
          <div className="amenity-filter">
            {amenityOptions.map((a) => (
              <button
                key={a}
                type="button"
                className={`amenity-chip${form.amenities.includes(a) ? ' on' : ''}`}
                onClick={() => toggleAmenity(a)}
              >
                {t(`amenity.${a}`)}
              </button>
            ))}
          </div>
        </div>
        <label className="full instant-book-toggle">
          <input
            type="checkbox"
            checked={form.instantBook}
            onChange={(e) => setForm({ ...form, instantBook: e.target.checked })}
          />
          <span>
            <strong>{t('create.fInstantBook')}</strong>
            <small>{t('create.fInstantBookHint')}</small>
          </span>
        </label>
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
