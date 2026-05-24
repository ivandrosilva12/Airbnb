import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

export default function HostPhotos() {
  const { t } = useT();
  const { id } = useParams();
  const [property, setProperty] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  async function load() {
    try {
      setProperty(await api.getProperty(id));
    } catch (e) {
      setError(e.message);
    }
  }

  useEffect(() => {
    load();
  }, [id]);

  const photos = property?.photos || [];

  async function run(fn) {
    setBusy(true);
    setError(null);
    try {
      setProperty(await fn());
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  const order = (ids) => api.reorderPhotos(id, ids);
  const ids = () => photos.map((p) => p.id);

  function makeCover(photoId) {
    run(() => order([photoId, ...ids().filter((x) => x !== photoId)]));
  }
  function move(index, delta) {
    const next = [...ids()];
    const target = index + delta;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    run(() => order(next));
  }
  function remove(photoId) {
    if (!confirm(t('photos.confirmDelete'))) return;
    run(() => api.deletePhoto(id, photoId));
  }
  async function upload(e) {
    const file = e.target.files?.[0];
    if (!file) return;
    await run(() => api.uploadPhoto(id, file));
    e.target.value = '';
  }

  if (!property) {
    return <div className="container">{error ? <p className="error">{error}</p> : <p>{t('common.loading')}</p>}</div>;
  }

  return (
    <div className="container">
      <Link to="/host" className="muted">{t('host.backDashboard')}</Link>
      <h1>{t('photos.title', { title: property.title })}</h1>
      <p className="muted">{t('photos.coverHint')}</p>
      {error && <p className="error">{error}</p>}

      <label className="btn btn-primary" style={{ display: 'inline-block', marginBottom: 16 }}>
        {t('photos.add')}
        <input type="file" accept="image/*" onChange={upload} disabled={busy} style={{ display: 'none' }} />
      </label>

      {photos.length === 0 ? (
        <p>{t('photos.empty')}</p>
      ) : (
        <div className="photo-grid">
          {photos.map((p, i) => (
            <div key={p.id} className={`photo-card${i === 0 ? ' is-cover' : ''}`}>
              <img src={p.url} alt="" />
              {i === 0 && <span className="photo-cover-badge">{t('photos.cover')}</span>}
              <div className="photo-actions">
                <button className="btn btn-ghost" disabled={busy || i === 0} onClick={() => move(i, -1)} aria-label="move left">←</button>
                <button className="btn btn-ghost" disabled={busy || i === photos.length - 1} onClick={() => move(i, 1)} aria-label="move right">→</button>
                {i !== 0 && <button className="btn btn-ghost" disabled={busy} onClick={() => makeCover(p.id)}>{t('photos.makeCover')}</button>}
                <button className="btn btn-ghost" disabled={busy} onClick={() => remove(p.id)}>{t('host.delete')}</button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
