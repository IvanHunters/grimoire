/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        claude: '#8b5cf6',
      },
      width: {
        'chat-panel': '600px',
      },
    },
  },
  plugins: [],
}
