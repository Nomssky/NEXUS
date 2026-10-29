# FirstRun — NEXUS First-Run Onboarding

## Overview

`FirstRun` replaces the previous login-and-office flow with the provided Penpot design: a dark-themed, fixed-canvas first-run onboarding screen that lets a user create their first business through the backend API.

After successful authentication, if no business has been created yet, the user sees this screen. On successful business creation, the app transitions to the virtual office layout (`OfficeLayout`).

## Files

| File | Purpose |
|------|---------|
| `FirstRun.tsx` | React component: layout, form state, API integration |
| `FirstRun.module.css` | CSS module: exact design reproduction (fonts, colors, spacing, absolute positioning) |
| `README.md` | This file |

## Design

The screen is a 1440×1024 dark canvas (`#080b11`) with two panels:

**Left panel** — The story:
- NEXUS wordmark + "personal AI operating system"
- "Welcome" / "NEXUS runs the work." headings (Space Grotesk 40px)
- Description paragraph
- 4-step progress (Business → Objective → Authority → Agents) with numbered circles and connecting lines
- 3 info cards (root authority, why lineage, office-as-view) with accent lines (blue / purple / teal)
- Runtime footer note

**Right panel** — The form (inside a 610×664 card):
- "Create your first business" title + subtitle
- Business name input (pre-filled: "AI Agency")
- Primary objective input (pre-filled: "Win three enterprise deals this quarter")
- Divisions to start with (Research, Business, Media, Development — toggle chips)
- "Create business" primary button (gradient #6c8cff → #b18cff)
- "Load a sample" secondary button
- "Step 1 of 4" indicator
- Bottom note: "You can change every one of these later — nothing here is permanent."

Fonts: JetBrains Mono (mono/UI), Inter (body), Space Grotesk (headings) — loaded from Google Fonts to match the original embedded base64 fonts.

## Backend Integration

### API Endpoint

`FirstRun` calls `POST /api/v1/businesses` via the `apiClient.createBusiness()` method added to `src/api/client.ts`.

**Request body:**
```json
{
  "name": "AI Agency",
  "owner_identity_id": "nx:human:bootstrap",
  "metadata": {
    "objective": "Win three enterprise deals this quarter",
    "divisions": "[\"research\"]",
    "created_via": "first-run"
  }
}
```

**Response:** The created `Business` record (`entity_id`, `name`, etc.).

### Backend routes used

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/businesses` | List businesses (used at login to validate credentials) |
| `POST` | `/api/v1/businesses` | Create a new business (first-run onboarding) |
| `GET` | `/health` | Health check (public) |

The backend business creation handler is in `internal/gateway/org.go` (`handleCreateBusiness`). It is membership-free (bootstrap-friendly) and requires only authentication when enforcement is on.

### API client additions

`src/api/client.ts`:
- Added `createBusiness(request: CreateBusinessRequest): Promise<Business>`

`src/types/api.ts`:
- Added `Business` interface
- Added `CreateBusinessRequest` interface

## App Flow

```
Authentication → [no business] → FirstRun → POST /api/v1/businesses → OfficeLayout
                                    ↑                             │
                                    └──── onBusinessCreated ──────┘
```

1. User logs in (or session restored from localStorage)
2. `AppContent` checks `onboardingComplete` (restored from `localStorage` `nexus-auth.businessId`)
3. If not complete: renders `<FirstRun actorId={...} onBusinessCreated={...} />`
4. User fills the form and clicks "Create business"
5. `FirstRun` calls `apiClient.createBusiness()`
6. On success: `onBusinessCreated` is called, which:
   - Sets `businessId` state
   - Sets `onboardingComplete = true`
   - Persists `businessId` to `localStorage` `nexus-auth`
7. `AppContent` re-renders with `OfficeLayout` and the chosen `businessId`

## Persistence

The chosen `businessId` is stored in `localStorage` under key `nexus-auth` alongside the existing auth record:
```json
{
  "actorId": "nx:human:bootstrap",
  "credential": "...",
  "businessId": "biz-abc123",
  "controlKey": ""
}
```

On refresh, `AppContent` reads `nexus-auth` from `localStorage`. If `businessId` exists and is not `"default"`, onboarding is skipped and the office layout loads directly.

## Running

### Development
```bash
cd /home/pc/NEXUS/nexus-app
npm run dev          # Vite dev server on :3000 (proxies /api to :8081)
```

### Build
```bash
cd /home/pc/NEXUS/nexus-app
npm run build        # Vite build to dist/
```

Note: `tsc --noEmit` passes cleanly. The `vite build` has a pre-existing unrelated error (`./OfficeTable` module not found in `OfficeFloor.tsx`) that exists on the main branch and is not caused by this change.

### Backend
The gateway runs on `:8081` (API) and `:8080` (health). Start with:
```bash
cd /home/pc/NEXUS
go run ./cmd/nexus
```

## Testing

- Type check: `cd nexus-app && npx tsc --noEmit`
- Unit tests: `cd nexus-app && npm test` (vitest)

There are no FirstRun-specific unit tests yet. The `authStorage.test.ts` and `localSession.test.ts` continue to pass.

## Customization

- **Pre-filled values**: Edit `businessName` and `objective` `useState` initializers in `FirstRun.tsx`
- **Divisions**: Edit the `DIVISIONS` constant array in `FirstRun.tsx`
- **Colors**: Edit `FirstRun.module.css` — the design uses `#6c8cff` (blue), `#b18cff` (purple), `#4fd1c5` (teal), `#f5b759` (amber) accents on `#080b11` / `#0b0e14` / `#11151e` / `#161b26` surfaces
- **Fonts**: Google Fonts imports at the top of `FirstRun.module.css` (JetBrains Mono, Inter, Space Grotesk)