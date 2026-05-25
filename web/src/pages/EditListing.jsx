import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// EditListing edits the fields the backend allows changing after creation
// (PATCH /properties/:id): title, description, pricing, capacity, policies and
// instant book. Address, type, rooms, amenities and photos are managed
// elsewhere (photos have their own screen).
export default function EditListing() {
  const { t } = useT();
  const { id } = useParams();
  const navigate = useNavigate();
  const [form, setForm] = useState(null);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api
      .getProperty(id)
      .then((p) =>
        setForm({
          title: p.title,
          description: p.description || '',
          price: (p.pricePerNight.amountCents / 100).toString(),
          cleaningFee: ((p.cleaningFee?.amountCents || 0) / 100).toString(),
          currency: p.pricePerNight.currency,
          maxGuests: p.maxGuests,
          cancellationPolicy: p.cancellationPolicy || 'flexible',
          weeklyDiscount: Math.round((p.weeklyDiscountPct || 0) * 100).toString(),
          monthlyDiscount: Math.round((p.monthlyDiscountPct || 0) * 100).toString(),
          taxRate: (Math.round((p.taxRatePct || 0) * 1000) / 10).toString(),
          instantBook: !!p.instantBook,
        }),
      )
      .catch((e) => setError(e.message));
  }, [id]);

  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });

  async function submit(e) {
    e.preventDefault();
    setError(null);
    setSaving(true);
    try {
      await api.updateProperty(id, {
        title: form.title,
        description: form.description,
        priceCents: Math.round(Number(form.price) * 100),
        cleaningFeeCents: Math.round(Number(form.cleaningFee || 0) * 100),
        currency: form.currency,
        maxGuests: Number(form.maxGuests),
        cancellationPolicy: form.cancellationPolicy,
        weeklyDiscountPct: Number(form.weeklyDiscount || 0) / 100,
        monthlyDiscountPct: Number(form.monthlyDiscount || 0) / 100,
        taxRatePct: Number(form.taxRate || 0) / 100,
        instantBook: form.instantBook,
      });
      navigate('/host');
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  if (error && !form) return <div className="container"><p className="error">{error}</p></div>;
  if (!form) return <div className="container"><p>{t('common.loading')}</p></div>;

  return (
    <div className="container">
      <h1>{t('edit.title')}</h1>
      {error && <p className="error">{error}</p>}
      <form className="form-grid" onSubmit={submit}>
        <label>{t('create.fTitle')}<input required value={form.title} onChange={set('title')} /></label>
        <label className="full">{t('create.fDescription')}<textarea value={form.description} onChange={set('description')} /></label>
        <label>{t('create.fPrice')}<input required type="number" min="0" step="0.01" value={form.price} onChange={set('price')} /></label>
        <label>{t('create.fCleaning')}<input type="number" min="0" step="0.01" value={form.cleaningFee} onChange={set('cleaningFee')} /></label>
        <label>{t('create.fCurrency')}<input required maxLength="3" value={form.currency} onChange={set('currency')} /></label>
        <label>{t('create.fMaxGuests')}<input type="number" min="1" value={form.maxGuests} onChange={set('maxGuests')} /></label>
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
        <div className="full">
          <button className="btn btn-primary" type="submit" disabled={saving}>
            {saving ? t('edit.saving') : t('edit.save')}
          </button>
        </div>
      </form>
    </div>
  );
}
