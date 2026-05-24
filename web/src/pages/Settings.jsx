import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { useT } from '../i18n/I18nContext';

export default function Settings() {
  const { t } = useT();
  const [prefs, setPrefs] = useState(null);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api
      .me()
      .then((u) => setPrefs(u.emailPreferences))
      .catch((e) => setError(e.message));
  }, []);

  async function toggle(key) {
    const next = { ...prefs, [key]: !prefs[key] };
    setPrefs(next);
    setSaving(true);
    setSaved(false);
    setError(null);
    try {
      const u = await api.updatePreferences({ [key]: next[key] });
      setPrefs(u.emailPreferences);
      setSaved(true);
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="container">
      <h1>{t('settings.title')}</h1>
      <h2>{t('settings.emailTitle')}</h2>
      <p className="muted">{t('settings.emailHint')}</p>
      {error && <p className="error">{error}</p>}
      {!prefs ? (
        <p>{t('common.loading')}</p>
      ) : (
        <div className="settings-list">
          <label className="settings-row">
            <input type="checkbox" checked={prefs.bookings} disabled={saving} onChange={() => toggle('bookings')} />
            <span>{t('settings.emailBookings')}</span>
          </label>
          <label className="settings-row">
            <input type="checkbox" checked={prefs.messages} disabled={saving} onChange={() => toggle('messages')} />
            <span>{t('settings.emailMessages')}</span>
          </label>
          {saved && <p className="muted">{t('settings.saved')}</p>}
        </div>
      )}
    </div>
  );
}
