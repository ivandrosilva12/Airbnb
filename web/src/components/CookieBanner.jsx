import { useState } from 'react';
import { useConsent } from '../context/ConsentContext';
import { useT } from '../i18n/I18nContext';

// CookieBanner is the bottom-of-page consent prompt that fires when the
// user has not yet recorded a decision. Three actions:
//
//   Accept all       → analytics + marketing on
//   Reject           → necessary only (GDPR-required equivalence with
//                      Accept — same prominence, same click count)
//   Customize        → expands an inline picker with per-category
//                      toggles + a Save button
//
// The component renders nothing once a decision is recorded. The user
// can revisit it from Settings → Cookie preferences (which calls
// reopen() on the consent context).
export default function CookieBanner() {
  const { t } = useT();
  const { consent, acceptAll, rejectNonEssential, savePartial } = useConsent();
  const [customizing, setCustomizing] = useState(false);
  // Local draft for the picker so checkbox toggles don't immediately
  // commit. The user has to press Save to apply, matching the modal
  // pattern used elsewhere in the app.
  const [draft, setDraft] = useState({ analytics: false, marketing: false });

  if (consent.decided) return null;

  function save() {
    savePartial(draft);
  }

  return (
    <aside
      className="cookie-banner"
      role="dialog"
      aria-modal="false"
      aria-labelledby="cookie-banner-title"
      aria-describedby="cookie-banner-body"
    >
      <div className="cookie-banner-inner">
        <h2 id="cookie-banner-title">{t('consent.title')}</h2>
        <p id="cookie-banner-body">{t('consent.body')}</p>

        {!customizing ? (
          <div className="cookie-banner-actions">
            {/* Order matters for GDPR: Reject is rendered with the same
                visual prominence as Accept (same button style, same
                position cluster) — regulators flag dark patterns that
                make Reject harder to find than Accept. */}
            <button
              type="button"
              className="btn btn-ghost"
              onClick={rejectNonEssential}
            >
              {t('consent.reject')}
            </button>
            <button
              type="button"
              className="btn btn-ghost"
              onClick={() => setCustomizing(true)}
            >
              {t('consent.customize')}
            </button>
            <button
              type="button"
              className="btn btn-primary"
              onClick={acceptAll}
            >
              {t('consent.acceptAll')}
            </button>
          </div>
        ) : (
          <div className="cookie-customize">
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
            <div className="cookie-banner-actions">
              <button
                type="button"
                className="btn btn-ghost"
                onClick={() => setCustomizing(false)}
              >
                {t('common.cancel')}
              </button>
              <button type="button" className="btn btn-primary" onClick={save}>
                {t('consent.save')}
              </button>
            </div>
          </div>
        )}
      </div>
    </aside>
  );
}
