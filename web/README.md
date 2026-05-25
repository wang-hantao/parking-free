# parking-free-web

React + Vite + TypeScript frontend for the parking-free Go backend.

A single-page app that asks for your location, shows it on a Google
Map, calls the `/allowed` endpoint, and renders the verdict with
pricing and payment-app deep-links.

## Development

```bash
cd web
cp .env.example .env.local
# Edit .env.local — set VITE_API_BASE_URL to your local server
# (http://localhost:8080) and VITE_GOOGLE_MAPS_API_KEY to a Maps
# JavaScript API key.

npm install
npm run dev
```

Then open http://localhost:5173.

You also need the backend running:

```bash
# In another terminal, from repo root
make docker-up
make migrate
make seed       # optional, gives you a demo zone in central Stockholm
make run
```

The backend listens on `:8080` and now allows CORS for the dev origin
when `CORS_ALLOWED_ORIGIN=http://localhost:5173` is set in the server's
environment (see `.env.example` at the repo root).

## Google Maps API key

1. Open https://console.cloud.google.com/google/maps-apis/credentials
2. Create a project if you don't have one.
3. Enable the **Maps JavaScript API**.
4. Create an API key.
5. Restrict the key to your domain(s) — `localhost:5173` for dev,
   plus your production domain.
6. Put it in `.env.local` as `VITE_GOOGLE_MAPS_API_KEY`.

The map renders a fallback "Map unavailable" panel when no key is set,
so the rest of the app stays usable in CI and for collaborators
without a key.

## Build and preview

```bash
npm run build      # produces dist/
npm run preview    # serves dist/ on http://localhost:4173
```

`npm run typecheck` runs `tsc --noEmit` for CI.

## Deployment

### Cloudflare Pages (recommended)

If you registered `parkingfree.io` at Cloudflare, this is the natural
fit — same vendor, free, custom domain in one click.

1. Push the repo to GitHub.
2. In the Cloudflare dashboard: **Workers & Pages → Create application
   → Pages → Connect to Git**.
3. Pick the repo, set:
   - Production branch: `master`
   - Framework preset: `Vite`
   - Build command: `npm install && npm run build`
   - Build output directory: `web/dist`
   - Root directory: `web`
4. Environment variables:
   - `VITE_API_BASE_URL` = `https://api.parkingfree.io` (or wherever
     you deployed the backend)
   - `VITE_GOOGLE_MAPS_API_KEY` = your key
5. Add a custom domain (e.g. `parkingfree.io`) on the Pages project's
   **Custom domains** tab.

### Vercel / Netlify

Same idea: Root directory `web`, build `npm run build`, output
`dist`. Both auto-detect Vite.

### GitHub Pages

Works but a bit more friction. If you publish to a project page
(`https://yourname.github.io/parking-free/`), set
`VITE_BASE_PATH=/parking-free/` in the build environment. A simple
GitHub Action:

```yaml
# .github/workflows/pages.yml
name: deploy-web
on:
  push:
    branches: [master]
permissions:
  contents: read
  pages: write
  id-token: write
jobs:
  build:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: web
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 20
      - run: npm ci
      - run: npm run build
        env:
          VITE_API_BASE_URL: https://api.parkingfree.io
          VITE_GOOGLE_MAPS_API_KEY: ${{ secrets.GOOGLE_MAPS_API_KEY }}
          VITE_BASE_PATH: /parking-free/
      - uses: actions/upload-pages-artifact@v3
        with:
          path: web/dist
  deploy:
    needs: build
    runs-on: ubuntu-latest
    environment: github-pages
    steps:
      - uses: actions/deploy-pages@v4
```

## Structure

```
web/
├── src/
│   ├── App.tsx                  # top-level flow
│   ├── main.tsx                 # React + QueryClient bootstrap
│   ├── index.css                # Tailwind directives
│   ├── components/
│   │   ├── LocationButton.tsx   # "Use my location" trigger
│   │   ├── MapView.tsx          # Google Map with the user's pin
│   │   ├── VerdictCard.tsx      # structured verdict display
│   │   └── PaymentLinks.tsx     # operator deep-link buttons
│   ├── hooks/
│   │   ├── useGeolocation.ts    # navigator.geolocation wrapper
│   │   └── useParkingVerdict.ts # React Query around fetchVerdict
│   ├── lib/
│   │   ├── api.ts               # typed /allowed client
│   │   └── env.ts               # env validation
│   └── types/
│       └── verdict.ts           # mirrors Go domain types
├── public/
│   └── favicon.svg
├── index.html
├── package.json
├── tsconfig.json
├── tsconfig.node.json
├── tailwind.config.js
├── postcss.config.js
└── vite.config.ts
```

## Known limitations (scaffolding)

- No real auth: plate is held in component state. Real signup/permits
  is future work.
- No offline support / installable PWA: future work.
- Payment deep-links use operator web fallbacks until the backend's
  `OperatorOption.app_url` is fully wired with plate substitution.
- No error boundaries yet — a single thrown render crashes the tree.
