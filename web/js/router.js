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

    var container = document.querySelector(".container");

    if (hash === "#/" || hash === "" || hash === "#/dashboard") {
        if (container) container.classList.remove("container-fluid");
        renderDashboard();
    } else if (hash === "#/reviews") {
        if (container) container.classList.remove("container-fluid");
        renderReviewsPage();
    } else {
        var match = hash.match(/^#\/reviews\/(.+)$/);
        if (match) {
            if (container) container.classList.add("container-fluid");
            renderReviewDetail(match[1]);
        } else {
            if (container) container.classList.remove("container-fluid");
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
