import ws from "../ws.js";
import api from "../api.js";
import { esc, truncate, formatDuration, badgeClass, formatDate, basename } from "../utils.js";
import StreamRenderer from "../stream-renderer.js";
import { renderMarkdown } from "../markdown.js";

var app = document.getElementById("app");

function renderReviewDetail(runId) {
    app.innerHTML = '<div class="loading">Loading review</div>';
    ws.unsubscribe("all");

    api.getReview(runId).then(function(r) {
        if (!r) {
            app.innerHTML = '<div class="empty">Review not found</div>';
            return;
        }
        if (r.status === "pending" || r.status === "running") {
            renderStreamingView(runId, r);
        } else {
            renderDetailContent(r, r.claude_output || null);
        }
    }).catch(function() {
        app.innerHTML = '<div class="empty">Failed to load review</div>';
    });
}

function renderStreamingView(runId, review) {
    var html = '<div class="detail-header">' +
        '<button class="btn" onclick="location.hash=\'#/reviews\'">&#8592; Back</button>' +
        '<h1>' + esc(truncate(review.run_id, 12)) + '</h1>' +
        '<span class="' + badgeClass(review.status) + '" id="detail-status">' + esc(review.status) + '</span>' +
        '</div>';

    html += '<div class="meta-grid" id="detail-meta">' +
        metaCard("Repo", basename(review.repo)) +
        metaCard("PR Number", "#" + (review.pr_number || "-")) +
        metaCard("Status", review.status) +
        metaCard("Base Branch", review.base_branch || "-") +
        metaCard("Head Branch", review.head_branch || "-") +
        metaCard("Created", formatDate(review.created_at)) +
        '</div>';

    html += '<div class="output-section">' +
        '<div class="output-header">' +
        '<span><span class="streaming-indicator" id="live-indicator"></span>Claude Output (live)</span>' +
        '</div>' +
        '<div class="output-body" id="stream-output" style="max-height:600px;overflow:auto">' +
        '<div class="stream-content" id="stream-content">Waiting for output...</div>' +
        '<button class="scroll-bottom-btn" id="scroll-bottom" onclick="document.getElementById(\'stream-output\').scrollTop=999999">&#8595;</button>' +
        '</div></div>';

    app.innerHTML = html;

    var container = document.getElementById("stream-output");
    var contentEl = document.getElementById("stream-content");
    var renderer = new StreamRenderer(container, contentEl);

    ws.subscribe("run:" + runId);

    var onStream = function(msg) {
        if (contentEl.textContent === "Waiting for output...") {
            contentEl.textContent = "";
        }
        if (msg.data) renderer.appendData(msg.data);
    };
    var onDone = function() {
        ws.off("stream", onStream);
        ws.off("stream_done", onDone);
        ws.off("review_update", onUpdate);
        ws.unsubscribe("run:" + runId);
        var indicator = document.getElementById("live-indicator");
        if (indicator) indicator.style.display = "none";
        renderer.flushPending();
        api.getReview(runId).then(function(r) {
            if (r) {
                var badge = document.getElementById("detail-status");
                if (badge) {
                    badge.className = badgeClass(r.status);
                    badge.textContent = r.status;
                }
            }
        }).catch(function() {});
    };
    var onUpdate = function(msg) {
        if (msg.run_id !== runId) return;
        var badge = document.getElementById("detail-status");
        if (badge && msg.status) {
            badge.className = badgeClass(msg.status);
            badge.textContent = msg.status;
        }
    };

    ws.on("stream", onStream);
    ws.on("stream_done", onDone);
    ws.on("review_update", onUpdate);
}

function renderDetailContent(r, streamOutput) {
    var html = '<div class="detail-header">' +
        '<button class="btn" onclick="location.hash=\'#/reviews\'">&#8592; Back</button>' +
        '<h1>' + esc(truncate(r.run_id, 12)) + '</h1>' +
        '<span class="' + badgeClass(r.status) + '">' + esc(r.status) + '</span>' +
        '</div>';

    html += '<div class="meta-grid">' +
        metaCard("Repo", basename(r.repo)) +
        metaCard("PR Number", "#" + (r.pr_number || "-")) +
        metaCard("Status", r.status) +
        metaCard("Conclusion", r.conclusion || "-") +
        metaCard("Base Branch", r.base_branch || "-") +
        metaCard("Head Branch", r.head_branch || "-") +
        metaCard("Duration", formatDuration(r.duration_ms)) +
        metaCard("Attempts", String(r.attempts || 0)) +
        metaCard("Created", formatDate(r.created_at)) +
        metaCard("Completed", formatDate(r.completed_at)) +
        '</div>';

    // Check if we have structured stream data for rich rendering
    var isStructured = streamOutput && streamOutput.indexOf('{"type":"') !== -1;

    html += '<div class="output-section">' +
        '<div class="output-header">' +
        '<span>Claude Output</span>' +
        '<button class="btn" onclick="this.closest(\'.output-section\').querySelector(\'.output-body\').classList.toggle(\'collapsed\');this.textContent=this.textContent===\'Expand\'?\'Collapse\':\'Expand\'">' +
        (streamOutput && streamOutput.length > 2000 ? "Expand" : "Collapse") +
        '</button>' +
        '</div>' +
        '<div class="output-body' + (streamOutput && streamOutput.length > 2000 ? ' collapsed' : '') + '">';

    if (isStructured) {
        html += '<div class="stream-content" id="stream-content"></div>';
    } else {
        html += '<pre>' + esc(r.claude_output || "No output available") + '</pre>';
    }

    html += '</div></div>';

    app.innerHTML = html;

    // Render structured stream data
    if (isStructured) {
        var container = document.querySelector("#stream-content");
        if (!container) return;
        var outputBody = container.closest(".output-body");
        var renderer = new StreamRenderer(outputBody || container, container);
        renderer.maxRenderedBlocks = 99999; // Show all for completed reviews
        renderer.appendData(streamOutput);
        renderer.flushPending();
    }
}

function metaCard(label, value) {
    return '<div class="meta-card"><div class="label">' + esc(label) + '</div><div class="value">' + esc(String(value)) + '</div></div>';
}

export { renderReviewDetail };
