import ws from "../ws.js";
import api from "../api.js";
import { esc, truncate, formatDuration, badgeClass, formatDetailedDate, basename, repoGitHubUrl, prUrl } from "../utils.js";
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

function statCardSimple(label, value) {
    return '<div class="meta-card" style="text-align:center; display:flex; flex-direction:column; justify-content:center; padding:16px; box-shadow:var(--shadow);"><div class="label" style="font-size:13px; font-weight:500;">' + esc(label) + '</div><div class="value" style="font-size:24px; font-weight:700;">' + esc(String(value)) + '</div></div>';
}

function leftColContent(r) {
    var title = '<h1 style="font-size:22px; font-weight:600; color:var(--text); margin-bottom:4px;">Detailed AI Review Analysis</h1>';
    
    var summaryCard = '<div class="meta-card" style="display:flex; flex-direction:column; gap:20px; box-shadow:var(--shadow);">' +
        '<div style="display:flex; justify-content:space-between; align-items:center;">' +
        '<h2 style="font-size:17px; font-weight:600; margin:0; color:var(--text);">PR Summary</h2>' +
        '<span class="' + badgeClass(r.status) + '" id="detail-status" style="font-size:10px; text-transform:uppercase; letter-spacing:0.1em; padding:2px 8px;">' + esc(r.status) + '</span>' +
        '</div>' +
        '<div style="display:grid; grid-template-columns: 1fr; gap:12px;">' +
        '<div><div class="label" style="font-size:11px; margin-bottom:6px;">REPO:</div><div style="font-size:14px; display:flex; align-items:center; gap:6px;"><i class="ph ph-github-logo text-lg text-text-secondary"></i>' + (repoGitHubUrl(r.repo) ? '<a href="'+esc(repoGitHubUrl(r.repo))+'" target="_blank" style="color:var(--text); font-weight:500; text-decoration:none;">'+esc(basename(r.repo))+'</a>' : '<span style="color:var(--text); font-weight:500;">'+esc(basename(r.repo))+'</span>') + '</div></div>' +
        '<div><div class="label" style="font-size:11px; margin-bottom:6px;">BRANCH:</div><div style="font-size:14px; font-weight:500; color:var(--text); display:flex; align-items:center; gap:6px;">' + esc(r.base_branch||'-') + ' <i class="ph ph-arrow-left" style="color:var(--text-secondary)"></i> ' + esc(truncate(r.head_branch||'-', 12)) + '</div></div>' +
        '<div><div class="label" style="font-size:11px; margin-bottom:6px;">CREATED:</div><div style="font-size:14px; font-weight:500; color:var(--text);">' + esc(formatDetailedDate(r.created_at)) + '</div></div>' +
        '</div>' +
        '</div>';

    var statsGrid = '<div style="display:grid; grid-template-columns: 1fr 1fr; gap:16px; margin-top:4px;">' +
        statCardSimple("Duration", formatDuration(r.duration_ms)) +
        statCardSimple("Attempts", r.attempts || 1) +
        statCardSimple("PR Number", r.pr_number ? "#"+r.pr_number : "-") +
        statCardSimple("Conclusion", r.conclusion || "-") +
        '</div>';

    return '<div class="detail-col-left">' + title + summaryCard + statsGrid + '</div>';
}

function renderStreamingView(runId, review) {
    var html = '<div class="detail-layout" style="height:calc(100vh - 112px)">';

    html += leftColContent(review);
    
    html += '<div class="detail-col-right" style="min-height:0">' +
        '<div class="detail-header">' +
        '<h1 style="visibility:hidden; font-size:22px; margin:0;">Live Log</h1>' + // balance height
        '<button class="btn" style="display:flex; align-items:center; gap:8px;" onclick="location.hash=\'#/reviews\'"><i class="ph ph-arrow-left"></i> Back</button>' +
        '</div>' +
        '<div class="output-section" style="flex:1; min-height:0; display:flex; flex-direction:column;">' +
        '<div class="output-header" style="padding:20px;"><h2 style="font-size:17px; font-weight:600; margin:0; display:flex; align-items:center; gap:8px;"><span class="streaming-indicator" id="live-indicator"></span>Live Analysis Log</h2></div>' +
        '<div style="padding:20px; flex:1; min-height:0; display:flex; flex-direction:column;">' +
        '<div class="output-body" id="stream-output" style="flex:1; min-height:0; overflow-y:auto; background:var(--code-bg); border:1px solid var(--border); border-radius:8px; position:relative;">' +
        '<div class="stream-content" id="stream-content" style="padding:20px;">Waiting for output...</div>' +
        '<button class="scroll-bottom-btn" id="scroll-bottom" onclick="document.getElementById(\'stream-output\').scrollTop=999999">&#8595;</button>' +
        '</div></div></div></div>';
        
    html += '</div>';

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
    var html = '<div class="detail-layout">';
    
    html += leftColContent(r);
    
    var isStructured = streamOutput && streamOutput.indexOf('{"type":"') !== -1;

    html += '<div class="detail-col-right">' +
        '<div class="detail-header">' +
        '<h1 style="visibility:hidden; font-size:22px; margin:0;">Analysis Log</h1>' +
        '<button class="btn" style="display:flex; align-items:center; gap:8px;" onclick="location.hash=\'#/reviews\'"><i class="ph ph-arrow-left"></i> Back</button>' +
        '</div>' +
        '<div class="output-section" style="flex:1; display:flex; flex-direction:column;">' +
        '<div class="output-header" style="padding:20px; display:flex; justify-content:space-between; align-items:center;"><h2 style="font-size:17px; font-weight:600; margin:0;">Claude Output</h2>' +
        '<button class="btn" onclick="var b=this.closest(\'.output-section\').querySelector(\'.output-body\'); b.classList.toggle(\'collapsed\'); this.textContent=b.classList.contains(\'collapsed\')?\'Expand\':\'Collapse\'">' +
        (streamOutput && streamOutput.length > 2000 ? "Expand" : "Collapse") +
        '</button></div>' +
        '<div style="padding:20px; flex:1; display:flex; flex-direction:column;">' +
        '<div class="output-body' + (streamOutput && streamOutput.length > 2000 ? ' collapsed' : '') + '" id="stream-output" style="flex:1; background:var(--code-bg); border:1px solid var(--border); border-radius:8px;">';

    if (isStructured) {
        html += '<div class="stream-content" id="stream-content" style="padding:20px;"></div>';
    } else {
        html += '<pre style="padding:20px; margin:0;">' + esc(r.claude_output || "No output available") + '</pre>';
    }

    html += '</div></div></div></div>';
        
    html += '</div>';

    app.innerHTML = html;

    if (isStructured) {
        var container = document.querySelector("#stream-content");
        if (!container) return;
        var outputBody = container.closest(".output-body");
        var renderer = new StreamRenderer(outputBody || container, container);
        renderer.maxRenderedBlocks = 99999;
        renderer.appendData(streamOutput);
        renderer.flushPending();
    }
}

export { renderReviewDetail };
