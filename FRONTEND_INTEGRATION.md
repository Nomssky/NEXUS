# NEXUS Frontend UI/UX Replacement — Integration Documentation

## Summary

The frontend UI/UX has been replaced with the provided Penpot design (`FirstRun` — first-run onboarding screen) and integrated with the NEXUS backend API.

- **Before:** React + Vite app with `OfficeLayout` virtual office shown immediately after login
- **After:** After authentication, users see the new `FirstRun` onboarding screen; after creating a business via `POST /api/v1/businesses`, they transition to the `OfficeLayout` virtual office

## Changes

### 1. New component: `src/components/FirstRun/FirstRun.tsx`

Pixel-for-pixel reproduction of the provided HTML design:
- 1440×1024 dark canvas (`#080b11`)
- Left panel: NEXUS wordmark, "personal AI operating system", Welcome/Running headings, 4-step progress, 3 info cards
- Right panel: Business name input, Primary objective input, Divisions chips (Research/Business/Media/Development), "Create business" button, "Load a sample" link, "Step 1 of 4"
- Form state, loading state, error state
- On success calls `onBusinessCreated` to transition to the office layout

### 2. New styles: `src/components/FirstRun/FirstRun.module.css`

CSS module with:
- Google Fonts imports: JetBrains Mono, Inter, Space Grotesk (matching the original embedded base64 fonts)
- Exact colors, typography, spacing, border-radius, and absolute positioning from the HTML
- Responsive fallback for screens narrower than 1440px

### 3. API client: `src/api/client.ts`

Added `createBusiness()` method:
```ts
async createBusiness(request: CreateBusinessRequest): Promise<Business>
```
Calls `POST /api/v1/businesses` (membership-free bootstrap endpoint).

### 4. Types: `src/types/api.ts`

Added:
```ts
export interface Business {
  entity_id: string;
  name: string;
  owner_identity_id: string;
  settings?: unknown;
  metadata?: Record<string, string>;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface CreateBusinessRequest {
  name: string;
  owner_identity_id: string;
  entity_id?: string;
  settings?: unknown;
  metadata?: Record<string, string>;
}
```

### 5. App shell: `src/App.tsx`

- Added `businessId` and `onboardingComplete` state (restored from `localStorage` `nexus-auth`)
- After auth: if `!onboardingComplete`, render `<FirstRun>`; otherwise render `<OfficeLayout>`
- `FirstRun.onBusinessCreated` persists `businessId` to `localStorage` and switches to the office layout

### 6. Documentation

- `nexus-app/src/components/FirstRun/README.md` — FirstRun component docs
- `FRONTEND_INTEGRATION.md` (this file) — end-to-end integration guide

## Backend Endpoints Used

| Method | Path | Handler | Purpose |
|--------|------|---------|---------|
| `GET` | `/api/v1/businesses` | `handleListBusinesses` | Validate credentials at login (returns memberships) |
| `POST` | `/api/v1/businesses` | `handleCreateBusiness` | Create first business (bootstrap, membership-free) |
| `GET` | `/health` | `handleHealth` | Health check |

Backend files:
- `internal/gateway/org.go` — `handleCreateBusiness`, `handleListBusinesses`
- `internal/gateway/server.go` — route registration (`POST /api/v1/businesses`, `GET /api/v1/businesses`)

## How to Run

### Prerequisites

- Go backend: `cd /home/pc/NEXUS && go run ./cmd/nexus`
- Frontend: `cd /home/pc/NEXUS/nexus-app && npm run dev`

The Vite dev server proxies `/api` to `http://localhost:8081` (gateway) and `/health` to `http://localhost:8080` (health server).

### Steps

1. Start the backend:
   ```bash
   cd /home/pc/NEXUS
   go run ./cmd/nexus
   ```
   Ensure `NEXUS_BOOTSTRAP_CREDENTIAL` is set (used for the bootstrap identity).

2. Start the frontend:
   ```bash
   cd /home/pc/NEXUS/nexus-app
   npm run dev
   ```
   Opens at `http://localhost:3000`.

3. Log in with the bootstrap actor (`nx:human:bootstrap`) and the bootstrap credential.

4. The `FirstRun` screen appears. Fill in (or keep defaults):
   - Business name: `AI Agency`
   - Objective: `Win three enterprise deals this quarter`
   - Divisions: `Research` (selected by default)

5. Click **Create business**. The frontend calls `POST /api/v1/businesses`.

6. On success, the app transitions to `OfficeLayout` (virtual office) with the new `businessId`.

7. Refreshing the page skips onboarding (businessId restored from `localStorage`).

## Type Check

```bash
cd /home/pc/NEXUS/nexus-app
npx tsc --noEmit
```
Passes cleanly.

## Build

```bash
cd /home/pc/NEXUS/nexus-app
npm run build
```

Note: `vite build` has a pre-existing unrelated error (`./OfficeTable` module not found in `OfficeFloor.tsx`) that exists on the main branch and is not caused by this change.

## Testing

```bash
cd /home/pc/NEXUS/nexus-app
npm test
```

Existing tests (`authStorage.test.ts`, `localSession.test.ts`, API client tests) continue to pass.

## Reversibility

To revert to the previous behavior (office layout immediately after login), replace the `AppContent` onboarding branch with the original `OfficeLayout` rendering and remove the `FirstRun` import. The backend `POST /api/v1/businesses` endpoint remains available for programmatic business creation.