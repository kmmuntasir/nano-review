import Auth from "./auth.js";
import { route } from "./router.js";

// --- Theme ---
var themeBtn = document.getElementById("theme-toggle");
var sunIcon = "\u2600";
var moonIcon = "\uD83C\uDF19";

function setTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    themeBtn.textContent = theme === "dark" ? sunIcon : moonIcon;
    localStorage.setItem("theme", theme);
}

(function() {
    var saved = localStorage.getItem("theme");
    if (saved) {
        setTheme(saved);
    } else if (window.matchMedia("(prefers-color-scheme: dark)").matches) {
        setTheme("dark");
    } else {
        setTheme("light");
    }
})();

themeBtn.addEventListener("click", function() {
    var current = document.documentElement.getAttribute("data-theme");
    setTheme(current === "dark" ? "light" : "dark");
});

window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function(e) {
    if (!localStorage.getItem("theme")) {
        setTheme(e.matches ? "dark" : "light");
    }
});

// --- Init ---
var app = document.getElementById("app");

Auth.init().then(function() {
    if (!Auth.authEnabled()) {
        route();
        return;
    }
    if (!Auth.authenticated()) {
        app.innerHTML =
            '<div class="empty">' +
                '<h2>Welcome to Nano Review</h2>' +
                '<p style="margin-bottom:1rem;color:var(--text-secondary)">Sign in to view review activity.</p>' +
                '<button class="btn" id="init-login-btn">Sign in with Google</button>' +
            '</div>';
        var btn = document.getElementById("init-login-btn");
        if (btn) btn.addEventListener("click", function() { Auth.login(); });
        return;
    }
    route();
});
