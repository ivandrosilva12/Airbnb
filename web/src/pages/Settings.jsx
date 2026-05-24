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

      <VerificationPanel />
    </div>
  );
}

// VerificationPanel lets a user submit a KYC identity document and shows the
// status of their latest request.
function VerificationPanel() {
  const { t } = useT();
  const [verification, setVerification] = useState(null);
  const [loaded, setLoaded] = useState(false);
  const [form, setForm] = useState({ documentType: 'passport', documentRef: '', legalName: '' });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    api
      .getVerification()
      .then((res) => setVerification(res.verification))
      .catch((e) => setError(e.message))
      .finally(() => setLoaded(true));
  }, []);

  const status = verification?.status || 'none';
  const canSubmit = status === 'none' || status === 'rejected';

  async function submit(e) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const v = await api.submitVerification(form);
      setVerification(v);
      setForm({ documentType: 'passport', documentRef: '', legalName: '' });
    } catch (err) {
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className="verify-panel">
      <h2>{t('verify.title')}</h2>
      <p className="muted">{t('verify.hint')}</p>
      {error && <p className="error">{error}</p>}
      {!loaded ? (
        <p>{t('common.loading')}</p>
      ) : (
        <>
          <p>
            {t('verify.statusLabel')}:{' '}
            <span className={`verify-badge verify-${status}`}>{t(`verify.status.${status}`)}</span>
          </p>
          {status === 'pending' && <p className="muted">{t('verify.pendingMsg')}</p>}
          {status === 'approved' && <p className="muted">{t('verify.approvedMsg')}</p>}
          {status === 'rejected' && (
            <p className="muted">{t('verify.rejectedMsg', { reason: verification.rejectionReason || '—' })}</p>
          )}

          {canSubmit && (
            <form className="verify-form" onSubmit={submit}>
              <label>
                {t('verify.docType')}
                <select
                  value={form.documentType}
                  onChange={(e) => setForm({ ...form, documentType: e.target.value })}
                >
                  <option value="passport">{t('verify.doc.passport')}</option>
                  <option value="id_card">{t('verify.doc.id_card')}</option>
                  <option value="driver_license">{t('verify.doc.driver_license')}</option>
                </select>
              </label>
              <label>
                {t('verify.legalName')}
                <input
                  value={form.legalName}
                  onChange={(e) => setForm({ ...form, legalName: e.target.value })}
                  required
                />
              </label>
              <label>
                {t('verify.docRef')}
                <input
                  value={form.documentRef}
                  onChange={(e) => setForm({ ...form, documentRef: e.target.value })}
                  required
                />
              </label>
              <button className="btn btn-primary" type="submit" disabled={submitting}>
                {submitting ? t('verify.submitting') : t('verify.submit')}
              </button>
            </form>
          )}
        </>
      )}
    </section>
  );
}
