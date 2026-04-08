# Frontend Plan — Nano Review Micro-Frontend

> **Status:** Draft — pending approval
> **Date:** 2026-04-09
> **Scope:** Add a read-only monitoring dashboard to the existing Go microbackend

---

## 1. Problem Statement

Nano Review is currently an API-only service. Operators have no visual interface to monitor review status, browse history, or inspect metrics. All interaction requires `curl` or direct API calls against `GET /reviews`, `GET /reviews/{run_id}`, and `GET /metrics`.

This plan adds a **read-only monitoring dashboard** — a single HTML file with embedded CSS and JS — that consumes the existing backend endpoints and renders them as a human-friendly UI.

---

## 2. Design Principles

1. **Zero-build frontend.** One `index.html` file. No Node.js, no npm, no bundler, no framework. Vanilla HTML/CSS/JS only.
2. **Served by Go.** The Go binary serves the HTML file directly via `net/http.FileServer`. No separate web server, no nginx, no CDN.
3. **Read-only.** The dashboard only reads data. Triggering reviews (`POST /review`) remains webhook-only via GitHub Actions. The dashboard never exposes the webhook secret.
4. **No restructuring.** The Go source tree stays exactly as-is. No `./backend` or `./frontend` directory reorganization. The HTML file lives at `web/index.html` and is embedded into the Go binary at build time via `go:embed`.
5. **Mobile-responsive.** Single-column layout on small screens, multi-column on desktop.

---

## 3. Directory Changes

```
nano-review/
├── web/
│   └── index.html              # Single-file frontend (new)
├── cmd/server/
│   └── main.go                 # Add go:embed + static file serving (modified)
├── internal/api/
│   └── handler.go              # Add GET / handler for SPA (modified)
```

**No files are moved.** No `./backend` or `./frontend` directories. The monorepo reorganization is explicitly out of scope — it adds complexity for no benefit when the frontend is a single embedded file.

### Why not separate `backend/` and `frontend/` directories?

- The frontend is one file embedded into the Go binary. A separate directory creates the illusion of a multi-module project.
- Moving Go source files breaks all imports and requires updating `go.mod` module path.
- Docker build context stays simple — `COPY . .` still works.
- If a proper framework (React, etc.) is adopted later, restructuring becomes a separate task.

---

## 4. Backend Modifications

### 4.1 Serve Static Files (`cmd/server/main.go`)

Use `go:embed` to embed `web/index.html` into the binary and serve it at the root path.

```go
//go:embed all:web
var webFS embed.FS

func main() {
    // ... existing setup ...

    mux := http.NewServeMux()

    // API routes (existing)
    mux.HandleFunc("POST /review", api.HandleReview(webhookSecret, worker))
    mux.HandleFunc("GET /reviews", api.HandleListReviews(store))
    mux.HandleFunc("GET /reviews/{run_id}", api.HandleGetReview(store))
    mux.HandleFunc("GET /metrics", api.HandleGetMetrics(store))

    // Static frontend (new)
    webContent, _ := fs.Sub(webFS, "web")
    mux.Handle("GET /", http.StripPrefix("/", http.FileServer(http.FS(webContent))))

    // ... existing server startup ...
}
```

### 4.2 CORS Headers (optional)

If the frontend will ever be served from a different origin (unlikely with embedded approach), add a middleware. For the embedded approach, this is unnecessary — same-origin by default.

### 4.3 No New Endpoints

The existing API is sufficient:
- `GET /metrics` — dashboard stats
- `GET /reviews?repo=&status=&limit=&offset=` — review list
- `GET /reviews/{run_id}` — single review detail (includes `claude_output`)

---

## 5. Frontend Design

### 5.1 Pages / Views

The dashboard is a **single-page application** using hash-based routing (`#/dashboard`, `#/reviews`, `#/reviews/<run_id>`).

| Route | View | Purpose |
|---|---|---|
| `#/dashboard` | Metrics Dashboard | Stats cards + recent reviews |
| `#/reviews` | Review List | Filterable, paginated table |
| `#/reviews/{run_id}` | Review Detail | Single review with Claude output |

**Default route:** `#/dashboard` (redirect from `#/`).

### 5.2 Metrics Dashboard (`#/dashboard`)

Four stat cards at the top:

| Card | Data Source | Format |
|---|---|---|
| Total Reviews | `metrics.total_reviews` | Integer |
| Success Rate | `metrics.success_count / metrics.total_reviews` | Percentage |
| Avg Duration | `metrics.avg_duration_ms` | Human-readable (e.g. "4m 23s") |
| Reviews Today | `metrics.reviews_today` | Integer |

Below the cards: a "Recent Reviews" table showing the last 10 reviews (calls `GET /reviews?limit=10`).

### 5.3 Review List (`#/reviews`)

- **Filter bar:** Status dropdown (`all`, `pending`, `running`, `completed`, `failed`, `timed_out`, `cancelled`).
- **Search:** Repo URL text filter.
- **Table columns:** Run ID (truncated), Repo (basename), PR #, Status (badge), Duration, Created, Actions (view).
- **Pagination:** "Load more" button at the bottom (uses `offset` parameter).
- **Status badges:** Color-coded — green (completed), blue (running), yellow (pending), red (failed), orange (timed_out), gray (cancelled).

### 5.4 Review Detail (`#/reviews/{run_id}`)

- **Header:** Run ID, status badge, repo name, PR number.
- **Metadata table:** Base branch, head branch, duration, attempts, created at, completed at.
- **Claude Output:** Expandable `<pre>` block showing the full `claude_output` field. Rendered as raw text (not markdown) for safety — the output is machine-generated and may contain code blocks.
- **Back button:** Returns to review list.

### 5.5 Auto-Refresh

- Dashboard: polls `GET /metrics` and `GET /reviews?limit=10` every 30 seconds.
- Review detail: if status is `pending` or `running`, polls `GET /reviews/{run_id}` every 5 seconds. Stops polling when status reaches a terminal state.

### 5.6 What's NOT in the Dashboard

| Feature | Reason |
|---|---|
| Trigger review form | `POST /review` requires webhook secret. Exposing it in the browser is a security risk. Reviews are triggered by GitHub Actions only. |
| Cancel/delete reviews | No backend endpoint exists. Out of scope. |
| Authentication/login | The backend has no auth on read endpoints. Adding auth is a separate task. |
| WebSocket/SSE | Overkill for a monitoring dashboard. Polling is simpler and sufficient. |
| Charts/graphs | No graphing library. Keep it text-based. Can add later if needed. |
| Dark mode | Not MVP. Can add via CSS media query later. |

---

## 6. Technical Approach

### 6.1 Single File Structure

`web/index.html` will contain:

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Nano Review</title>
    <style>
        /* ~200 lines of CSS: layout, cards, table, badges, responsive */
    </style>
</head>
<body>
    <nav>...</nav>
    <div id="app"></div>
    <script>
        /* ~300 lines of JS: router, API client, views, polling */
    </script>
</body>
</html>
```

**Estimated size:** ~500-600 lines total. Small enough to reason about, large enough to be useful.

### 6.2 CSS Approach

- CSS custom properties (variables) for colors and spacing — makes future theming trivial.
- CSS Grid for dashboard cards, Flexbox for nav and layouts.
- No CSS framework. The UI is simple enough that utility classes add more weight than value.
- Responsive breakpoint at 768px (single-column below, multi-column above).

### 6.3 JavaScript Architecture

```
// Minimal SPA router
const routes = {
    '/dashboard': renderDashboard,
    '/reviews': renderReviewList,
    '/reviews/:id': renderReviewDetail,
};

// API client (thin wrapper around fetch)
const api = {
    getMetrics: () => fetch('/metrics').then(r => r.json()),
    listReviews: (params) => fetch('/reviews?' + new URLSearchParams(params)).then(r => r.json()),
    getReview: (id) => fetch('/reviews/' + id).then(r => r.json()),
};

// View renderers (return HTML strings, attach event listeners after mount)
function renderDashboard() { ... }
function renderReviewList() { ... }
function renderReviewDetail(runId) { ... }

// Polling manager
function startPolling(fn, intervalMs) { ... }
function stopPolling() { ... }
```

### 6.4 Color Palette

| Element | Color | Hex |
|---|---|---|
| Background | White | `#ffffff` |
| Surface | Light gray | `#f9fafb` |
| Border | Gray | `#e5e7eb` |
| Text primary | Near-black | `#111827` |
| Text secondary | Gray | `#6b7280` |
| Accent (nav, links) | Indigo | `#4f46e5` |
| Status: completed | Green | `#10b981` |
| Status: running | Blue | `#3b82f6` |
| Status: pending | Yellow | `#f59e0b` |
| Status: failed | Red | `#ef4444` |
| Status: timed_out | Orange | `#f97316` |
| Status: cancelled | Gray | `#9ca3af` |

---

## 7. Docker & Build Changes

### 7.1 Dockerfile

No changes required. `go:embed` bundles the HTML file into the binary at compile time. The existing `COPY . .` in the builder stage already picks up `web/index.html`.

### 7.2 docker-compose.yml

No changes. The frontend is served by the same Go process on the same port.

### 7.3 Makefile

No changes. `make dev` builds and runs the same way.

---

## 8. Files to Create/Modify

| File | Action | Description |
|---|---|---|
| `web/index.html` | **Create** | Single-file frontend (~500-600 lines) |
| `cmd/server/main.go` | **Modify** | Add `go:embed` directive and static file serving (~10 lines changed) |

---

## 9. Implementation Sequence

1. **Create `web/index.html`** — Build the complete UI with all three views, API client, and router.
2. **Modify `cmd/server/main.go`** — Add `go:embed` for `web/` directory and register `GET /` to serve static files.
3. **Test in Docker** — `make dev`, verify dashboard loads at `http://localhost:8080`, API routes still work.
4. **Verify** — Run existing test suite to confirm no regressions.

---

## 10. Future Considerations (Out of Scope)

These are explicitly **not** part of this plan but worth noting for future iterations:

- **Framework migration:** If the UI grows beyond ~1000 lines, consider migrating to a lightweight framework (Preact, htmx, Alpine.js) or a separate build pipeline (Vite + React).
- **Backend restructuring:** If a proper frontend build is added (Node.js, package.json, etc.), then restructuring into `./backend` and `./frontend` directories makes sense. Not needed for a single embedded HTML file.
- **Authentication:** Add a read-only API key or session-based auth for the dashboard. The current backend has no auth on GET endpoints.
- **Dark mode:** Add via `prefers-color-scheme` media query and CSS custom properties.
- **Charts:** Add a simple charting library (Chart.js via CDN) for success rate trends over time.
- **WebSocket/SSE:** Replace polling with server-sent events for live status updates if polling becomes insufficient.
- **Manual review trigger:** Add a new `POST /reviews` endpoint (distinct from webhook `/review`) that accepts auth from the dashboard. Requires a backend auth layer.
- **Cancel running reviews:** Add `DELETE /reviews/{run_id}` endpoint and a cancel button in the UI.
