/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        display: ['"Cabinet Grotesk"', '"PingFang SC"', '"HarmonyOS Sans SC"', '"Microsoft YaHei"', 'system-ui', 'sans-serif'],
        body: ['Geist', 'system-ui', '"PingFang SC"', '"HarmonyOS Sans SC"', '"Microsoft YaHei"', 'sans-serif'],
        mono: ['"JetBrains Mono"', '"Fira Code"', 'monospace'],
      },
      colors: {
        // ── Brand: Aurora Violet（对齐 Logo 的紫色渐变） ──
        brand: {
          DEFAULT: '#8b5cf6',
          50:  '#f5f3ff',
          100: '#ede9fe',
          200: '#ddd6fe',
          300: '#c4b5fd',
          400: '#a78bfa',
          500: '#8b5cf6',
          600: '#7c3aed',
          700: '#6d28d9',
          800: '#5b21b6',
          900: '#4c1d95',
          950: '#2e1065',
        },
        // ── Gold: 星光金（评分 / 精选 / 会员时刻） ──
        gold: {
          DEFAULT: '#f0b34e',
          300: '#fbd38d',
          400: '#f5c265',
          500: '#f0b34e',
          600: '#d99a2b',
          700: '#b47a1c',
        },
        // ── Sage → Pulse Cyan（Logo 上的青色光点，信息态点缀） ──
        sage: {
          DEFAULT: '#22d3ee',
          50:  '#ecfeff',
          100: '#cffafe',
          200: '#a5f3fc',
          300: '#67e8f9',
          400: '#22d3ee',
          500: '#06b6d4',
          600: '#0891b2',
          700: '#0e7490',
          800: '#155e75',
          950: '#083344',
        },
        // ── Ink: 文本层级（浅色主题） ──
        ink: {
          DEFAULT: '#121218',
          50:  '#6b6b76',
          100: '#4b4b55',
          200: '#37373f',
          300: '#23232b',
          400: '#121218',
          500: '#121218',
          600: '#0c0c11',
          700: '#08080c',
          800: '#000000',
          900: '#000000',
        },
        // ── Sand: 中性灰阶 ──
        sand: {
          DEFAULT: '#f7f7fa',
          50:  '#ffffff',
          100: '#f7f7fa',
          200: '#f0f0f4',
          300: '#e4e4ea',
          400: '#c9c9d4',
          500: '#9c9ca8',
          600: '#6b6b76',
          700: '#4b4b55',
          800: '#37373f',
          900: '#23232b',
        },
        // ── Surface: 兼容映射 ──
        surface: {
          DEFAULT: '#ffffff',
          50:  '#ffffff',
          100: '#f7f7fa',
          200: '#f0f0f4',
          300: '#e4e4ea',
          400: '#ffffff',
          500: '#ffffff',
          600: '#23232b',
          700: '#121218',
          800: '#0c0c11',
          900: '#07070c',
          950: '#000000',
        },
        // 兼容别名
        primary: {
          400: '#a78bfa',
          500: '#8b5cf6',
          600: '#7c3aed',
        },
        accent: {
          400: '#22d3ee',
          500: '#06b6d4',
        },
        cream: {
          DEFAULT: '#f7f7fa',
          50:  '#ffffff',
          100: '#f7f7fa',
          200: '#f0f0f4',
          300: '#e4e4ea',
          400: '#9c9ca8',
          500: '#6b6b76',
          600: '#4b4b55',
          700: '#23232b',
          800: '#121218',
          900: '#000000',
        },
      },
      fontSize: {
        '2xs': ['0.625rem', { lineHeight: '0.875rem' }],
      },
      spacing: {
        '18': '4.5rem',
        '88': '22rem',
      },
      transitionDuration: {
        '400': '400ms',
      },
      transitionTimingFunction: {
        'spring': 'cubic-bezier(0.34, 1.56, 0.64, 1)',
        'smooth': 'cubic-bezier(0.21, 0.47, 0.32, 0.98)',
      },
      boxShadow: {
        'card': '0 1px 2px rgba(0,0,0,0.04)',
        'card-hover': '0 12px 32px rgba(0,0,0,0.08), 0 2px 8px rgba(0,0,0,0.04)',
        'elevated': '0 24px 60px rgba(0,0,0,0.12), 0 4px 16px rgba(0,0,0,0.06)',
        'sidebar': '1px 0 0 rgba(255,255,255,0.04)',
        'glow': '0 0 24px rgba(139,92,246,0.35), 0 4px 16px rgba(139,92,246,0.2)',
        'glow-sm': '0 0 12px rgba(139,92,246,0.3)',
        'glow-lg': '0 0 48px rgba(139,92,246,0.4), 0 8px 32px rgba(139,92,246,0.25)',
        'glow-gold': '0 0 20px rgba(240,179,78,0.3)',
        'poster': '0 8px 24px rgba(0,0,0,0.35)',
        'poster-hover': '0 16px 48px rgba(0,0,0,0.5), 0 0 32px rgba(139,92,246,0.28)',
      },
      keyframes: {
        shimmer: {
          '100%': { transform: 'translateX(100%)' },
        },
        'fade-rise': {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        'glow-drift': {
          '0%, 100%': { transform: 'translate(0, 0) scale(1)' },
          '50%': { transform: 'translate(4%, 6%) scale(1.08)' },
        },
      },
      animation: {
        shimmer: 'shimmer 1.8s infinite',
        'fade-rise': 'fade-rise 0.5s cubic-bezier(0.21, 0.47, 0.32, 0.98) both',
        'glow-drift': 'glow-drift 14s ease-in-out infinite',
      },
    },
  },
  plugins: [],
}
