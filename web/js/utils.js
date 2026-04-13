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
