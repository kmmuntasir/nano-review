import ws from "../ws.js";
import api from "../api.js";
import { esc, truncate, basename, formatDuration, formatDate, badgeClass } from "../utils.js";

var app = document.getElementById("app");

// --- Review list view ---
var reviewsPageState = {
    status: "",
    repo: "",
    offset: 0,
    reviews: [],
    loading: false,
    onReviewUpdate: null
};

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

function renderReviewsPageContent() {
    var html = '<div class="table-wrap">' +
        '<div class="table-header">' +
        '<h2>Reviews</h2>' +
        '<div class="filters">' +
        '<select id="status-filter" onchange="window.__applyFilter()">' +
        '<option value="">All statuses</option>' +
        '<option value="pending"' + (reviewsPageState.status === "pending" ? " selected" : "") + '>Pending</option>' +
        '<option value="queued"' + (reviewsPageState.status === "queued" ? " selected" : "") + '>Queued</option>' +
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

function updateReviewsPageTable() {
    var tbody = document.getElementById("reviews-table-body");
    if (!tbody) return;
    var html = "";
    for (var i = 0; i < reviewsPageState.reviews.length; i++) {
        html += reviewRow(reviewsPageState.reviews[i]);
    }
    tbody.innerHTML = html;
}

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

function cleanupReviewsPageHandlers() {
    if (reviewsPageState.onReviewUpdate) {
        ws.off("review_update", reviewsPageState.onReviewUpdate);
        reviewsPageState.onReviewUpdate = null;
    }
}

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

export { renderReviewsPage, cleanupReviewsPageHandlers };
