export function escapeHtml(s) {
    var d = document.createElement("div");
    d.textContent = s;
    return d.innerHTML;
}

export function truncate(str, len) {
    if (!str) return "";
    if (str.length <= len) return str;
    return str.slice(0, len) + "...";
}

export function basename(url) {
    if (!url) return "";
    var parts = url.replace(/\/$/, "").split("/");
    return parts[parts.length - 1];
}

export function formatDuration(ms) {
    if (!ms) return "-";
    var s = Math.floor(ms / 1000);
    var m = Math.floor(s / 60);
    s = s % 60;
    var h = Math.floor(m / 60);
    m = m % 60;
    if (h > 0) return h + "h " + m + "m " + s + "s";
    if (m > 0) return m + "m " + s + "s";
    return s + "s";
}

export function formatDate(iso) {
    if (!iso) return "-";
    var d = new Date(iso);
    return d.toLocaleString();
}

export function formatPercent(part, total) {
    if (!total) return "0%";
    return Math.round((part / total) * 100) + "%";
}

export function badgeClass(status) {
    return "badge badge-" + (status || "pending");
}

export function esc(str) {
    var el = document.createElement("span");
    el.textContent = str;
    return el.innerHTML;
}

export function wordCount(text) {
    if (!text) return 0;
    return text.trim().split(/\s+/).filter(Boolean).length;
}

function computeVisiblePages(current, total, maxVisible) {
    if (total <= maxVisible) {
        var pages = [];
        for (var i = 1; i <= total; i++) pages.push(i);
        return pages;
    }
    var pages = [];
    var half = Math.floor(maxVisible / 2);
    var start = Math.max(2, current - half + 1);
    var end = Math.min(total - 1, start + maxVisible - 3);
    start = Math.max(2, end - maxVisible + 3);
    pages.push(1);
    if (start > 2) pages.push("...");
    for (var i = start; i <= end; i++) pages.push(i);
    if (end < total - 1) pages.push("...");
    pages.push(total);
    return pages;
}

export function renderPagination(currentPage, totalItems, pageSize) {
    var totalPages = Math.ceil(totalItems / pageSize);
    if (totalPages <= 1) return "";

    var html = '<div class="pagination">';
    html += '<button class="pagination-btn pagination-btn--nav"' +
        (currentPage <= 1 ? ' disabled' : ' onclick="window.__goToPage(' + (currentPage - 1) + ')"') +
        '>&laquo; Prev</button>';

    var pages = computeVisiblePages(currentPage, totalPages, 7);
    for (var i = 0; i < pages.length; i++) {
        var p = pages[i];
        if (p === "...") {
            html += '<span class="pagination-ellipsis">&hellip;</span>';
        } else if (p === currentPage) {
            html += '<button class="pagination-btn pagination-btn--active" disabled>' + p + '</button>';
        } else {
            html += '<button class="pagination-btn" onclick="window.__goToPage(' + p + ')">' + p + '</button>';
        }
    }

    html += '<button class="pagination-btn pagination-btn--nav"' +
        (currentPage >= totalPages ? ' disabled' : ' onclick="window.__goToPage(' + (currentPage + 1) + ')"') +
        '>Next &raquo;</button>';

    html += '</div>';
    return html;
}
