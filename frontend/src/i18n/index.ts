import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import zh from './zh.json';
import en from './en.json';

// Persist + restore the user's language preference. Defaults to Chinese (zh)
// because the primary user is Chinese; English speakers can switch from the
// header and their choice is remembered across sessions.
const savedLang = (typeof localStorage !== 'undefined' && localStorage.getItem('nexa-lang')) || 'zh';

void i18n.use(initReactI18next).init({
  resources: {
    zh: { translation: zh },
    en: { translation: en },
  },
  lng: savedLang,
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
  returnEmptyString: false,
});

export const LANGS = [
  { code: 'zh', label: '中文' },
  { code: 'en', label: 'English' },
] as const;

export function setLanguage(code: 'zh' | 'en') {
  void i18n.changeLanguage(code);
  if (typeof localStorage !== 'undefined') localStorage.setItem('nexa-lang', code);
}

export default i18n;
