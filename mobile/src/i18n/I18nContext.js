import { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { NativeModules, Platform } from 'react-native';
import * as Storage from '../auth/storage';
import { translations } from './translations';

// I18nContext mirrors the web app's i18n provider (web/src/i18n/I18nContext.jsx)
// so the same key namespace and the same chain-fallback semantics apply on
// mobile. Keep this file in lockstep with the web version when adding locales —
// the dictionaries themselves live in ./translations.js and are shared by key
// name with the web bundle so a single audit covers both platforms.
const I18nContext = createContext(null);
const STORAGE_KEY = 'airhost.lang';

// LOCALES enumerates every supported locale code together with the native
// label the switcher displays. Order is the order the switcher renders them.
// Matches the web's LOCALES list 1:1 so the option set is identical on both
// platforms.
export const LOCALES = [
  { code: 'en', label: 'English' },
  { code: 'pt', label: 'Português' },
  { code: 'pt-BR', label: 'Português (BR)' },
  { code: 'es', label: 'Español' },
];

// FALLBACK_CHAIN defines the per-locale lookup order. A key missing in the
// active locale is searched up the chain, ending at English. pt-BR borrows
// from pt before falling through to en, which keeps the BR dictionary small
// (only divergent vocabulary is overridden). es falls through directly to en
// since there is no Latin-American Spanish variant configured.
const FALLBACK_CHAIN = {
  en: ['en'],
  pt: ['pt', 'en'],
  'pt-BR': ['pt-BR', 'pt', 'en'],
  es: ['es', 'en'],
};

const SUPPORTED = new Set(LOCALES.map((l) => l.code));

// readDeviceLocale returns the OS-reported locale tag (e.g. "pt-BR", "en_US")
// without throwing on web or unusual device configurations. We avoid the
// expo-localization package so this works in a managed Expo project without
// adding a native dependency — RN already surfaces the locale through the
// platform-specific NativeModules, and on web we fall back to navigator.
function readDeviceLocale() {
  try {
    if (Platform.OS === 'ios') {
      const s = NativeModules.SettingsManager?.settings;
      return s?.AppleLocale || (Array.isArray(s?.AppleLanguages) && s.AppleLanguages[0]) || '';
    }
    if (Platform.OS === 'android') {
      return NativeModules.I18nManager?.localeIdentifier || '';
    }
    // Expo web (and bare web fallback): mirror the web app's detection.
    if (typeof navigator !== 'undefined') return navigator.language || '';
  } catch {
    /* swallow — readDeviceLocale must never crash the app */
  }
  return '';
}

// normaliseLocale maps a raw OS locale tag to one of our supported codes.
// Identical to the web's branching: pt-BR / pt-br stays as pt-BR; any other
// pt-* collapses to pt; any es-* collapses to es; everything else is English.
function normaliseLocale(raw) {
  const tag = String(raw || '').toLowerCase().replace('_', '-');
  if (!tag) return 'en';
  if (tag === 'pt-br' || tag.startsWith('pt-br')) return 'pt-BR';
  if (tag.startsWith('pt')) return 'pt';
  if (tag.startsWith('es')) return 'es';
  return 'en';
}

export function I18nProvider({ children }) {
  // Start with a sensible synchronous guess from the device locale so the
  // first render doesn't flash English. The async load from secure storage
  // below then overrides if the user has previously picked a locale.
  const [lang, setLangState] = useState(() => normaliseLocale(readDeviceLocale()));
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    Storage.getItem(STORAGE_KEY)
      .then((stored) => {
        if (cancelled) return;
        if (stored && SUPPORTED.has(stored)) setLangState(stored);
      })
      .catch(() => {
        /* persistence is best-effort — fall back to the device-locale guess */
      })
      .finally(() => {
        if (!cancelled) setReady(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const setLang = useCallback((l) => {
    if (!SUPPORTED.has(l)) return; // ignore garbage from the switcher
    setLangState(l);
    // Persist asynchronously; failure here doesn't matter — the new value is
    // already live in state and the next startup will just re-detect from
    // the device locale.
    Storage.setItem(STORAGE_KEY, l).catch(() => {});
  }, []);

  // t walks the locale's fallback chain (e.g. pt-BR → pt → en) and returns
  // the first hit. Returns the raw key as the last resort so missing strings
  // stay visible during development. Interpolates {placeholders} from vars.
  const t = useCallback(
    (key, vars) => {
      const chain = FALLBACK_CHAIN[lang] || FALLBACK_CHAIN.en;
      let msg = key;
      for (const code of chain) {
        const dict = translations[code];
        if (dict && Object.prototype.hasOwnProperty.call(dict, key)) {
          msg = dict[key];
          break;
        }
      }
      if (vars) {
        for (const [k, v] of Object.entries(vars)) {
          // String#replaceAll exists in Hermes (RN 0.71+); we're on 0.74.
          msg = msg.split(`{${k}}`).join(String(v));
        }
      }
      return msg;
    },
    [lang],
  );

  return (
    <I18nContext.Provider value={{ t, lang, setLang, ready, locales: LOCALES }}>
      {children}
    </I18nContext.Provider>
  );
}

// useT is the primary hook — name matches the web's so screens that work on
// both platforms can share import paths after a shim.
export function useT() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error('useT must be used within I18nProvider');
  return ctx;
}
