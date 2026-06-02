import { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { translations } from './translations';

const I18nContext = createContext(null);
const STORAGE_KEY = 'airhost.lang';

// LOCALES enumerates every supported locale code together with the native
// label the switcher displays. The order is the order the switcher renders
// them in. Add a new entry here when a new locale lands — the rest of the
// app (detectLang, the chain fallback) is data-driven off this list.
export const LOCALES = [
  { code: 'en', label: 'English' },
  { code: 'pt', label: 'Português' },
  { code: 'pt-BR', label: 'Português (BR)' },
  { code: 'es', label: 'Español' },
];

// FALLBACK_CHAIN defines the per-locale lookup order. A key missing in the
// active locale is searched up the chain, ending at English. This keeps
// regional locales (pt-BR) and partial locales (es) usable without
// requiring 100% translation coverage — untranslated keys gracefully
// fall back instead of rendering raw key paths.
const FALLBACK_CHAIN = {
  en: ['en'],
  pt: ['pt', 'en'],
  'pt-BR': ['pt-BR', 'pt', 'en'],
  es: ['es', 'en'],
};

const SUPPORTED = new Set(LOCALES.map((l) => l.code));

// detectLang reads a previously chosen locale from localStorage, then falls
// back to the browser's navigator.language. The browser hint is normalised:
// "pt-BR"/"pt-br" → pt-BR, anything else starting with "pt" → pt,
// "es-*" → es, otherwise English.
function detectLang() {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored && SUPPORTED.has(stored)) return stored;
  const nav = (navigator.language || 'en').toLowerCase();
  if (nav === 'pt-br' || nav.startsWith('pt-br')) return 'pt-BR';
  if (nav.startsWith('pt')) return 'pt';
  if (nav.startsWith('es')) return 'es';
  return 'en';
}

export function I18nProvider({ children }) {
  const [lang, setLangState] = useState(detectLang);

  const setLang = useCallback((l) => {
    if (!SUPPORTED.has(l)) return; // ignore garbage from the switcher
    localStorage.setItem(STORAGE_KEY, l);
    setLangState(l);
  }, []);

  // Keep the document language in sync so assistive tech announces content in
  // the right language and the browser applies correct hyphenation/quotes.
  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  // t looks up a key by walking the locale's fallback chain (e.g.
  // pt-BR → pt → en). Returns the raw key as the last resort so missing
  // strings stay visible in dev. Interpolates {placeholders} from vars.
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
          msg = msg.replaceAll(`{${k}}`, String(v));
        }
      }
      return msg;
    },
    [lang],
  );

  return <I18nContext.Provider value={{ t, lang, setLang, locales: LOCALES }}>{children}</I18nContext.Provider>;
}

export function useT() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error('useT must be used within I18nProvider');
  return ctx;
}
