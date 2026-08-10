/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // ── Fliance design tokens (measured from the reference exchange UI) ──
        bg1: '#202630', // page primary background
        bg2: '#181A20', // secondary background
        bg3: '#0B0E11', // deepest background (footer / inputs wells)
        container: '#29313D', // cards / inputs / hover rows
        line: '#333B47', // hairline dividers
        pri: '#EAECEF', // primary text
        sec: '#929AA5', // secondary text
        third: '#707A8A', // tertiary text
        cta: '#FCD535', // CTA button background
        brand: '#F0B90B', // brand yellow / badges
        gain: '#2EBD85', // up / buy
        loss: '#F6465D', // down / sell
        'gain-bg': 'rgba(46, 189, 133, 0.1)',
        'loss-bg': 'rgba(246, 70, 93, 0.1)',
        // ── Legacy palette remapped onto the token ramp so existing pages
        //    pick up the new scheme automatically ──
        nexa: {
          950: '#0B0E11',
          900: '#181A20',
          800: '#202630',
          700: '#2B3139',
          600: '#333B47',
          500: '#5E6675',
          400: '#707A8A',
          300: '#929AA5',
          200: '#C9CDD4',
          100: '#EAECEF',
          50: '#FFFFFF',
        },
        up: '#2EBD85',
        down: '#F6465D',
        accent: '#FCD535',
      },
      fontFamily: {
        sans: ['IBM Plex Sans', 'Inter', 'system-ui', 'sans-serif'],
        mono: ['IBM Plex Mono', 'JetBrains Mono', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [],
}
