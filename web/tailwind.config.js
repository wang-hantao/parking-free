/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Verdict tones — kept semantic so the verdict UI can pull
        // whichever colour applies without re-thinking palette later.
        allowed: {
          50: "#f0fdf4",
          500: "#22c55e",
          700: "#15803d",
        },
        forbidden: {
          50: "#fef2f2",
          500: "#ef4444",
          700: "#b91c1c",
        },
        pending: {
          50: "#fffbeb",
          500: "#f59e0b",
          700: "#b45309",
        },
      },
    },
  },
  plugins: [],
};
