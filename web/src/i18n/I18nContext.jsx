import { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { translations } from './translations';

const I18nContext = createContext(null);
const STORAGE_KEY = 'airhost.lang';

function detectLang() {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'pt' || stored === 'en') return stored;
  return (navigator.language || 'en').toLowerCase().startsWith('pt') ? 'pt' : 'en';
}

export function I18nProvider({ children }) {
  const [lang, setLangState] = useState(detectLang);

  const setLang = useCallback((l) => {
    localStorage.setItem(STORAGE_KEY, l);
    setLangState(l);
  }, []);

  // Keep the document language in sync so assistive tech announces content in
  // the right language and the browser applies correct hyphenation/quotes.
  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  // t looks up a key for the current language, falling back to English then the
  // key itself, and interpolates {placeholders} from vars.
  const t = useCallback(
    (key, vars) => {
      const dict = translations[lang] || translations.en;
      let msg = dict[key] ?? translations.en[key] ?? key;
      if (vars) {
        for (const [k, v] of Object.entries(vars)) {
          msg = msg.replaceAll(`{${k}}`, String(v));
        }
      }
      return msg;
    },
    [lang],
  );

  return <I18nContext.Provider value={{ t, lang, setLang }}>{children}</I18nContext.Provider>;
}

export function useT() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error('useT must be used within I18nProvider');
  return ctx;
}
