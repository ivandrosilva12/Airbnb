import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

// HostExperienceForm doubles as the create + edit screen for an experience.
// We pick the mode off the route param: with :id present we load the existing
// resource via GET /experiences/:id (public read) and PATCH on submit;
// without it we POST and then hop into the freshly-minted edit URL so the
// host can immediately attach photos.
//
// Photo upload is intentionally minimal here — until the experience object-
// store path lands, hosts paste an objectKey + URL pair (the exact shape the
// backend already accepts for properties). Real multipart upload is a
// future slice.
const CATEGORIES = ['cooking', 'tour', 'sport', 'art', 'wellness', 'other'];
const LANGUAGES = ['en', 'pt', 'es'];

const initial = {
  title: '',
  description: '',
  category: 'tour',
  city: '',
  country: '',
  latitude: 0,
  longitude: 0,
  durationMinutes: 60,
  maxGuests: 4,
  pricePerGuestCents: 0,
  currency: 'EUR',
  language: 'en',
};

function toForm(x) {
  return {
    title: x.title || '',
    description: x.description || '',
    category: x.category || 'tour',
    city: x.city || '',
    country: x.country || '',
    latitude: x.latitude ?? 0,
    longitude: x.longitude ?? 0,
    durationMinutes: x.durationMinutes ?? 60,
    maxGuests: x.maxGuests ?? 4,
    pricePerGuestCents: x.pricePerGuest?.amountCents ?? x.pricePerGuestCents ?? 0,
    currency: x.pricePerGuest?.currency || x.currency || 'EUR',
    language: x.language || 'en',
  };
}

function toBody(form) {
  return {
    title: form.title,
    description: form.description,
    category: form.category,
    city: form.city,
    country: form.country,
    latitude: Number(form.latitude),
    longitude: Number(form.longitude),
    durationMinutes: Number(form.durationMinutes),
    maxGuests: Number(form.maxGuests),
    pricePerGuestCents: Number(form.pricePerGuestCents),
    currency: form.currency,
    language: form.language,
  };
}

export default function HostExperienceForm() {
  const { t } = useT();
  const { id } = useParams();
  const navigate = useNavigate();
  const isEdit = Boolean(id);
  const [form, setForm] = useState(isEdit ? null : initial);
  const [photos, setPhotos] = useState([]);
  const [photoDraft, setPhotoDraft] = useState({ objectKey: '', url: '' });
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  const [savedFlash, setSavedFlash] = useState(false);

  useEffect(() => {
    if (!isEdit) return;
    api
      .getExperience(id)
      .then((x) => {
        setForm(toForm(x));
        setPhotos(x.photos || []);
      })
      .catch((e) => setError(e.message));
  }, [id, isEdit]);

  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });

  async function submit(e) {
    e.preventDefault();
    setError(null);
    setSaving(true);
    try {
      const body = toBody(form);
      if (isEdit) {
        await api.updateExperience(id, body);
        setSavedFlash(true);
        setTimeout(() => setSavedFlash(false), 2000);
      } else {
        const created = await api.createExperience(body);
        navigate(`/host/experiences/${created.id}/edit`);
      }
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  async function addPhoto() {
    if (!photoDraft.objectKey || !photoDraft.url) return;
    setError(null);
    try {
      const updated = await api.addExperiencePhoto(id, {
        objectKey: photoDraft.objectKey,
        url: photoDraft.url,
      });
      setPhotos(updated?.photos || []);
      setPhotoDraft({ objectKey: '', url: '' });
    } catch (err) {
      setError(err.message);
    }
  }

  if (isEdit && !form) {
    return (
      <div className="container">
        {error ? <p className="error">{error}</p> : <p>{t('common.loading')}</p>}
      </div>
    );
  }

  return (
    <div className="container">
      <Link to="/host/experiences" className="muted">{t('host.backDashboard')}</Link>
      <h1>{isEdit ? t('host.experiences.form.editTitle') : t('host.experiences.form.createTitle')}</h1>
      {error && <p className="error">{error}</p>}
      {savedFlash && <p className="success">{t('host.experiences.form.saved')}</p>}
      <form className="form-grid" onSubmit={submit}>
        <label>{t('host.experiences.form.title')}
          <input required value={form.title} onChange={set('title')} />
        </label>
        <label>{t('host.experiences.form.category')}
          <select value={form.category} onChange={set('category')}>
            {CATEGORIES.map((c) => (
              <option key={c} value={c}>{t(`host.experiences.category.${c}`)}</option>
            ))}
          </select>
        </label>
        <label className="full">{t('host.experiences.form.description')}
          <textarea value={form.description} onChange={set('description')} />
        </label>
        <label>{t('host.experiences.form.city')}
          <input required value={form.city} onChange={set('city')} />
        </label>
        <label>{t('host.experiences.form.country')}
          <input required value={form.country} onChange={set('country')} />
        </label>
        <label>{t('host.experiences.form.latitude')}
          <input type="number" step="any" value={form.latitude} onChange={set('latitude')} />
        </label>
        <label>{t('host.experiences.form.longitude')}
          <input type="number" step="any" value={form.longitude} onChange={set('longitude')} />
        </label>
        <label>{t('host.experiences.form.durationMinutes')}
          <input required type="number" min="1" value={form.durationMinutes} onChange={set('durationMinutes')} />
        </label>
        <label>{t('host.experiences.form.maxGuests')}
          <input required type="number" min="1" value={form.maxGuests} onChange={set('maxGuests')} />
        </label>
        <label>{t('host.experiences.form.pricePerGuestCents')}
          <input required type="number" min="0" value={form.pricePerGuestCents} onChange={set('pricePerGuestCents')} />
        </label>
        <label>{t('host.experiences.form.currency')}
          <input required maxLength="3" value={form.currency} onChange={set('currency')} />
        </label>
        <label>{t('host.experiences.form.language')}
          <select value={form.language} onChange={set('language')}>
            {LANGUAGES.map((l) => (
              <option key={l} value={l}>{l.toUpperCase()}</option>
            ))}
          </select>
        </label>
        <div className="full">
          <button className="btn btn-primary" type="submit" disabled={saving}>
            {saving
              ? t('host.experiences.form.saving')
              : isEdit
                ? t('host.experiences.form.save')
                : t('host.experiences.form.create')}
          </button>
        </div>
      </form>

      {isEdit && (
        <section style={{ marginTop: 32 }}>
          <h2>{t('host.experiences.photos.title')}</h2>
          {photos.length === 0 ? (
            <p>{t('photos.empty')}</p>
          ) : (
            <div className="photo-grid">
              {photos.map((p) => (
                <div key={p.id || p.objectKey} className="photo-card">
                  <img src={p.url} alt="" />
                </div>
              ))}
            </div>
          )}
          <div className="form-grid" style={{ marginTop: 12 }}>
            <label>{t('host.experiences.photos.objectKey')}
              <input
                value={photoDraft.objectKey}
                onChange={(e) => setPhotoDraft({ ...photoDraft, objectKey: e.target.value })}
              />
            </label>
            <label>{t('host.experiences.photos.url')}
              <input
                value={photoDraft.url}
                onChange={(e) => setPhotoDraft({ ...photoDraft, url: e.target.value })}
              />
            </label>
            <div className="full">
              <button
                type="button"
                className="btn btn-primary"
                onClick={addPhoto}
                disabled={!photoDraft.objectKey || !photoDraft.url}
              >
                {t('host.experiences.photos.add')}
              </button>
            </div>
          </div>
        </section>
      )}
    </div>
  );
}
