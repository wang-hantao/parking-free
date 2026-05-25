import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

// Vite config.
//
// `base` defaults to "/" — correct for custom domains (parkingfree.io),
// Cloudflare Pages, Vercel, Netlify, and GitHub Pages with a custom
// domain. For GitHub Pages on a project subpath
// (e.g. https://wang-hantao.github.io/parking-free/) set
// VITE_BASE_PATH=/parking-free/ at build time.
//
// `VITE_API_BASE_URL` is the URL of the parking-free Go server.
// In dev: http://localhost:8080. In prod: your deployed URL,
// e.g. https://api.parkingfree.io.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  return {
    base: env.VITE_BASE_PATH || "/",
    plugins: [react()],
    server: {
      port: 5173,
      strictPort: true,
    },
    build: {
      outDir: "dist",
      sourcemap: true,
      // Code-split the maps library and react-query into separate
      // chunks so the first paint can render the shell before the
      // heavy Google Maps script downloads.
      rollupOptions: {
        output: {
          manualChunks: {
            "vendor-react": ["react", "react-dom"],
            "vendor-maps": ["@vis.gl/react-google-maps"],
            "vendor-query": ["@tanstack/react-query"],
          },
        },
      },
    },
  };
});
