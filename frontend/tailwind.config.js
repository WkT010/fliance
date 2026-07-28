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
        nexa: {
          950: '#0b0e11',
          900: '#111418',
          800: '#151a21',
          700: '#1e242c',
          600: '#2a313c',
          500: '#3a4654',
          400: '#5d6b7a',
          300: '#8b97a8',
          100: '#d4dbe3',
          50: '#f0f3f6',
        },
        up: '#0ecb81',
        down: '#f6465d',
        accent: '#f0b90b',
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [],
}
