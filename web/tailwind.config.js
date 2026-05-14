/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        display: ['Orbitron', 'sans-serif'],
        body: ['Inter', 'PingFang SC', 'Microsoft YaHei', 'sans-serif'],
      },
      colors: {
        primary: {
          400: '#00F0FF',
          500: '#00D4E0',
          600: '#00A8B8',
        },
        accent: {
          400: '#A855F7',
          500: '#8A2BE2',
        },
        surface: {
          700: '#1a2332',
          800: '#121a27',
          900: '#0b1120',
          950: '#060a13',
        },
      },
    },
  },
  plugins: [],
}
