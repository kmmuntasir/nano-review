import { escapeHtml } from "./utils.js";

// --- Auth state management ---
var Auth = {
    _user: null,
    _checked: false,
    _authEnabled: true,
    _listeners: [],

    fetchSession: function() {
        return fetch("/auth/me").then(function(r) {
            if (r.ok) return r.json();
            return null;
        }).then(function(data) {
            if (data && data.auth_enabled === false) {
                Auth._authEnabled = false;
                Auth._user = null;
            } else {
                Auth._user = data;
            }
            Auth._checked = true;
            Auth._notify();
        }).catch(function() {
            Auth._user = null;
            Auth._checked = true;
            Auth._notify();
        });
    },

    login: function() {
        window.location.href = "/auth/login?state=" + encodeURIComponent(window.location.hash || "#/dashboard");
    },

    logout: function() {
        window.location.href = "/auth/logout";
    },

    user: function() { return Auth._user; },
    checked: function() { return Auth._checked; },
    authenticated: function() { return !!Auth._user; },
    authEnabled: function() { return Auth._authEnabled; },

    onChange: function(fn) { Auth._listeners.push(fn); },
    offChange: function(fn) { Auth._listeners = Auth._listeners.filter(function(f) { return f !== fn; }); },

    _notify: function() {
        Auth._listeners.forEach(function(fn) { fn(Auth._user); });
    }
};

Auth.init = function() {
    return Auth.fetchSession();
};

// --- Auth nav rendering ---
var authNav = document.getElementById("auth-nav");

export function renderAuthNav(user) {
    if (!Auth.authEnabled()) {
        authNav.innerHTML = "";
        return;
    }
    if (!user) {
        authNav.innerHTML =
            '<a href="#" class="btn-google-login" id="google-login-btn">' +
                '<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">' +
                    '<path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/>' +
                    '<path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>' +
                    '<path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/>' +
                    '<path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>' +
                '</svg>' +
                'Sign in with Google' +
            '</a>';
        document.getElementById("google-login-btn").addEventListener("click", function(e) {
            e.preventDefault();
            Auth.login();
        });
        return;
    }

    var name = user.name || user.email || user.id || "User";
    var initials = name.charAt(0).toUpperCase();
    var avatarHtml = user.picture
        ? '<div class="user-avatar"><img src="' + escapeHtml(user.picture) + '" alt=""></div>'
        : '<div class="user-avatar">' + escapeHtml(initials) + '</div>';

    authNav.innerHTML =
        '<div class="user-menu">' +
            '<button class="user-menu-btn" id="user-menu-btn">' +
                avatarHtml +
                '<span>' + escapeHtml(name) + '</span>' +
            '</button>' +
            '<div class="user-dropdown" id="user-dropdown">' +
                '<button class="logout-btn" id="logout-btn">Sign out</button>' +
            '</div>' +
        '</div>';

    document.getElementById("user-menu-btn").addEventListener("click", function(e) {
        e.stopPropagation();
        document.getElementById("user-dropdown").classList.toggle("open");
    });

    document.getElementById("logout-btn").addEventListener("click", function() {
        Auth.logout();
    });
}

function closeUserDropdown() {
    var dd = document.getElementById("user-dropdown");
    if (dd) dd.classList.remove("open");
}

document.addEventListener("click", closeUserDropdown);

Auth.onChange(renderAuthNav);
if (Auth.checked()) renderAuthNav(Auth.user());

export default Auth;
