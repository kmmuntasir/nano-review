import ws from "../ws.js";
import api from "../api.js";
import { esc, truncate, basename, formatDuration, formatPercent, badgeClass, repoGitHubUrl, prUrl } from "../utils.js";

let dashboardState = {
    metrics: null,
    queuedReviews: [],
    pendingReviews: [],
    runningReviews: [],
    onReviewUpdate: null,
    onMetricsUpdate: null
};

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

function renderDashboard() {
    var app = document.getElementById("app");
    app.innerHTML = '<div class="loading">Loading metrics</div>';
    ws.subscribe("all");
    Promise.all([
        api.getMetrics(),
        api.listReviews({ status: "queued", limit: 5 }),
        api.listReviews({ status: "pending", limit: 5 }),
        api.listReviews({ status: "running", limit: 5 })
    ]).then(function(results) {
        var m = results[0];
        var queued = (results[1] && results[1].reviews) || [];
        var pending = (results[2] && results[2].reviews) || [];
        var running = (results[3] && results[3].reviews) || [];
        renderDashboardContent(m, queued, pending, running);
        setupDashboardWebSocketHandlers();
    }).catch(function() {
        app.innerHTML = '<div class="empty">Failed to load dashboard data</div>';
    });
}

function renderDashboardContent(metrics, queuedReviews, pendingReviews, runningReviews) {
    var app = document.getElementById("app");
    dashboardState.metrics = metrics;
    dashboardState.queuedReviews = queuedReviews || [];
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
        activeReviewsSection(queuedReviews, pendingReviews, runningReviews) +
        '</div>';
    app.innerHTML = html;
}

function updateMetricsCards(metrics) {
    if (!metrics) return;

    var totalEl = document.querySelector(".stat-card:nth-child(1) .value");
    var successEl = document.querySelector(".stat-card:nth-child(2) .value");
    var durationEl = document.querySelector(".stat-card:nth-child(3) .value");
    var todayEl = document.querySelector(".stat-card:nth-child(4) .value");

    if (totalEl) totalEl.textContent = metrics.total_reviews || 0;
    if (successEl) successEl.textContent = formatPercent(
        metrics.success_count || 0,
        metrics.total_reviews || 0
    );
    if (durationEl) durationEl.textContent = formatDuration(metrics.avg_duration_ms || 0);
    if (todayEl) todayEl.textContent = metrics.reviews_today || 0;
}

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

            // Check queued reviews
            if (!wasActive) {
                for (var i = 0; i < dashboardState.queuedReviews.length; i++) {
                    if (dashboardState.queuedReviews[i].run_id === msg.run_id) {
                        wasActive = true;
                        removeIdx = i;
                        break;
                    }
                }
                if (wasActive) {
                    dashboardState.queuedReviews.splice(removeIdx, 1);
                }
            }

            // If review is now active, add it to the right list
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
            } else if (newStatus === "queued") {
                dashboardState.queuedReviews.unshift(reviewData);
                if (dashboardState.queuedReviews.length > 5) {
                    dashboardState.queuedReviews = dashboardState.queuedReviews.slice(0, 5);
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

// --- Dashboard aggregate helpers ---

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

function activeReviewsSection(queued, pending, running) {
    var all = (queued || []).concat(running || []).concat(pending || []);
    if (!all.length) {
        return '<div class="empty">No active reviews</div>';
    }

    var html = '<div class="active-reviews">';
    all.forEach(function(r) {
        var ru = repoGitHubUrl(r.repo);
        var pl = prUrl(r.repo, r.pr_number);
        var repoSpan = ru
            ? '<a href="' + esc(ru) + '" target="_blank" rel="noopener" class="repo-link">' + esc(basename(r.repo)) + '</a>'
            : '<span>' + esc(basename(r.repo)) + '</span>';
        var prSpan = pl
            ? '<a href="' + esc(pl) + '" target="_blank" rel="noopener" class="pr-link">#' + (r.pr_number || '-') + '</a>'
            : '<span>#' + (r.pr_number || '-') + '</span>';
        html += '<div class="active-review-card">' +
            '<span class="run-id">' + esc(truncate(r.run_id, 8)) + '</span>' +
            '<span class="' + badgeClass(r.status) + '">' + esc(r.status) + '</span>' +
            '<div class="review-info">' +
            repoSpan + prSpan +
            '</div>' +
            '<span>' + formatDuration(r.duration_ms) + '</span>' +
            '<a href="#/reviews/' + r.run_id + '" class="view-link">View</a>' +
            '</div>';
    });
    html += '</div>';
    return html;
}

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

function updateActiveReviews() {
    var container = document.querySelector(".active-reviews");
    if (!container) return;

    var all = dashboardState.queuedReviews.concat(dashboardState.runningReviews).concat(dashboardState.pendingReviews);
    if (!all.length) {
        var wrap = container.parentElement;
        if (wrap) wrap.innerHTML = '<div class="empty">No active reviews</div>';
        return;
    }

    var html = "";
    all.forEach(function(r) {
        var ru = repoGitHubUrl(r.repo);
        var pl = prUrl(r.repo, r.pr_number);
        var repoSpan = ru
            ? '<a href="' + esc(ru) + '" target="_blank" rel="noopener" class="repo-link">' + esc(basename(r.repo)) + '</a>'
            : '<span>' + esc(basename(r.repo)) + '</span>';
        var prSpan = pl
            ? '<a href="' + esc(pl) + '" target="_blank" rel="noopener" class="pr-link">#' + (r.pr_number || '-') + '</a>'
            : '<span>#' + (r.pr_number || '-') + '</span>';
        html += '<div class="active-review-card">' +
            '<span class="run-id">' + esc(truncate(r.run_id, 8)) + '</span>' +
            '<span class="' + badgeClass(r.status) + '">' + esc(r.status) + '</span>' +
            '<div class="review-info">' +
            repoSpan + prSpan +
            '</div>' +
            '<span>' + formatDuration(r.duration_ms) + '</span>' +
            '<a href="#/reviews/' + r.run_id + '" class="view-link">View</a>' +
            '</div>';
    });
    container.innerHTML = html;
}

function statCard(label, value) {
    return '<div class="stat-card"><div class="label">' + esc(label) + '</div><div class="value">' + esc(String(value)) + '</div></div>';
}

export { renderDashboard, cleanupDashboardHandlers };
