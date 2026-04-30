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
        statCard("Total Reviews", metrics.total_reviews || 0, "cyan") +
        statCard("Success Rate", formatPercent(metrics.success_count || 0, metrics.total_reviews || 0), "green") +
        statCard("Avg Duration", formatDuration(metrics.avg_duration_ms || 0), "purple") +
        statCard("Reviews Today", metrics.reviews_today || 0, "blue") +
        '</div>' +
        '<div style="display:grid; grid-template-columns: 1fr; gap: 24px; margin-bottom: 24px;">' +
        '<div class="table-wrap" style="margin-top: 0;">' +
        '<div class="table-header"><h2>Status Distribution</h2></div>' +
        statusBar(metrics) +
        '</div>' +
        '<div class="table-wrap" style="margin-top: 0;">' +
        '<div class="table-header"><h2>Active Reviews</h2></div>' +
        '<div style="overflow-x:auto;">' +
        activeReviewsSection(queuedReviews, pendingReviews, runningReviews) +
        '</div>' +
        '</div>' +
        '</div>';
    app.innerHTML = html;
    
    if (window.Chart) {
        initDonutChart();
        updateStatusDistribution();
    }
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
        var reviewData = msg.review || {
            run_id: msg.run_id,
            status: msg.status,
            conclusion: msg.conclusion,
            duration_ms: msg.duration_ms
        };
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
    return '<div style="display:flex; flex-direction:column; align-items:center; gap:24px; padding:20px;">' +
        '<div style="position:relative; width: 200px; height: 200px;"><canvas id="donutChart"></canvas>' +
        '<div style="position:absolute; inset:0; display:flex; align-items:center; justify-content:center; pointer-events:none;"><span style="font-size:32px; font-weight:700;" id="donutCenterText"></span></div></div>' +
        '<div id="donutLegend" style="display:flex; flex-wrap:wrap; gap:16px; justify-content:center;"></div>' +
        '</div>';
}

function activeReviewsSection(queued, pending, running) {
    var all = (queued || []).concat(running || []).concat(pending || []);
    if (!all.length) {
        return '<div class="empty">No active reviews</div>';
    }

    var html = '<table><thead><tr><th>Run ID</th><th>Status</th><th>Repo</th><th>PR</th><th>Duration</th><th></th></tr></thead><tbody>';
    all.forEach(function(r) {
        var ru = repoGitHubUrl(r.repo);
        var pl = prUrl(r.repo, r.pr_number);
        var repoSpan = ru
            ? '<a href="' + esc(ru) + '" target="_blank" rel="noopener" class="repo-link">' + esc(basename(r.repo)) + '</a>'
            : '<span>' + esc(basename(r.repo)) + '</span>';
        var prSpan = pl
            ? '<a href="' + esc(pl) + '" target="_blank" rel="noopener" class="pr-link">#' + (r.pr_number || '-') + '</a>'
            : '<span>#' + (r.pr_number || '-') + '</span>';
        
        var pulseHtml = r.status === 'running' ? '<div style="display:flex;gap:2px;margin-right:6px;"><div class="stream-block__status-dot"></div><div class="stream-block__status-dot" style="animation-delay:0.2s"></div><div class="stream-block__status-dot" style="animation-delay:0.4s"></div></div>' : '';

        html += '<tr>' +
            '<td class="run-id">' + esc(truncate(r.run_id, 8)) + '</td>' +
            '<td><span class="' + badgeClass(r.status) + '">' + pulseHtml + esc(r.status) + '</span></td>' +
            '<td><div style="display:flex;align-items:center;gap:6px;"><i class="ph ph-github-logo"></i>' + repoSpan + '</div></td>' +
            '<td>' + prSpan + '</td>' +
            '<td style="color:var(--text-secondary);">' + formatDuration(r.duration_ms) + '</td>' +
            '<td style="text-align:right;"><a href="#/reviews/' + r.run_id + '" class="view-link"><i class="ph ph-arrow-right text-lg"></i></a></td>' +
            '</tr>';
    });
    html += '</tbody></table>';
    return html;
}

let chartInstance = null;
function initDonutChart() {
    var ctx = document.getElementById('donutChart');
    if (!ctx) return;
    
    // Check if chart already exists and destroy it
    if (chartInstance) {
        chartInstance.destroy();
    }
    
    var style = getComputedStyle(document.body);
    var colorSuccess = style.getPropertyValue('--success').trim() || '#10B981';
    var colorBlue = style.getPropertyValue('--brand-blue').trim() || '#3B82F6';
    var colorDanger = style.getPropertyValue('--danger').trim() || '#EF4444';
    var colorWarning = style.getPropertyValue('--warning').trim() || '#F59E0B';
    
    chartInstance = new Chart(ctx, {
        type: 'doughnut',
        data: {
            labels: ['Completed', 'Active', 'Failed', 'Other'],
            datasets: [{
                data: [0, 0, 0, 0],
                backgroundColor: [colorSuccess, colorBlue, colorDanger, colorWarning],
                borderWidth: 0,
                hoverOffset: 4
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            cutout: '80%',
            plugins: {
                legend: { display: false }
            }
        }
    });
}

function updateStatusDistribution() {
    var legendContainer = document.getElementById("donutLegend");
    var centerText = document.getElementById("donutCenterText");
    if (!dashboardState.metrics) return;

    var m = dashboardState.metrics;
    var total = m.total_reviews || 0;
    
    var completed = m.success_count || 0;
    var failed = m.failure_count || 0;
    var timedOut = m.timed_out_count || 0;
    var cancelled = m.cancelled_count || 0;
    var active = total - completed - failed - timedOut - cancelled;

    if (centerText) centerText.textContent = total;

    if (chartInstance) {
        chartInstance.data.datasets[0].data = [completed, active, failed, timedOut + cancelled];
        chartInstance.update();
    }

    if (legendContainer) {
        var style = getComputedStyle(document.body);
        var colorSuccess = style.getPropertyValue('--success').trim() || '#10B981';
        var colorBlue = style.getPropertyValue('--brand-blue').trim() || '#3B82F6';
        var colorDanger = style.getPropertyValue('--danger').trim() || '#EF4444';
        var colorWarning = style.getPropertyValue('--warning').trim() || '#F59E0B';
        
        legendContainer.innerHTML = 
            '<div style="display:flex; align-items:center; gap:6px;"><span style="width:10px;height:10px;border-radius:50%;background:'+colorSuccess+';"></span><span style="font-size:13px;color:var(--text-secondary)">Completed (' + completed + ')</span></div>' +
            '<div style="display:flex; align-items:center; gap:6px;"><span style="width:10px;height:10px;border-radius:50%;background:'+colorBlue+';"></span><span style="font-size:13px;color:var(--text-secondary)">Active (' + active + ')</span></div>' +
            '<div style="display:flex; align-items:center; gap:6px;"><span style="width:10px;height:10px;border-radius:50%;background:'+colorDanger+';"></span><span style="font-size:13px;color:var(--text-secondary)">Failed (' + failed + ')</span></div>' +
            '<div style="display:flex; align-items:center; gap:6px;"><span style="width:10px;height:10px;border-radius:50%;background:'+colorWarning+';"></span><span style="font-size:13px;color:var(--text-secondary)">Other (' + (timedOut+cancelled) + ')</span></div>';
    }
}

function updateActiveReviews() {
    var container = document.querySelector(".table-wrap table tbody");
    if (!container) return; // Might be empty div

    var all = dashboardState.queuedReviews.concat(dashboardState.runningReviews).concat(dashboardState.pendingReviews);
    if (!all.length) {
        var wrap = container.closest('.table-wrap');
        if (wrap) wrap.innerHTML = '<div class="table-header"><h2>Active Reviews</h2></div><div class="empty">No active reviews</div>';
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
        
        var pulseHtml = r.status === 'running' ? '<div style="display:flex;gap:2px;margin-right:6px;"><div class="stream-block__status-dot"></div><div class="stream-block__status-dot" style="animation-delay:0.2s"></div><div class="stream-block__status-dot" style="animation-delay:0.4s"></div></div>' : '';

        html += '<tr>' +
            '<td class="run-id">' + esc(truncate(r.run_id, 8)) + '</td>' +
            '<td><span class="' + badgeClass(r.status) + '">' + pulseHtml + esc(r.status) + '</span></td>' +
            '<td><div style="display:flex;align-items:center;gap:6px;"><i class="ph ph-github-logo"></i>' + repoSpan + '</div></td>' +
            '<td>' + prSpan + '</td>' +
            '<td style="color:var(--text-secondary);">' + formatDuration(r.duration_ms) + '</td>' +
            '<td style="text-align:right;"><a href="#/reviews/' + r.run_id + '" class="view-link"><i class="ph ph-arrow-right text-lg"></i></a></td>' +
            '</tr>';
    });
    container.innerHTML = html;
}

function statCard(label, value, colorType) {
    var svg = "";
    if (colorType === "cyan") {
        svg = '<div class="sparkline"><svg viewBox="0 0 100 50" class="w-full h-full preserve-3d" preserveAspectRatio="none"><defs><linearGradient id="grad-cyan" x1="0%" y1="0%" x2="0%" y2="100%"><stop offset="0%" stop-color="#06B6D4" stop-opacity="0.3" /><stop offset="100%" stop-color="#06B6D4" stop-opacity="0" /></linearGradient></defs><path d="M0 40 Q 10 35, 20 40 T 40 30 T 60 40 T 80 20 T 100 10" fill="none" stroke="#06B6D4" stroke-width="2.5"/><path d="M0 40 Q 10 35, 20 40 T 40 30 T 60 40 T 80 20 T 100 10 L 100 50 L 0 50 Z" fill="url(#grad-cyan)" /></svg></div>';
    } else if (colorType === "green") {
        svg = '<div class="sparkline"><svg viewBox="0 0 100 50" class="w-full h-full" preserveAspectRatio="none"><defs><linearGradient id="grad-green" x1="0%" y1="0%" x2="0%" y2="100%"><stop offset="0%" stop-color="#10B981" stop-opacity="0.3" /><stop offset="100%" stop-color="#10B981" stop-opacity="0" /></linearGradient></defs><path d="M0 45 Q 15 40, 25 30 T 50 35 T 70 20 T 90 25 T 100 15" fill="none" stroke="#10B981" stroke-width="2.5"/><path d="M0 45 Q 15 40, 25 30 T 50 35 T 70 20 T 90 25 T 100 15 L 100 50 L 0 50 Z" fill="url(#grad-green)" /></svg></div>';
    } else if (colorType === "purple") {
        svg = '<div class="sparkline"><svg viewBox="0 0 100 50" class="w-full h-full" preserveAspectRatio="none"><defs><linearGradient id="grad-purple" x1="0%" y1="0%" x2="0%" y2="100%"><stop offset="0%" stop-color="#A855F7" stop-opacity="0.3" /><stop offset="100%" stop-color="#A855F7" stop-opacity="0" /></linearGradient></defs><path d="M0 35 Q 10 45, 25 30 T 45 40 T 65 15 T 85 30 T 100 20" fill="none" stroke="#A855F7" stroke-width="2.5"/><path d="M0 35 Q 10 45, 25 30 T 45 40 T 65 15 T 85 30 T 100 20 L 100 50 L 0 50 Z" fill="url(#grad-purple)" /></svg></div>';
    } else if (colorType === "blue") {
        svg = '<div class="sparkline"><svg viewBox="0 0 100 50" class="w-full h-full" preserveAspectRatio="none"><defs><linearGradient id="grad-blue" x1="0%" y1="0%" x2="0%" y2="100%"><stop offset="0%" stop-color="#3B82F6" stop-opacity="0.3" /><stop offset="100%" stop-color="#3B82F6" stop-opacity="0" /></linearGradient></defs><path d="M0 40 Q 20 40, 40 25 T 60 30 T 80 15 T 100 5" fill="none" stroke="#3B82F6" stroke-width="2.5"/><path d="M0 40 Q 20 40, 40 25 T 60 30 T 80 15 T 100 5 L 100 50 L 0 50 Z" fill="url(#grad-blue)" /></svg></div>';
    }
    
    return '<div class="stat-card hover-' + (colorType || '') + '"><div class="relative"><div class="label">' + esc(label) + '</div><div class="value">' + esc(String(value)) + '</div></div>' + svg + '</div>';
}

export { renderDashboard, cleanupDashboardHandlers };
