# Handoff · otel-magnify Console (OpAMP Control Plane)

## Overview

`otel-magnify` is a SaaS control plane for OpenTelemetry fleets. Operators connect their OTel collectors and SDKs over the OpAMP protocol and from a single console they can **inventory** agents, **push versioned configs** (with canary + rollback), watch **alerts**, audit every control-plane action, and manage **multi-tenant RBAC**.

This handoff bundles a clickable, high-fidelity React prototype (`prototype.html`) that covers the full product surface: dashboard, fleet inventory, agent detail, config editor + canary rollout, alerts, audit log, tenants & RBAC, onboarding wizard, and user profile.

## About the Design Files

The file `prototype.html` is a **design reference**, not production code. It is a self-contained HTML page running React + Babel in-browser, wired to mock data, with three switchable themes and FR/EN copy.

**Your task is to recreate these screens in the target codebase's environment.** The existing `otel-magnify` frontend is a React/Vite app — reuse its build setup, routing (React Router), data layer, and UI primitives. The prototype is authoritative for **visual design, layout, copy and interaction model**; it is NOT authoritative for state management, data fetching, or component architecture. Pick idiomatic solutions in the target stack.

## Fidelity

**High-fidelity.** Final colors, typography scales, spacings, iconography, copy (FR + EN), empty / loading / degraded / success states, canary push animation, and at least one populated example per screen. Pixel-match the prototype under the **Signal** theme; the Paper and Terminal themes are showcased as theme variants the user picks from their profile.

## Tech Context

- **Target app:** existing React + Vite frontend for `otel-magnify`.
- **Backend contract:** the control plane already speaks OpAMP and exposes REST endpoints for agents, configs, tenants, audit events, alerts. Wire each screen to the matching endpoint — do not invent new API shapes.
- **Auth:** SSO (Google Workspace on `lumentyr.com`); the user profile page is read-only for identity fields.
- **i18n:** FR + EN must be shipped. The prototype uses a flat key/value dictionary — migrate to `react-i18next` or whatever the codebase already uses.
- **Theming:** driven by `data-theme` attribute on `<html>` + CSS custom properties. Persist user's choice server-side (profile preference) AND in localStorage for instant apply on next load.

## Screens

### 1. Dashboard (`/`)

**Purpose:** at-a-glance health of the fleet for the current tenant.

**Layout:** full-bleed content area with a sticky left sidebar (240px) and a sticky topbar.
- Page title + subtitle.
- **Stat grid** — 6 cards in one row (collapse to 3×2 under 1100px): Collectors, Instrumented SDKs, Supervised, Connected, Degraded, Active alerts. Each card: large tabular-mono value + uppercase mono label + delta chip top-right. Cards are clickable → navigate to the matching screen.
- **Two-col grid** (1.7fr / 1fr):
  - Left: "Push activity (7d)" bar chart (vertical bars, last bar uses accent color), then "Recent alerts" table (first 4 alerts).
  - Right: "Fleet health" panel = SVG donut (connected/degraded/disconnected) + key-value breakdown; then "Deployed versions" panel = mini horizontal bar per version.

### 2. Fleet inventory (`/agents`)

**Purpose:** list every OTel agent (collectors + SDKs) with filters.

**Layout:**
- Filter bar: search input (280px min, 340px max, flex:1) + 3 selects (type, status, control mode).
- Each result is an **agent card** (not a table row) with an icon (collector vs SDK), name, type chip, supervised pill (if any), version, last-seen, label chips on the right, status badge.
- Hover: `translateX(2px)` + border lightens. Click → agent detail.
- Empty-state: dashed panel with `"Aucun agent ne correspond aux filtres"`.

### 3. Agent detail (`/agents/:id`)

**Purpose:** inspect and manage one agent.

**Layout:**
- Breadcrumb + "← Retour à la flotte" link top-right.
- **Detail grid** — 4 cells per row: Type, Version, Status (badge), Control mode (accent color when supervised), Last seen, Active config id, CPU, MEM. Each cell has a mono uppercase label and a mono value.
- Labels block (chip row).
- **Configuration panel**:
  - If supervised → first 18 lines of the YAML with syntax highlighting + "Historique" / "Éditer & pousser" buttons.
  - If read-only → info block with accent left-border explaining that magnify can observe but not push, with a CTA "Passer en supervised".

### 4. Config editor (`/configs/:id` and push flow)

**Purpose:** edit YAML, diff against deployed, push canary with live rollout.

**Layout:**
- Tab strip: **Edit** · **Diff vs déployé** · **Preview**.
  - *Edit* = full YAML with gutter line numbers + token-colored syntax (keys=accent, numbers=amber, strings/urls=green, comments=dim italic).
  - *Diff* = unified diff with `+` lines green-tinted, `−` lines red-tinted.
  - *Preview* = key/value rows summarizing the effective config + CPU/MEM estimates.
- Status row: "✓ YAML valide · 0 erreur · 0 warning" badge + `Valider` + `Push canary →`.
- **Canary rollout** (renders when push starts):
  - 3 progressive stages: `Canary (5%) 3 agents`, `Wave (25%) 15 agents`, `Full (100%) 60 agents`.
  - Each stage is a row: label · progress bar · "X / Y" counter. Active stage has accent border + glow; done stages fade + green tick. Bar fills over ~2.2s per stage.
  - Controls: Pause / Resume, Abort & rollback (danger).
- **Push history** table at the bottom.

### 5. Alerts (`/alerts`)

Standard table (Agent, Rule, Severity, Message, Fired) with severity chips (critical = red, warning = amber, info = accent). "Resolve" button per row. Empty-state `"✓ Aucune alerte active"`.

### 6. Audit log (`/audit`)

**Layout:** filter bar (search + actor select) + **vertical timeline** with accent dot per event (green = success, red = failed). Each item shows: time-ago + event id, action code + actor, target + tenant. Two buttons top-right: Export CSV / Export JSON.

### 7. Tenants & RBAC (`/tenants`)

Visible only in `tenantMode === 'multi'`.

**Layout:**
- Tile grid of tenant cards: 40×40 rounded stamp with initials in tenant color, name, `N agents · N membres · quota N`, and a usage bar colored with the tenant color.
- "New tenant" primary button top-right.
- RBAC table below: email, role chip (owner/editor/viewer with distinct styling), tenants (as colored label-chips), last seen.

### 8. Onboarding (`/onboarding`)

3-step stepper (pills with a numeric circle): Install · Connect · Verify.
- Step 1: code block with `curl ... install.sh | sh`, copy button.
- Step 2: code block with `otel-magnify-supervisor --server ... --token otm_live_...` + info box about 24h token + cert rotation.
- Step 3: spinner + animated "En attente de la connexion OpAMP..." with ellipsis that cycles; after ~4.5s, flip to success card showing the newly-detected agent card + "Voir dans la flotte →".

### 9. User profile (`/me`) — entered via clicking the email chip in the topbar

**Layout:**
- Page header.
- **Identity card** — 56×56 avatar circle (mono initials, accent bg), display name + email + role chip, "Edit" button.
- Two-col body (240px sticky side-nav of anchor links / scrollable content):
  - **Apparence** → **theme picker** = 3 cards with visual swatches (preview of each theme's bars + dot + bg), active card has accent 2px border + accent glow halo + ✓ circle top-right.
  - **Langue** — row with label + pill segmented control (FR/EN).
  - **Densité** — row with label + pill segmented control (compact/normal/confort).
  - **Identité** — read-only panel: email, role, tenants, member since, last login, SSO provider.
  - **Avancé** — tenant-mode toggle + Sign out (danger).
- Each change triggers a toast "Préférences enregistrées".

## Global chrome

### Sidebar (240px, sticky)

- Wordmark `otel-magnify` (accent on `-magnify`) + "OpAMP Control Plane" mono subtitle + pulsing green signal dot top-right.
- Sections: **FLEET** (Dashboard · Agents · Configurations · Alerts[badge]) · **ENTERPRISE** (Audit log · Tenants & RBAC · Onboarding). In single-tenant mode, hide "Tenants & RBAC".
- Active item: accent bg + accent text + 2px left accent bar.
- Footer pill: "LIVE · v0.9.0" + pulsing dot.

### Topbar

- Breadcrumbs (mono) on the left.
- Tenant picker pill (multi-tenant mode only) — colored dot + name + ▾ → dropdown listing tenants + link to Tenants & RBAC.
- User chip (clickable) — 26px accent avatar + email + ▾ → dropdown with: Mon profil, Apparence (both route to profile), API tokens & keys, Sign out (danger).
- Thin 2px accent gradient bar fixed at `top:0`.

## Interactions

- **Persistence:** current screen, selected tenant, theme, density, lang, tenant-mode all persist in `localStorage` (migrate to server-side user prefs for profile-owned settings).
- **Theme switch:** mutate `document.documentElement.setAttribute('data-theme', value)` — all colors are CSS custom properties, reflow is instant.
- **Toast:** bottom-center, high-contrast pill, auto-dismiss in 1.8s.
- **Hover animations:** 120ms ease `cubic-bezier(.2,.7,.2,1)`.
- **Enter animations:** 180ms `translateY(4px) → 0` + opacity.
- **Canary progress:** `setTimeout(advance, 2200)` chain, pausable.

## Design tokens

### Fonts
- Sans (default body): **Plus Jakarta Sans**, 400/500/600/700.
- Mono (code, IDs, numbers, small labels): **Fira Code** 400/500/600.
- Serif (Paper theme display): **Newsreader** 400/600.
- Base size 14px, line-height 1.5.

### Radii
- `sm` 4px · `base` 6px · `lg` 10px.

### Themes

| Token | Signal (default) | Paper | Terminal |
|---|---|---|---|
| `--bg` | `#0B0F14` | `#F5F2EC` | `#0A0E0B` |
| `--surface` | `#0F151C` | `#FBFAF6` | `#0A0E0B` |
| `--surface-2` | `#131B25` | `#F0ECE3` | `#0F1410` |
| `--surface-3` | `#1B2532` | `#E5DFD3` | `#141A15` |
| `--border` | `#1E2A38` | `#DCD4C3` | `#1C2820` |
| `--border-hi` | `#2A3A4D` | `#B8AD96` | `#2C3D2F` |
| `--text` | `#C5D0DD` | `#2A2620` | `#B8D4BC` |
| `--text-hi` | `#E8EEF6` | `#100E0A` | `#DAF0DD` |
| `--text-muted` | `#6E7D90` | `#6B6356` | `#5D7762` |
| `--text-dim` | `#4A5668` | `#9A907D` | `#3A4D3E` |
| `--accent` | `#6EE7FF` cyan | `#B44A1A` rust | `#8AE66E` green |
| `--green` | `#34D399` | `#2E7D4F` | `#8AE66E` |
| `--amber` | `#F0B429` | `#A37300` | `#E8C547` |
| `--red` | `#F87171` | `#B91C1C` | `#F47373` |
| `--purple` | `#C084FC` | `#6B46A8` | `#B589E8` |

Each theme additionally sets `--accent-dim` (12% alpha of accent), `--accent-glow` (25–40% alpha), `--accent-text` (contrasting).

### Density

| Variable | compact | normal | comfort |
|---|---|---|---|
| `--cell-py` | .45rem | .7rem | 1rem |
| `--pad` | .75rem | 1rem | 1.3rem |

### Components

All component-level CSS lives in the `<style>` block of `prototype.html` — lift the classes verbatim (`.panel`, `.data-table`, `.stat-card`, `.agent-card`, `.badge`, `.sev`, `.label-chip`, `.role-chip`, `.btn`, `.btn-primary`, `.btn-danger`, `.yaml-editor`, `.diff-row`, `.canary-stage`, `.timeline`, `.tabstrip`, `.seg`, `.theme-card`, `.user-menu`, `.tenant-menu`) and migrate them into your CSS-in-JS / Tailwind plugin / CSS modules.

## Icons

All icons are inline SVG, stroke 1.6, linecap/linejoin round, 24×24 viewBox, 16×16 render. Swap for your icon library (Lucide / Phosphor) if preferred — match the stroke weight to stay visually consistent.

## Copy

All user-facing copy is centralized in a `DICT` object with `fr` / `en` branches near the top of the `<script type="text/babel">` block. Lift the whole dictionary as the seed translation file.

## State management

- **Session state** (current screen, agent selection): local component state + URL routing.
- **User preferences** (theme, density, language): persisted in user profile table, mirrored to localStorage for SSR-less instant theme application.
- **Tenant context**: stored in URL or a `TenantContext` React provider.
- **Data:** each screen hits a REST endpoint — mock data in the prototype should be replaced with loaders / react-query.

## Files in this bundle

- `README.md` — this document.
- `prototype.html` — the clickable prototype. Open in any browser (no build step). Tweaks panel (bottom-right when the host toolbar enables it) lets you switch theme/density/lang/tenant-mode; the same controls exist in the user profile page.

## Notes for the implementer

- The prototype uses `dangerouslySetInnerHTML` only for YAML/diff syntax highlighting. In production, use a safe tokenizer like `prismjs` or `shiki`.
- The theme picker swatches are hand-built with divs — keep them that way (cheaper than SVG and they reflect the real theme variables well).
- Badge severity styles use a `::before` glowing dot that's disabled in Paper theme — preserve that rule.
- The canary rollout is cosmetic in the prototype. Wire the real OpAMP rollout progress stream (server-sent events or WebSocket) to the stage counters.
- The Audit export buttons (CSV/JSON) are placeholders. Implement server-side export.
