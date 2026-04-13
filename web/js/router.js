import Auth from "./auth.js";
import { renderDashboard, cleanupDashboardHandlers } from "./views/dashboard.js";
import { renderReviewsPage, cleanupReviewsPageHandlers } from "./views/reviews.js";
import { renderReviewDetail } from "./views/detail.js";

// =========================================================
// Router
// =========================================================
function route() {
    if (!Auth.checked() || (Auth.authEnabled() && !Auth.authenticated())) return;

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

function updateNav(hash) {
    document.querySelectorAll("nav a[data-nav]").forEach(function(a) {
        a.classList.toggle("active", hash.indexOf(a.getAttribute("data-nav")) !== -1);
    });
}

window.addEventListener("hashchange", route);

export { route };
