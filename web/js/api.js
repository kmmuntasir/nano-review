import Auth from "./auth.js";

// --- API client with auth-aware fetch ---

function apiFetch(url, options) {
    options = options || {};
    return fetch(url, options).then(function(r) {
        if (r.status === 401 && Auth.authenticated()) {
            Auth._user = null;
            Auth._notify();
        }
        return r;
    });
}

var api = {
    getMetrics: function() {
        return apiFetch("/metrics").then(function(r) { return r.json(); });
    },
    listReviews: function(params) {
        var qs = new URLSearchParams(params).toString();
        return apiFetch("/reviews?" + qs).then(function(r) { return r.json(); });
    },
    getReview: function(id) {
        return apiFetch("/reviews/" + id).then(function(r) {
            if (r.status === 404) return null;
            return r.json();
        });
    }
};

export default api;
