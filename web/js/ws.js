var ws = {
    conn: null,
    reconnectDelay: 1000,
    maxReconnectDelay: 30000,
    handlers: {},
    subscriptions: {},
    reconnectTimer: null,
    _onopen: null,
    _onclose: null,

    _getSessionToken: function() {
        var match = document.cookie.match(/(?:^|;\s*)nano_session_token=([^;]*)/);
        return match ? decodeURIComponent(match[1]) : null;
    },

    connect: function() {
        var protocol = location.protocol === "https:" ? "wss:" : "ws:";
        var url = protocol + "//" + location.host + "/ws";

        // Pass session token as query parameter for WebSocket auth.
        var token = ws._getSessionToken();
        if (token) {
            url += "?token=" + encodeURIComponent(token);
        }
        try {
            ws.conn = new WebSocket(url);
        } catch(e) {
            ws.conn = null;
            ws._scheduleReconnect();
            return;
        }
        ws.conn.onopen = function() {
            ws.reconnectDelay = 1000;
            // Re-subscribe to active topics
            for (var topic in ws.subscriptions) {
                if (ws.subscriptions[topic]) {
                    ws._send({type: "subscribe", topic: topic});
                }
            }
            if (ws._onopen) ws._onopen();
        };
        ws.conn.onclose = function() {
            ws.conn = null;
            ws._scheduleReconnect();
            if (ws._onclose) ws._onclose();
        };
        ws.conn.onmessage = function(e) {
            try {
                var msg = JSON.parse(e.data);
                if (msg.type && ws.handlers[msg.type]) {
                    ws.handlers[msg.type].forEach(function(fn) { fn(msg); });
                }
            } catch(err) {}
        };
    },

    _scheduleReconnect: function() {
        if (ws.reconnectTimer) return;
        ws.reconnectTimer = setTimeout(function() {
            ws.reconnectTimer = null;
            ws.connect();
        }, ws.reconnectDelay);
        ws.reconnectDelay = Math.min(ws.reconnectDelay * 2, ws.maxReconnectDelay);
    },

    _send: function(obj) {
        if (ws.conn && ws.conn.readyState === WebSocket.OPEN) {
            ws.conn.send(JSON.stringify(obj));
        }
    },

    subscribe: function(topic) {
        ws.subscriptions[topic] = true;
        ws._send({type: "subscribe", topic: topic});
    },

    unsubscribe: function(topic) {
        delete ws.subscriptions[topic];
        ws._send({type: "unsubscribe", topic: topic});
    },

    on: function(type, fn) {
        if (!ws.handlers[type]) ws.handlers[type] = [];
        ws.handlers[type].push(fn);
    },

    off: function(type, fn) {
        if (!ws.handlers[type]) return;
        ws.handlers[type] = ws.handlers[type].filter(function(f) { return f !== fn; });
    },

    disconnect: function() {
        if (ws.reconnectTimer) {
            clearTimeout(ws.reconnectTimer);
            ws.reconnectTimer = null;
        }
        ws.subscriptions = {};
        ws.handlers = {};
        if (ws.conn) {
            ws.conn.onclose = null;
            ws.conn.close();
            ws.conn = null;
        }
    }
};

ws.connect();

export default ws;
