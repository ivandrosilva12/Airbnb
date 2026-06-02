import { useEffect, useState } from 'react';
import { api } from '../api/client';
import keycloak from '../keycloak';
import { useConsent } from '../context/ConsentContext';
import { useT } from '../i18n/I18nContext';

export default function Settings() {
  const { t } = useT();
  const [emailPrefs, setEmailPrefs] = useState(null);
  const [pushPrefs, setPushPrefs] = useState(null);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api
      .me()
      .then((u) => {
        setEmailPrefs(u.emailPreferences);
        setPushPrefs(u.pushPreferences || { bookings: true, messages: true });
      })
      .catch((e) => setError(e.message));
  }, []);

  // toggle sends only the changed channel/key so the user can have email and
  // push opt-outs that diverge per category.
  async function toggle(channel, key) {
    const prev = channel === 'push' ? pushPrefs : emailPrefs;
    const nextValue = !prev[key];
    if (channel === 'push') {
      setPushPrefs({ ...prev, [key]: nextValue });
    } else {
      setEmailPrefs({ ...prev, [key]: nextValue });
    }
    setSaving(true);
    setSaved(false);
    setError(null);
    try {
      const body = { [channel]: { [key]: nextValue } };
      const u = await api.updatePreferences(body);
      setEmailPrefs(u.emailPreferences);
      setPushPrefs(u.pushPreferences || { bookings: true, messages: true });
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
      <ProfilePanel />
      <h2>{t('settings.emailTitle')}</h2>
      <p className="muted">{t('settings.emailHint')}</p>
      {error && <p className="error">{error}</p>}
      {!emailPrefs ? (
        <p>{t('common.loading')}</p>
      ) : (
        <div className="settings-list">
          <label className="settings-row">
            <input type="checkbox" checked={emailPrefs.bookings} disabled={saving} onChange={() => toggle('email', 'bookings')} />
            <span>{t('settings.emailBookings')}</span>
          </label>
          <label className="settings-row">
            <input type="checkbox" checked={emailPrefs.messages} disabled={saving} onChange={() => toggle('email', 'messages')} />
            <span>{t('settings.emailMessages')}</span>
          </label>
        </div>
      )}

      <h2>{t('settings.pushTitle')}</h2>
      <p className="muted">{t('settings.pushHint')}</p>
      {!pushPrefs ? (
        <p>{t('common.loading')}</p>
      ) : (
        <div className="settings-list">
          <label className="settings-row">
            <input type="checkbox" checked={pushPrefs.bookings} disabled={saving} onChange={() => toggle('push', 'bookings')} />
            <span>{t('settings.pushBookings')}</span>
          </label>
          <label className="settings-row">
            <input type="checkbox" checked={pushPrefs.messages} disabled={saving} onChange={() => toggle('push', 'messages')} />
            <span>{t('settings.pushMessages')}</span>
          </label>
          {saved && <p className="muted">{t('settings.saved')}</p>}
        </div>
      )}

      <VerificationPanel />
      <SecurityPanel />
      <PrivacyPanel />
      <CookieConsentPanel />
      <SavedRepliesPanel />
    </div>
  );
}

// CookieConsentPanel (S70) lets a user change the cookie/tracking preferences
// they recorded via the banner — or reopen the banner from scratch. The
// "current state" header doubles as a transparency signal: "this is what
// the platform is currently allowed to do".
function CookieConsentPanel() {
  const { t } = useT();
  const { consent, acceptAll, rejectNonEssential, savePartial, reopen } = useConsent();
  // Local draft mirrors the banner's customize picker so toggles are
  // staged before Save. Necessary stays read-only / always-on.
  const [draft, setDraft] = useState({
    analytics: consent.analytics,
    marketing: consent.marketing,
  });
  // If the persisted consent changes elsewhere (e.g. the user clicked
  // "Reset" then chose Reject), re-sync the draft.
  useEffect(() => {
    setDraft({ analytics: consent.analytics, marketing: consent.marketing });
  }, [consent.analytics, consent.marketing]);

  return (
    <section className="privacy-panel" aria-labelledby="cookie-pref-title">
      <h2 id="cookie-pref-title">{t('consent.settingsTitle')}</h2>
      <p className="muted">{t('consent.settingsHint')}</p>
      {consent.decided && (
        <p className="muted-text">
          {t('consent.lastSet', {
            when: consent.decidedAt
              ? new Date(consent.decidedAt).toLocaleDateString()
              : '—',
          })}
        </p>
      )}
      <ul className="consent-categories">
        <li>
          <label className="consent-row">
            <input type="checkbox" checked disabled />
            <span>
              <strong>{t('consent.cat.necessary')}</strong>
              <span className="muted-text"> — {t('consent.cat.necessaryHint')}</span>
            </span>
          </label>
        </li>
        <li>
          <label className="consent-row">
            <input
              type="checkbox"
              checked={draft.analytics}
              onChange={(e) => setDraft({ ...draft, analytics: e.target.checked })}
            />
            <span>
              <strong>{t('consent.cat.analytics')}</strong>
              <span className="muted-text"> — {t('consent.cat.analyticsHint')}</span>
            </span>
          </label>
        </li>
        <li>
          <label className="consent-row">
            <input
              type="checkbox"
              checked={draft.marketing}
              onChange={(e) => setDraft({ ...draft, marketing: e.target.checked })}
            />
            <span>
              <strong>{t('consent.cat.marketing')}</strong>
              <span className="muted-text"> — {t('consent.cat.marketingHint')}</span>
            </span>
          </label>
        </li>
      </ul>
      <div className="privacy-actions">
        <button type="button" className="btn btn-ghost" onClick={rejectNonEssential}>
          {t('consent.reject')}
        </button>
        <button type="button" className="btn btn-ghost" onClick={acceptAll}>
          {t('consent.acceptAll')}
        </button>
        <button type="button" className="btn btn-primary" onClick={() => savePartial(draft)}>
          {t('consent.save')}
        </button>
        <button type="button" className="btn-link-danger" onClick={reopen}>
          {t('consent.reset')}
        </button>
      </div>
    </section>
  );
}

// SavedRepliesPanel is the host's playbook editor: add, rename, edit body,
// delete. The composer in Messages.jsx reads the same list and renders chips.
// Empty by default — the section disappears visually for users who don't
// bother saving anything (the header still shows so they discover the feature).
function SavedRepliesPanel() {
  const { t } = useT();
  const [items, setItems] = useState([]);
  const [draft, setDraft] = useState({ label: '', body: '' });
  const [editingId, setEditingId] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  function reload() {
    setError(null);
    api
      .listMessageTemplates()
      .then((r) => setItems(r.items || []))
      .catch((e) => setError(e.message));
  }

  useEffect(() => {
    reload();
  }, []);

  async function save(e) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (editingId) {
        await api.updateMessageTemplate(editingId, draft);
      } else {
        await api.createMessageTemplate(draft);
      }
      setDraft({ label: '', body: '' });
      setEditingId(null);
      reload();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  function startEdit(it) {
    setEditingId(it.id);
    setDraft({ label: it.label, body: it.body });
  }

  function cancelEdit() {
    setEditingId(null);
    setDraft({ label: '', body: '' });
  }

  async function remove(id) {
    if (!confirm(t('savedReplies.confirmDelete'))) return;
    setError(null);
    try {
      await api.deleteMessageTemplate(id);
      if (editingId === id) cancelEdit();
      reload();
    } catch (err) {
      setError(err.message);
    }
  }

  return (
    <section className="saved-replies-panel">
      <h2>{t('savedReplies.title')}</h2>
      <p className="muted">{t('savedReplies.hint')}</p>
      {error && <p className="error" role="alert">{error}</p>}
      <form
        className="form-grid"
        onSubmit={save}
        aria-label={editingId ? t('savedReplies.save') : t('savedReplies.add')}
      >
        <label>{t('savedReplies.fLabel')}
          <input
            required
            maxLength={60}
            value={draft.label}
            onChange={(e) => setDraft({ ...draft, label: e.target.value })}
            placeholder={t('savedReplies.fLabelPlaceholder')}
          />
        </label>
        <label className="full">{t('savedReplies.fBody')}
          <textarea
            required
            maxLength={4000}
            rows={3}
            value={draft.body}
            onChange={(e) => setDraft({ ...draft, body: e.target.value })}
            placeholder={t('savedReplies.fBodyPlaceholder')}
          />
        </label>
        <div className="full">
          <button
            type="submit"
            className="btn btn-primary"
            disabled={busy}
            aria-busy={busy}
            aria-label={editingId ? t('savedReplies.save') : t('savedReplies.add')}
          >
            {busy ? '…' : editingId ? t('savedReplies.save') : t('savedReplies.add')}
          </button>
          {editingId && (
            <button type="button" className="btn btn-ghost" onClick={cancelEdit}>{t('common.cancel')}</button>
          )}
        </div>
      </form>

      {items.length === 0 ? (
        <p className="muted">{t('savedReplies.empty')}</p>
      ) : (
        <ul className="saved-replies-list" aria-label={t('savedReplies.title')}>
          {items.map((it) => (
            <li key={it.id} className={editingId === it.id ? 'editing' : ''}>
              <div>
                <strong>{it.label}</strong>
                <small className="muted">{it.body}</small>
              </div>
              <div>
                <button
                  type="button"
                  className="btn-link"
                  onClick={() => startEdit(it)}
                  aria-label={`${t('savedReplies.edit')}: ${it.label}`}
                >{t('savedReplies.edit')}</button>
                <button
                  type="button"
                  className="btn-link-danger"
                  onClick={() => remove(it.id)}
                  aria-label={`${t('savedReplies.delete')}: ${it.label}`}
                >{t('savedReplies.delete')}</button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// ProfilePanel lets the user edit their display name and avatar URL (PATCH /me).
// Email comes from the identity provider and is shown read-only.
function ProfilePanel() {
  const { t } = useT();
  const [form, setForm] = useState(null);
  const [email, setEmail] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    api
      .me()
      .then((u) => {
        setForm({ fullName: u.fullName || '', avatarUrl: u.avatarUrl || '' });
        setEmail(u.email || '');
      })
      .catch((e) => setError(e.message));
  }, []);

  async function submit(e) {
    e.preventDefault();
    if (!form.fullName.trim()) {
      setError(t('settings.nameRequired'));
      return;
    }
    setSaving(true);
    setSaved(false);
    setError(null);
    try {
      const u = await api.updateProfile({ fullName: form.fullName.trim(), avatarUrl: form.avatarUrl.trim() });
      setForm({ fullName: u.fullName || '', avatarUrl: u.avatarUrl || '' });
      setSaved(true);
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="profile-panel">
      <h2>{t('settings.profileTitle')}</h2>
      {error && <p className="error">{error}</p>}
      {!form ? (
        <p>{t('common.loading')}</p>
      ) : (
        <form className="profile-form" onSubmit={submit}>
          <label>
            {t('settings.email')}
            <input value={email} disabled />
          </label>
          <label>
            {t('settings.fullName')}
            <input value={form.fullName} onChange={(e) => setForm({ ...form, fullName: e.target.value })} required />
          </label>
          <label>
            {t('settings.avatarUrl')}
            <input value={form.avatarUrl} placeholder="https://…" onChange={(e) => setForm({ ...form, avatarUrl: e.target.value })} />
          </label>
          <div className="row-between">
            <button className="btn btn-primary" type="submit" disabled={saving}>
              {saving ? t('common.loading') : t('settings.saveProfile')}
            </button>
            {saved && <span className="muted">{t('settings.saved')}</span>}
          </div>
        </form>
      )}
    </section>
  );
}

// SecurityPanel lets the user enable two-factor authentication. 2FA is fully
// delegated to Keycloak: the button launches Keycloak's CONFIGURE_TOTP
// application-initiated action, so the OTP credential is set up and stored by
// the identity provider, then the user is returned here.
function SecurityPanel() {
  const { t } = useT();

  function setupTotp() {
    // keycloak-js forwards `action` as kc_action, triggering the AIA flow.
    keycloak.login({ action: 'CONFIGURE_TOTP', redirectUri: window.location.href });
  }

  return (
    <section className="security-panel">
      <h2>{t('security.title')}</h2>
      <p className="muted">{t('security.hint')}</p>
      <button className="btn btn-ghost" onClick={setupTotp}>{t('security.enable2fa')}</button>
    </section>
  );
}

// PrivacyPanel offers GDPR self-service: download a copy of your data, or
// permanently delete (anonymise) your account.
function PrivacyPanel() {
  const { t } = useT();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  async function exportData() {
    setError(null);
    setBusy(true);
    try {
      await api.exportMyData();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function deleteAccount() {
    if (!window.confirm(t('privacy.deleteConfirm'))) return;
    setError(null);
    setBusy(true);
    try {
      await api.deleteAccount();
      // The account is gone; end the session and return to the public site.
      keycloak.logout({ redirectUri: window.location.origin });
    } catch (e) {
      setError(e.message);
      setBusy(false);
    }
  }

  return (
    <section className="privacy-panel">
      <h2>{t('privacy.title')}</h2>
      <p className="muted">{t('privacy.hint')}</p>
      {error && <p className="error">{error}</p>}
      <div className="privacy-actions">
        <button className="btn btn-ghost" onClick={exportData} disabled={busy}>{t('privacy.export')}</button>
        <button className="btn-link-danger" onClick={deleteAccount} disabled={busy}>{t('privacy.delete')}</button>
      </div>
    </section>
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
