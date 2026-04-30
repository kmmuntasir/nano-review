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

export function repoGitHubUrl(repo) {
    if (!repo) return "";
    var s = repo.replace(/\.git$/, "");
    var m = s.match(/git@github\.com:([^/]+\/[^/]+)/);
    if (m) return "https://github.com/" + m[1];
    m = s.match(/https?:\/\/github\.com\/([^/]+\/[^/]+)/);
    if (m) return "https://github.com/" + m[1];
    return "";
}

export function prUrl(repo, prNumber) {
    var base = repoGitHubUrl(repo);
    if (!base || !prNumber) return "";
    return base + "/pull/" + prNumber;
}

export function timeAgo(iso) {
    if (!iso) return "-";
    var now = Date.now();
    var then = new Date(iso).getTime();
    var diff = Math.floor((now - then) / 1000);
    if (diff < 0) diff = 0;
    if (diff < 60) return "just now";
    if (diff < 3600) return Math.floor(diff / 60) + "m ago";
    if (diff < 86400) return Math.floor(diff / 3600) + "h ago";
    if (diff < 172800) return "Yesterday";
    var d = new Date(iso);
    var months = ["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"];
    return months[d.getMonth()] + " " + d.getDate();
}
