import { createContext, useContext, useState, useCallback, useEffect } from 'react';

// ConsentContext models the user's cookie/tracking preferences. Per GDPR
// (ePrivacy + Art. 7), the user must:
//   - have NOT consented by default (no implied consent)
//   - be able to refuse non-essential cookies as easily as accepting them
//   - be able to withdraw consent at any time
//
// Categories:
//   - necessary: session, auth, CSRF — always on, locked in the UI
//   - analytics: page-view / funnel telemetry (Plausible/PostHog)
//   - marketing: third-party retargeting / ad pixels — currently unused
//     but exposed so we can light it up without rewiring consent storage
//
// State persists in localStorage (NOT a cookie, deliberately — using a
// cookie to store consent would itself need consent, which is circular).

const STORAGE_KEY = 'airhost.consent';
const ConsentContext = createContext(null);

// CONSENT_VERSION lets us re-prompt the user when the cookie policy
// materially changes. Bump it whenever the categories or their meaning
// changes; old persisted decisions with a lower version are ignored.
const CONSENT_VERSION = 1;

// defaultConsent is the pre-decision state — necessary always on, every
// other category off, no decision recorded. The banner reads `decided`
// to know whether to show itself.
const defaultConsent = {
  decided: false,
  necessary: true,
  analytics: false,
  marketing: false,
  decidedAt: '',
  version: CONSENT_VERSION,
};

// readPersisted hydrates the saved decision (if any) and validates its
// shape — corrupt entries fall back to defaultConsent so the banner
// re-prompts rather than silently honouring garbage.
function readPersisted() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return defaultConsent;
    const parsed = JSON.parse(raw);
    if (
      parsed &&
      typeof parsed === 'object' &&
      parsed.version === CONSENT_VERSION &&
      typeof parsed.decided === 'boolean'
    ) {
      // Necessary is always-on regardless of what's persisted —
      // belt-and-braces against a future bug or hand-edited localStorage.
      return { ...parsed, necessary: true };
    }
  } catch {
    // localStorage might be blocked (private browsing, embedded
    // contexts). Fall through to defaults — the banner re-prompts.
  }
  return defaultConsent;
}

function persist(next) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // ignore — see readPersisted for context. Worst case the user
    // sees the banner on every visit, which is annoying but not
    // a correctness issue.
  }
}

export function ConsentProvider({ children }) {
  const [consent, setConsent] = useState(readPersisted);

  // When a non-default consent shape lands (e.g. after acceptAll), persist
  // it. The effect runs on every mount so the in-memory copy and the
  // stored copy stay aligned even if persistence failed initially.
  useEffect(() => {
    if (consent.decided) persist(consent);
  }, [consent]);

  const acceptAll = useCallback(() => {
    setConsent({
      decided: true,
      necessary: true,
      analytics: true,
      marketing: true,
      decidedAt: new Date().toISOString(),
      version: CONSENT_VERSION,
    });
  }, []);

  // rejectNonEssential is the "Reject" button — necessary stays on
  // (it has to, to log the user in at all), everything else off.
  // Critical for GDPR compliance: reject must be as one-click as accept.
  const rejectNonEssential = useCallback(() => {
    setConsent({
      decided: true,
      necessary: true,
      analytics: false,
      marketing: false,
      decidedAt: new Date().toISOString(),
      version: CONSENT_VERSION,
    });
  }, []);

  // savePartial commits a customised picker state. The dialog passes a
  // partial { analytics, marketing }; necessary is force-set to true
  // even if the caller dropped it.
  const savePartial = useCallback((picks) => {
    setConsent({
      decided: true,
      necessary: true,
      analytics: !!picks.analytics,
      marketing: !!picks.marketing,
      decidedAt: new Date().toISOString(),
      version: CONSENT_VERSION,
    });
  }, []);

  // reopen wipes the decision so the banner shows again. Used by the
  // "Change preferences" link in Settings, and by debug tooling.
  const reopen = useCallback(() => {
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch {
      // ignore — the in-memory reset below is what actually drives
      // the banner re-render.
    }
    setConsent(defaultConsent);
  }, []);

  return (
    <ConsentContext.Provider value={{ consent, acceptAll, rejectNonEssential, savePartial, reopen }}>
      {children}
    </ConsentContext.Provider>
  );
}

export function useConsent() {
  const ctx = useContext(ConsentContext);
  if (!ctx) throw new Error('useConsent must be used within ConsentProvider');
  return ctx;
}
