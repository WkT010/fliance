import { create } from 'zustand';

export type Theme = 'dark' | 'light';

export const THEME_STORAGE_KEY = 'fliance-theme';

function readStoredTheme(): Theme {
  try {
    return localStorage.getItem(THEME_STORAGE_KEY) === 'light' ? 'light' : 'dark';
  } catch {
    return 'dark';
  }
}

function applyTheme(theme: Theme) {
  document.documentElement.setAttribute('data-theme', theme);
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    /* private mode / storage disabled — theme still applies for the session */
  }
}

interface ThemeStore {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  toggleTheme: () => void;
}

export const useThemeStore = create<ThemeStore>((set, get) => ({
  theme: readStoredTheme(),
  setTheme: (theme) => {
    applyTheme(theme);
    set({ theme });
  },
  toggleTheme: () => {
    get().setTheme(get().theme === 'dark' ? 'light' : 'dark');
  },
}));

// Apply the persisted theme as soon as this module is imported (main.tsx
// imports it before rendering). Together with the inline bootstrap script
// in index.html this prevents any dark→light flash on first paint.
applyTheme(useThemeStore.getState().theme);
