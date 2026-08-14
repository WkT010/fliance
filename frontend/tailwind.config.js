/** @type {import('tailwindcss').Config} */

// Resolve a theme token from CSS variables (defined in src/index.css).
// <alpha-value> keeps Tailwind opacity modifiers (bg-cta/15) working.
const token = (name) => `rgb(var(--c-${name}) / <alpha-value>)`;

export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // ── Fliance design tokens — CSS-variable driven so the whole
        //    app re-skins when <html data-theme> switches (dark default,
        //    [data-theme="light"] overrides every token in index.css) ──
        bg1: token('bg1'), // page primary background
        bg2: token('bg2'), // secondary background
        bg3: token('bg3'), // deepest background (footer / inputs wells)
        container: token('container'), // cards / inputs / hover rows
        line: token('line'), // hairline dividers
        pri: token('pri'), // primary text
        sec: token('sec'), // secondary text
        third: token('third'), // tertiary text
        cta: token('cta'), // CTA button background (teal)
        'cta-bright': token('cta-bright'), // brighter teal for dark-surface accents
        'cta-deep': token('cta-deep'), // teal hover state
        brand: token('brand'), // brand teal / badges
        'brand-soft': 'rgb(var(--c-brand) / 0.12)', // translucent brand tint
        'on-cta': token('on-cta'), // text on teal CTA surfaces
        'band-bg': token('band-bg'), // button bg on the teal CTA band
        'band-bg-hover': token('band-bg-hover'),
        'band-text': token('band-text'),
        gain: token('gain'), // up / buy (semantic)
        loss: token('loss'), // down / sell (semantic)
        'gain-bg': 'rgb(var(--c-gain) / 0.1)',
        'loss-bg': 'rgb(var(--c-loss) / 0.1)',
        // ── Legacy palette remapped onto the token ramp so existing pages
        //    pick up the new scheme automatically (ramp inverts per theme) ──
        nexa: {
          950: token('nexa-950'),
          900: token('nexa-900'),
          800: token('nexa-800'),
          700: token('nexa-700'),
          600: token('nexa-600'),
          500: token('nexa-500'),
          400: token('nexa-400'),
          300: token('nexa-300'),
          200: token('nexa-200'),
          100: token('nexa-100'),
          50: token('nexa-50'),
        },
        up: token('gain'),
        down: token('loss'),
        accent: token('accent'),
        warning: token('warning'),
      },
      fontFamily: {
        sans: ['IBM Plex Sans', 'Inter', 'system-ui', 'sans-serif'],
        mono: ['IBM Plex Mono', 'JetBrains Mono', 'ui-monospace', 'monospace'],
        // Self-hosted display face (see @font-face in src/index.css).
        // Reserved for display elements — headings, hero numbers,
        // big ticker prices, brand wordmark. Never for body/forms.
        condensed: ['Barlow Condensed', 'IBM Plex Sans', 'Inter', 'system-ui', 'sans-serif'],
      },
    },
  },
  plugins: [],
}
