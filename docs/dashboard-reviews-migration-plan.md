# Plan: Migrate Review List to Reviews Page, Rebuild Dashboard with Aggregates

## Context

The Dashboard currently shows a "Recent Reviews" table with real-time WebSocket updates, while the dedicated Reviews page has broken, stale code that never fetches data on load (`renderReviewList()` resets state and renders UI but never calls `fetchReviews()`). The user wants to:

1. **Discard the old Reviews page logic entirely** (buggy, no WebSocket, never calls `fetchReviews()`)
2. **Migrate the WebSocket-powered review list from Dashboard to Reviews page**, keeping its filter/pagination controls
3. **Replace Dashboard's review table with aggregate/trend components** (status distribution, active reviews)

**Single file changed:** `web/index.html` (all frontend CSS/JS/HTML lives here)

---

## Step 1: Add CSS for new Dashboard components

Add after `.load-more-wrap` style (line 417):

```css
/* Status distribution bar */
.status-bar {
    height: 28px;
    border-radius: 6px;
    overflow: hidden;
    display: flex;
    background: var(--bg-secondary);
}
.status-bar__segment {
    height: 100%;
    transition: width 0.4s ease;
    min-width: 2px;
}
.status-bar__segment--completed { background: var(--status-completed); }
.status-bar__segment--failed { background: var(--status-failed); }
.status-bar__segment--timed_out { background: var(--status-timed_out); }
.status-bar__segment--cancelled { background: var(--status-cancelled); }
.status-bar__segment--active { background: var(--status-running); }
.status-bar__legend {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    margin-top: 8px;
    font-size: 13px;
    color: var(--text-secondary);
}
.status-bar__legend-item {
    display: flex;
    align-items: center;
    gap: 4px;
}
.swatch {
    display: inline-block;
    width: 12px;
    height: 12px;
    border-radius: 3px;
}
.swatch--completed { background: var(--status-completed); }
.swatch--failed { background: var(--status-failed); }
.swatch--timed_out { background: var(--status-timed_out); }
.swatch--cancelled { background: var(--status-cancelled); }
.swatch--active { background: var(--status-running); }

/* Active reviews */
.active-reviews {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.active-review-card {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
    border-radius: 6px;
    background: var(--bg-secondary);
    font-size: 14px;
}
.active-review-card .run-id {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--text-secondary);
}
.active-review-card .review-info {
    flex: 1;
    display: flex;
    gap: 8px;
    align-items: center;
}
.active-review-card .review-info span {
    color: var(--text-secondary);
}
```

Non-destructive — no existing code breaks.

---

## Step 2: Add cleanup helper functions

Create two new functions (place near the router, around line 1940):

```javascript
function cleanupDashboardHandlers() {
    if (dashboardState.onReviewUpdate) {
        ws.off("review_update", dashboardState.onReviewUpdate);
        dashboardState.onReviewUpdate = null;
    }
    if (dashboardState.onMetricsUpdate) {
        ws.off("metrics_update", dashboardState.onMetricsUpdate);
        dashboardState.onMetricsUpdate = null;
    }
}

function cleanupReviewsPageHandlers() {
    if (reviewsPageState.onReviewUpdate) {
        ws.off("review_update", reviewsPageState.onReviewUpdate);
        reviewsPageState.onReviewUpdate = null;
    }
}
```

These run at the top of every route change, ensuring no stale handlers from a previous page leak into the next.

---

## Step 3: Add `reviewsPageState` (replaces `listState`)

Replace the existing `listState` variable (line 2169) with:

```javascript
var reviewsPageState = {
    status: "",
    repo: "",
    offset: 0,
    reviews: [],
    loading: false,
    onReviewUpdate: null
};
```

---

## Step 4: Rewrite the router

Rewrite `route()` (line 1950) to:

1. Call `cleanupDashboardHandlers()` + `cleanupReviewsPageHandlers()` at the top (cleanup before setup)
2. Remove the inline dashboard cleanup logic (the old `if hash.indexOf("#/dashboard") === -1` block — lines 1961-1967)
3. Dispatch to `renderDashboard()`, new `renderReviewsPage()`, or `renderReviewDetail()`

```javascript
function route() {
    if (!Auth.checked() || !Auth.authenticated()) return;

    var hash = location.hash || "#/dashboard";

    // Cleanup ALL page-specific WebSocket handlers before routing
    cleanupDashboardHandlers();
    cleanupReviewsPageHandlers();

    updateNav(hash);

    if (hash === "#/" || hash === "" || hash === "#/dashboard") {
        renderDashboard();
    } else if (hash === "#/reviews") {
        renderReviewsPage();
    } else {
        var match = hash.match(/^#\/reviews\/(.+)$/);
        if (match) {
            renderReviewDetail(match[1]);
        } else {
            renderDashboard();
        }
    }
}
```

**Subscription management:**
- Both Dashboard and Reviews call `ws.subscribe("all")` — idempotent, harmless to call twice
- Only `renderReviewDetail()` calls `ws.unsubscribe("all")` (already does this at line 2265)
- Do NOT unsubscribe from `"all"` during Dashboard-to-Reviews or Reviews-to-Dashboard transitions — only when going to a detail page

---

## Step 5: Build new Reviews page functions

**Delete** the following (lines 2169-2237):
- `listState` variable
- `renderReviewList()` function
- `renderListContent()` function
- `fetchReviews()` function
- `window.__applyFilter` assignment
- `window.__loadMore` assignment

**Create** the following functions in their place:

### `renderReviewsPage()` — entry point

```javascript
function renderReviewsPage() {
    reviewsPageState.offset = 0;
    reviewsPageState.reviews = [];
    reviewsPageState.loading = true;
    ws.subscribe("all");

    app.innerHTML = '<div class="loading">Loading reviews</div>';

    fetchReviewsForPage(function() {
        renderReviewsPageContent();
        setupReviewsPageWebSocketHandlers();
    });
}
```

### `fetchReviewsForPage(callback)` — API fetch with filters + pagination

```javascript
function fetchReviewsForPage(callback) {
    reviewsPageState.loading = true;
    var params = { limit: 20, offset: reviewsPageState.offset };
    if (reviewsPageState.status) params.status = reviewsPageState.status;
    if (reviewsPageState.repo) params.repo = reviewsPageState.repo;

    api.listReviews(params).then(function(data) {
        reviewsPageState.loading = false;
        reviewsPageState.reviews = reviewsPageState.reviews.concat(data.reviews || []);
        reviewsPageState.offset += data.reviews ? data.reviews.length : 0;
        if (callback) callback();
    }).catch(function() {
        reviewsPageState.loading = false;
        if (callback) callback();
    });
}
```

### `renderReviewsPageContent()` — full HTML render

```javascript
function renderReviewsPageContent() {
    var html = '<div class="table-wrap">' +
        '<div class="table-header">' +
        '<h2>Reviews</h2>' +
        '<div class="filters">' +
        '<select id="status-filter" onchange="window.__applyFilter()">' +
        '<option value="">All statuses</option>' +
        '<option value="pending"' + (reviewsPageState.status === "pending" ? " selected" : "") + '>Pending</option>' +
        '<option value="running"' + (reviewsPageState.status === "running" ? " selected" : "") + '>Running</option>' +
        '<option value="completed"' + (reviewsPageState.status === "completed" ? " selected" : "") + '>Completed</option>' +
        '<option value="failed"' + (reviewsPageState.status === "failed" ? " selected" : "") + '>Failed</option>' +
        '<option value="timed_out"' + (reviewsPageState.status === "timed_out" ? " selected" : "") + '>Timed out</option>' +
        '<option value="cancelled"' + (reviewsPageState.status === "cancelled" ? " selected" : "") + '>Cancelled</option>' +
        '</select>' +
        '<input type="text" id="repo-filter" placeholder="Filter by repo..." value="' + esc(reviewsPageState.repo) + '" onkeydown="if(event.key===\'Enter\')window.__applyFilter()">' +
        '<button class="btn" onclick="window.__applyFilter()">Filter</button>' +
        '</div></div>';

    if (reviewsPageState.loading && reviewsPageState.reviews.length === 0) {
        html += '<div class="loading">Loading</div>';
    } else if (reviewsPageState.reviews.length === 0) {
        html += '<div class="empty">No reviews found</div>';
    } else {
        html += '<table><thead><tr>' +
            '<th>Run ID</th><th>Repo</th><th>PR #</th><th>Status</th><th>Duration</th><th>Created</th><th></th>' +
            '</tr></thead><tbody id="reviews-table-body">';
        reviewsPageState.reviews.forEach(function(r) {
            html += reviewRow(r);
        });
        html += '</tbody></table>';
        html += '<div class="load-more-wrap"><button class="btn" onclick="window.__loadMore()">Load more</button></div>';
    }

    html += '</div>';
    app.innerHTML = html;
}
```

### `reviewRow(r)` — single row HTML generator (extracted for reuse)

```javascript
function reviewRow(r) {
    return '<tr>' +
        '<td><span class="run-id">' + esc(truncate(r.run_id, 8)) + '</span></td>' +
        '<td>' + esc(basename(r.repo)) + '</td>' +
        '<td>' + (r.pr_number || '-') + '</td>' +
        '<td><span class="' + badgeClass(r.status) + '">' + esc(r.status) + '</span></td>' +
        '<td>' + formatDuration(r.duration_ms) + '</td>' +
        '<td>' + formatDate(r.created_at) + '</td>' +
        '<td><a href="#/reviews/' + r.run_id + '" class="view-link">View</a></td>' +
        '</tr>';
}
```

### `updateReviewsPageTable()` — DOM-patch table body only

```javascript
function updateReviewsPageTable() {
    var tbody = document.getElementById("reviews-table-body");
    if (!tbody) return;
    var html = "";
    for (var i = 0; i < reviewsPageState.reviews.length; i++) {
        html += reviewRow(reviewsPageState.reviews[i]);
    }
    tbody.innerHTML = html;
}
```

Uses `id="reviews-table-body"` to avoid selector conflicts with other pages.

### `setupReviewsPageWebSocketHandlers()` — real-time updates

```javascript
function setupReviewsPageWebSocketHandlers() {
    if (reviewsPageState.onReviewUpdate) {
        ws.off("review_update", reviewsPageState.onReviewUpdate);
    }

    reviewsPageState.onReviewUpdate = function(msg) {
        var reviewData = msg.review;
        if (!reviewData) return;

        var runId = reviewData.run_id;
        var statusFilter = reviewsPageState.status;
        var repoFilter = reviewsPageState.repo;

        // Check if review matches active filters
        if (statusFilter && reviewData.status !== statusFilter) {
            // Review doesn't match status filter — remove if it was in the list
            var idx = -1;
            for (var i = 0; i < reviewsPageState.reviews.length; i++) {
                if (reviewsPageState.reviews[i].run_id === runId) { idx = i; break; }
            }
            if (idx >= 0) {
                reviewsPageState.reviews.splice(idx, 1);
                updateReviewsPageTable();
            }
            return;
        }
        if (repoFilter && basename(reviewData.repo).toLowerCase().indexOf(repoFilter.toLowerCase()) === -1) {
            return; // Doesn't match repo filter — ignore entirely
        }

        // Update existing or prepend new
        var existingIdx = -1;
        for (var i = 0; i < reviewsPageState.reviews.length; i++) {
            if (reviewsPageState.reviews[i].run_id === runId) { existingIdx = i; break; }
        }

        if (existingIdx >= 0) {
            Object.assign(reviewsPageState.reviews[existingIdx], reviewData);
        } else {
            reviewsPageState.reviews.unshift(reviewData);
        }
        updateReviewsPageTable();
    };

    ws.on("review_update", reviewsPageState.onReviewUpdate);
}
```

### `window.__applyFilter` and `window.__loadMore`

```javascript
window.__applyFilter = function() {
    var s = document.getElementById("status-filter");
    var r = document.getElementById("repo-filter");
    reviewsPageState.status = s ? s.value : "";
    reviewsPageState.repo = r ? r.value : "";
    reviewsPageState.offset = 0;
    reviewsPageState.reviews = [];
    reviewsPageState.loading = true;
    renderReviewsPageContent();
    fetchReviewsForPage(function() {
        renderReviewsPageContent();
    });
};

window.__loadMore = function() {
    fetchReviewsForPage(function() {
        renderReviewsPageContent();
    });
};
```

**Reuses:** `reviewTable()` shared component can be kept but the new code uses `reviewRow()` directly with an explicit `<tbody id="reviews-table-body">` for targeted DOM patching. The old `reviewTable()` function can stay for backward compatibility or be removed in cleanup.

---

## Step 6: Modify `dashboardState`

Change (line 1992):

```javascript
// Before:
var dashboardState = { metrics: null, reviews: [], onReviewUpdate: null, onMetricsUpdate: null };

// After:
var dashboardState = { metrics: null, pendingReviews: [], runningReviews: [], onReviewUpdate: null, onMetricsUpdate: null };
```

Remove `reviews` array, add `pendingReviews` and `runningReviews`.

---

## Step 7: Rewrite Dashboard rendering

### Rewrite `renderDashboard()` (line 2000)

```javascript
function renderDashboard() {
    app.innerHTML = '<div class="loading">Loading metrics</div>';
    ws.subscribe("all");
    Promise.all([
        api.getMetrics(),
        api.listReviews({ status: "pending", limit: 5 }),
        api.listReviews({ status: "running", limit: 5 })
    ]).then(function(results) {
        var m = results[0];
        var pending = (results[1] && results[1].reviews) || [];
        var running = (results[2] && results[2].reviews) || [];
        renderDashboardContent(m, pending, running);
        setupDashboardWebSocketHandlers();
    }).catch(function() {
        app.innerHTML = '<div class="empty">Failed to load dashboard data</div>';
    });
}
```

### Rewrite `renderDashboardContent(metrics, pendingReviews, runningReviews)` (line 2015)

```javascript
function renderDashboardContent(metrics, pendingReviews, runningReviews) {
    dashboardState.metrics = metrics;
    dashboardState.pendingReviews = pendingReviews || [];
    dashboardState.runningReviews = runningReviews || [];

    var html = '<div class="stats-grid">' +
        statCard("Total Reviews", metrics.total_reviews || 0) +
        statCard("Success Rate", formatPercent(metrics.success_count || 0, metrics.total_reviews || 0)) +
        statCard("Avg Duration", formatDuration(metrics.avg_duration_ms || 0)) +
        statCard("Reviews Today", metrics.reviews_today || 0) +
        '</div>' +
        '<div class="table-wrap">' +
        '<div class="table-header"><h2>Status Distribution</h2></div>' +
        statusBar(metrics) +
        '</div>' +
        '<div class="table-wrap">' +
        '<div class="table-header"><h2>Active Reviews</h2></div>' +
        activeReviewsSection(pendingReviews, runningReviews) +
        '</div>';
    app.innerHTML = html;
}
```

### New helper: `statusBar(metrics)`

```javascript
function statusBar(metrics) {
    var total = metrics.total_reviews || 0;
    if (total === 0) {
        return '<div class="empty">No reviews yet</div>';
    }

    var completed = metrics.success_count || 0;
    var failed = metrics.failure_count || 0;
    var timedOut = metrics.timed_out_count || 0;
    var cancelled = metrics.cancelled_count || 0;
    var active = total - completed - failed - timedOut - cancelled;

    var segments = "";
    if (completed > 0) segments += '<div class="status-bar__segment status-bar__segment--completed" style="width:' + ((completed/total)*100).toFixed(1) + '%"></div>';
    if (failed > 0) segments += '<div class="status-bar__segment status-bar__segment--failed" style="width:' + ((failed/total)*100).toFixed(1) + '%"></div>';
    if (timedOut > 0) segments += '<div class="status-bar__segment status-bar__segment--timed_out" style="width:' + ((timedOut/total)*100).toFixed(1) + '%"></div>';
    if (cancelled > 0) segments += '<div class="status-bar__segment status-bar__segment--cancelled" style="width:' + ((cancelled/total)*100).toFixed(1) + '%"></div>';
    if (active > 0) segments += '<div class="status-bar__segment status-bar__segment--active" style="width:' + ((active/total)*100).toFixed(1) + '%"></div>';

    var legend = '<div class="status-bar__legend">' +
        '<span class="status-bar__legend-item"><span class="swatch swatch--completed"></span> Completed: ' + completed + '</span>' +
        '<span class="status-bar__legend-item"><span class="swatch swatch--failed"></span> Failed: ' + failed + '</span>' +
        '<span class="status-bar__legend-item"><span class="swatch swatch--timed_out"></span> Timed out: ' + timedOut + '</span>' +
        '<span class="status-bar__legend-item"><span class="swatch swatch--cancelled"></span> Cancelled: ' + cancelled + '</span>' +
        (active > 0 ? '<span class="status-bar__legend-item"><span class="swatch swatch--active"></span> Active: ' + active + '</span>' : '') +
        '</div>';

    return '<div class="status-bar">' + segments + '</div>' + legend;
}
```

Note: Backend uses `failure_count` (not `failed_count`). Verified from `internal/storage/store.go:58`.

### New helper: `activeReviewsSection(pending, running)`

```javascript
function activeReviewsSection(pending, running) {
    var all = (running || []).concat(pending || []);
    if (!all.length) {
        return '<div class="empty">No active reviews</div>';
    }

    var html = '<div class="active-reviews">';
    all.forEach(function(r) {
        html += '<div class="active-review-card">' +
            '<span class="run-id">' + esc(truncate(r.run_id, 8)) + '</span>' +
            '<span class="' + badgeClass(r.status) + '">' + esc(r.status) + '</span>' +
            '<div class="review-info">' +
            '<span>' + esc(basename(r.repo)) + '</span>' +
            '<span>#' + (r.pr_number || '-') + '</span>' +
            '</div>' +
            '<span>' + formatDuration(r.duration_ms) + '</span>' +
            '<a href="#/reviews/' + r.run_id + '" class="view-link">View</a>' +
            '</div>';
    });
    html += '</div>';
    return html;
}
```

---

## Step 8: Update Dashboard WebSocket handlers

### Delete `updateReviewsTable()` (line 2033) — no longer needed

### New: `updateStatusDistribution()`

```javascript
function updateStatusDistribution() {
    var container = document.querySelector(".status-bar");
    var legend = document.querySelector(".status-bar__legend");
    if (!container || !dashboardState.metrics) return;

    var m = dashboardState.metrics;
    var total = m.total_reviews || 0;
    if (total === 0) return;

    var completed = m.success_count || 0;
    var failed = m.failure_count || 0;
    var timedOut = m.timed_out_count || 0;
    var cancelled = m.cancelled_count || 0;
    var active = total - completed - failed - timedOut - cancelled;

    var segments = "";
    if (completed > 0) segments += '<div class="status-bar__segment status-bar__segment--completed" style="width:' + ((completed/total)*100).toFixed(1) + '%"></div>';
    if (failed > 0) segments += '<div class="status-bar__segment status-bar__segment--failed" style="width:' + ((failed/total)*100).toFixed(1) + '%"></div>';
    if (timedOut > 0) segments += '<div class="status-bar__segment status-bar__segment--timed_out" style="width:' + ((timedOut/total)*100).toFixed(1) + '%"></div>';
    if (cancelled > 0) segments += '<div class="status-bar__segment status-bar__segment--cancelled" style="width:' + ((cancelled/total)*100).toFixed(1) + '%"></div>';
    if (active > 0) segments += '<div class="status-bar__segment status-bar__segment--active" style="width:' + ((active/total)*100).toFixed(1) + '%"></div>';

    container.innerHTML = segments;

    if (legend) {
        legend.innerHTML =
            '<span class="status-bar__legend-item"><span class="swatch swatch--completed"></span> Completed: ' + completed + '</span>' +
            '<span class="status-bar__legend-item"><span class="swatch swatch--failed"></span> Failed: ' + failed + '</span>' +
            '<span class="status-bar__legend-item"><span class="swatch swatch--timed_out"></span> Timed out: ' + timedOut + '</span>' +
            '<span class="status-bar__legend-item"><span class="swatch swatch--cancelled"></span> Cancelled: ' + cancelled + '</span>' +
            (active > 0 ? '<span class="status-bar__legend-item"><span class="swatch swatch--active"></span> Active: ' + active + '</span>' : '');
    }
}
```

### New: `updateActiveReviews()`

```javascript
function updateActiveReviews() {
    var container = document.querySelector(".active-reviews");
    if (!container) return;

    var all = dashboardState.runningReviews.concat(dashboardState.pendingReviews);
    if (!all.length) {
        var wrap = container.parentElement;
        if (wrap) wrap.innerHTML = '<div class="empty">No active reviews</div>';
        return;
    }

    var html = "";
    all.forEach(function(r) {
        html += '<div class="active-review-card">' +
            '<span class="run-id">' + esc(truncate(r.run_id, 8)) + '</span>' +
            '<span class="' + badgeClass(r.status) + '">' + esc(r.status) + '</span>' +
            '<div class="review-info">' +
            '<span>' + esc(basename(r.repo)) + '</span>' +
            '<span>#' + (r.pr_number || '-') + '</span>' +
            '</div>' +
            '<span>' + formatDuration(r.duration_ms) + '</span>' +
            '<a href="#/reviews/' + r.run_id + '" class="view-link">View</a>' +
            '</div>';
    });
    container.innerHTML = html;
}
```

### Rewrite `setupDashboardWebSocketHandlers()` (line 2070)

```javascript
function setupDashboardWebSocketHandlers() {
    if (dashboardState.onReviewUpdate) {
        ws.off("review_update", dashboardState.onReviewUpdate);
    }
    if (dashboardState.onMetricsUpdate) {
        ws.off("metrics_update", dashboardState.onMetricsUpdate);
    }

    dashboardState.onReviewUpdate = function(msg) {
        var reviewData = msg.review;
        var newStatus = msg.status;

        if (reviewData) {
            // Check if this review was in active lists
            var wasActive = false;
            var removeIdx;

            // Check running reviews
            for (var i = 0; i < dashboardState.runningReviews.length; i++) {
                if (dashboardState.runningReviews[i].run_id === msg.run_id) {
                    wasActive = true;
                    removeIdx = i;
                    break;
                }
            }
            if (wasActive) {
                dashboardState.runningReviews.splice(removeIdx, 1);
            }

            // Check pending reviews
            if (!wasActive) {
                for (var i = 0; i < dashboardState.pendingReviews.length; i++) {
                    if (dashboardState.pendingReviews[i].run_id === msg.run_id) {
                        wasActive = true;
                        removeIdx = i;
                        break;
                    }
                }
                if (wasActive) {
                    dashboardState.pendingReviews.splice(removeIdx, 1);
                }
            }

            // If review is now active (pending or running), add it
            if (newStatus === "running") {
                dashboardState.runningReviews.unshift(reviewData);
                if (dashboardState.runningReviews.length > 5) {
                    dashboardState.runningReviews = dashboardState.runningReviews.slice(0, 5);
                }
            } else if (newStatus === "pending") {
                dashboardState.pendingReviews.unshift(reviewData);
                if (dashboardState.pendingReviews.length > 5) {
                    dashboardState.pendingReviews = dashboardState.pendingReviews.slice(0, 5);
                }
            }

            updateActiveReviews();
        }

        // Status distribution changes with every review update
        updateStatusDistribution();
    };

    dashboardState.onMetricsUpdate = function(msg) {
        if (msg.metrics) {
            dashboardState.metrics = msg.metrics;
            updateMetricsCards(msg.metrics);
            updateStatusDistribution();
        }
    };

    ws.on("review_update", dashboardState.onReviewUpdate);
    ws.on("metrics_update", dashboardState.onMetricsUpdate);
}
```

### Keep unchanged
- `updateMetricsCards(metrics)` (line 2053)
- `statCard(label, value)` (line 2164)

---

## Step 9: Delete dead code

After all new code is in place, delete the old functions that have been replaced:

| What | Lines | Reason |
|------|-------|--------|
| `listState` | 2169 | Replaced by `reviewsPageState` |
| `renderReviewList()` | 2171-2175 | Replaced by `renderReviewsPage()` |
| `renderListContent()` | 2177-2206 | Replaced by `renderReviewsPageContent()` |
| `window.__applyFilter` (old) | 2208-2216 | Replaced with new version using `reviewsPageState` |
| `window.__loadMore` (old) | 2218-2220 | Replaced with new version using `reviewsPageState` |
| `fetchReviews()` | 2222-2237 | Replaced by `fetchReviewsForPage()` |
| `updateReviewsTable()` | 2033-2051 | Dashboard no longer shows review table |

Optionally delete `reviewTable()` (line 2240) if the Reviews page uses `reviewRow()` directly instead. Or keep it as a shared utility.

---

## WebSocket Lifecycle State Machine

```
Route: #/dashboard
  -> cleanupDashboardHandlers()    // no-op (nothing registered yet)
  -> cleanupReviewsPageHandlers()  // no-op
  -> renderDashboard()
     -> ws.subscribe("all")
     -> ws.on("review_update", dashboardState.onReviewUpdate)
     -> ws.on("metrics_update", dashboardState.onMetricsUpdate)

Route: #/dashboard -> #/reviews
  -> cleanupDashboardHandlers()    // ws.off("review_update") + ws.off("metrics_update")
  -> cleanupReviewsPageHandlers()  // no-op
  -> renderReviewsPage()
     -> ws.subscribe("all")        // idempotent, already subscribed
     -> ws.on("review_update", reviewsPageState.onReviewUpdate)

Route: #/reviews -> #/reviews/{run_id}
  -> cleanupDashboardHandlers()    // no-op
  -> cleanupReviewsPageHandlers()  // ws.off("review_update")
  -> renderReviewDetail(runId)
     -> ws.unsubscribe("all")      // already does this at line 2265
     -> ws.subscribe("run:{runId}")
     -> ws.on("stream", ...)
     -> ws.on("stream_done", ...)
     -> ws.on("review_update", ...)

Route: #/reviews/{run_id} -> #/reviews
  -> cleanupDashboardHandlers()    // no-op
  -> cleanupReviewsPageHandlers()  // no-op (detail page has its own handlers)
  -> renderReviewsPage()
     -> ws.subscribe("all")        // re-subscribe
     -> ws.on("review_update", reviewsPageState.onReviewUpdate)
```

---

## Backend Data Used

No backend changes needed. All data comes from existing endpoints:

| Source | Data | Used By |
|--------|------|---------|
| `GET /metrics` | `total_reviews`, `success_count`, `failure_count`, `timed_out_count`, `cancelled_count`, `avg_duration_ms`, `reviews_today` | Dashboard stat cards + status distribution bar |
| `GET /reviews?status=pending&limit=5` | Pending reviews | Dashboard active reviews |
| `GET /reviews?status=running&limit=5` | Running reviews | Dashboard active reviews |
| `GET /reviews?limit=20&offset=N` | Paginated review list | Reviews page |
| WS `review_update` | Embedded full `ReviewRecord` | Both pages (real-time updates) |
| WS `metrics_update` | Updated `Metrics` object | Dashboard (real-time stat updates) |

Note: Backend `Metrics` struct uses `failure_count` (not `failed_count`). Verified from `internal/storage/store.go:58`.

---

## Functions Summary

| Action | Function | Lines |
|--------|----------|-------|
| DELETE | `listState` | 2169 |
| DELETE | `renderReviewList()` | 2171 |
| DELETE | `renderListContent()` | 2177 |
| DELETE | `fetchReviews()` | 2222 |
| DELETE | `updateReviewsTable()` | 2033 |
| MODIFY | `dashboardState` | 1992 |
| MODIFY | `route()` | 1950 |
| REWRITE | `renderDashboard()` | 2000 |
| REWRITE | `renderDashboardContent()` | 2015 |
| REWRITE | `setupDashboardWebSocketHandlers()` | 2070 |
| CREATE | `cleanupDashboardHandlers()` | new |
| CREATE | `cleanupReviewsPageHandlers()` | new |
| CREATE | `reviewsPageState` | replaces listState |
| CREATE | `renderReviewsPage()` | new |
| CREATE | `fetchReviewsForPage()` | new |
| CREATE | `renderReviewsPageContent()` | new |
| CREATE | `reviewRow()` | extracted from reviewTable |
| CREATE | `updateReviewsPageTable()` | new |
| CREATE | `setupReviewsPageWebSocketHandlers()` | new |
| CREATE | `statusBar()` | new |
| CREATE | `updateStatusDistribution()` | new |
| CREATE | `activeReviewsSection()` | new |
| CREATE | `updateActiveReviews()` | new |
| KEEP | `statCard()`, `updateMetricsCards()`, `badgeClass()`, all formatters | unchanged |
| KEEP | `reviewTable()` | optionally keep or delete |
| KEEP | `renderReviewDetail()`, `renderStreamingView()` | unchanged |
| KEEP | WebSocket manager (`ws`), API module (`api`) | unchanged |

---

## Verification

1. **Build and run**: `rtk docker compose up --build`
2. **Dashboard** (`#/dashboard`):
   - Should show 4 stat cards with correct values
   - Should show status distribution bar (color-coded, proportional widths)
   - Should show active reviews (pending/running) with live WebSocket updates
   - Should NOT show a reviews table
   - Metrics cards should update in real-time when a review completes
3. **Reviews page** (`#/reviews`):
   - Should load reviews immediately on page load (no more "No reviews found" bug)
   - Should show all reviews with filter controls and "Load more" pagination
   - Should update in real-time via WebSocket (new reviews appear, status changes update)
   - Filtering by status should work correctly with WebSocket updates (reviews leaving filtered status get removed)
4. **Route transitions**:
   - Dashboard → Reviews: no stale handlers, reviews load correctly
   - Reviews → Review detail: "all" unsubscribed, detail streams correctly
   - Review detail → Reviews: re-subscribes to "all", list updates resume
   - Reviews → Dashboard: handlers swapped, dashboard renders aggregates
5. **Run tests**: `docker compose run --rm nano-review go test -race ./...`
