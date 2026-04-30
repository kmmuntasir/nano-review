// =========================================================
// StreamRenderer: render commands -> DOM
// =========================================================

import { esc, formatDuration, wordCount } from "./utils.js";
import { renderMarkdown, highlightCode } from "./markdown.js";
import StreamParser from "./stream-parser.js";

var TOOL_CATEGORIES = {
    "Bash": "terminal",
    "Read": "file",
    "Grep": "search",
    "Glob": "search",
    "Edit": "file",
    "Write": "file"
};

function getToolCategory(name) {
    if (TOOL_CATEGORIES.hasOwnProperty(name)) return TOOL_CATEGORIES[name];
    if (name && name.indexOf("mcp__github") === 0) return "github";
    return "tool";
}

var TOOL_ICONS = {
    terminal: "\u25B8",
    file: "\u25A3",
    search: "\u25C9",
    github: "\u25D0",
    tool: "\u2699"
};

function getToolSummary(name, input) {
    if (!input) return "";
    try {
        var parsed = typeof input === "string" ? JSON.parse(input) : input;
    } catch(e) { return ""; }
    switch(name) {
        case "Bash":
            var cmd = parsed.command || "";
            var firstLine = cmd.split("\n")[0];
            return esc(firstLine);
        case "Glob":
            return esc(parsed.pattern || "");
        case "Grep":
            return esc(parsed.pattern || "");
        case "Read":
            return esc(parsed.file_path || "");
        default:
            return "";
    }
}

function StreamRenderer(container, contentEl) {
    this.container = container;
    this.contentEl = contentEl;
    this.parser = new StreamParser();
    this.blockElements = {};
    this.isAutoScroll = true;
    this.accumulatedMarkdown = {};
    this.renderPendingTimer = null;
    this.pendingMarkdownBlocks = {};
    this.blockCount = 0;
    this.maxRenderedBlocks = 500;
    this.hiddenBlockCount = 0;
    this.metaElement = null;
    this.resultElement = null;
    this.isComplete = false;
    this._pendingRenders = {};
    this._setupScrollDetection();
}

StreamRenderer.prototype._setupScrollDetection = function() {
    var self = this;
    this.container.addEventListener("scroll", function() {
        var atBottom = self.container.scrollTop + self.container.clientHeight >= self.container.scrollHeight - 40;
        self.isAutoScroll = atBottom;
        var scrollBtn = document.getElementById("scroll-bottom");
        if (scrollBtn) {
            scrollBtn.style.display = atBottom ? "none" : "flex";
        }
    });
};

StreamRenderer.prototype.appendData = function(jsonlChunk) {
    var commands = this.parser.feed(jsonlChunk);
    for (var i = 0; i < commands.length; i++) {
        this._handleCommand(commands[i]);
    }
    this._autoScroll();
};

StreamRenderer.prototype.flushPending = function() {
    if (this.renderPendingTimer) {
        clearTimeout(this.renderPendingTimer);
        this.renderPendingTimer = null;
    }
    this._flushMarkdown();
};

StreamRenderer.prototype._handleCommand = function(cmd) {
    switch(cmd.type) {
        case "SYSTEM_INIT": this._renderMeta(cmd.data); break;
        case "BLOCK_START": this._renderBlockStart(cmd.data); break;
        case "BLOCK_DELTA": this._renderBlockDelta(cmd.data); break;
        case "BLOCK_END": this._renderBlockEnd(cmd.data); break;
        case "TOOL_RESULT": this._renderToolResult(cmd.data); break;
        case "RESULT": this._renderResult(cmd.data); break;
        case "RAW": this._renderRaw(cmd.data); break;
    }
};

StreamRenderer.prototype._renderMeta = function(data) {
    if (this.metaElement) return;
    var el = document.createElement("div");
    el.className = "stream-meta";
    el.innerHTML =
        '<span class="stream-meta-item"><span class="stream-meta-label">Model</span><span>' + esc(data.model) + '</span></span>' +
        '<span class="stream-meta-item"><span class="stream-meta-label">Version</span><span>' + esc(data.version) + '</span></span>' +
        (data.tools ? '<span class="stream-meta-item"><span class="stream-meta-label">Tools</span><span>' + data.tools + '</span></span>' : '');
    this.contentEl.appendChild(el);
    this.metaElement = el;
};

StreamRenderer.prototype._renderBlockStart = function(data) {
    var self = this;
    var idx = data.index;

    if (this.blockCount >= this.maxRenderedBlocks) {
        this.hiddenBlockCount++;
        this.blockCount++;
        return;
    }

    this.blockCount++;
    var wrapper = document.createElement("div");
    wrapper.className = "stream-block stream-block--" + data.blockType;
    wrapper.setAttribute("data-block-index", idx);

    if (data.blockType === "thinking") {
        var header = document.createElement("div");
        header.className = "stream-block__header is-collapsed";
        header.innerHTML = '<span class="stream-block__chevron"></span>' +
            '<span>Thinking</span>' +
            '<span class="stream-thinking-counter"></span>';
        var body = document.createElement("div");
        body.className = "stream-block__body is-hidden";
        this._makeCollapsible(header, body);
        wrapper.appendChild(header);
        wrapper.appendChild(body);
    } else if (data.blockType === "tool_use") {
        var isReadOnly = (data.name === "Read");
        var header = document.createElement("div");
        header.className = "stream-block__header" + (isReadOnly ? " is-collapsed" : "");
        if (isReadOnly) header.style.cursor = "default";
        var cat = getToolCategory(data.name);
        var icon = TOOL_ICONS[cat] || TOOL_ICONS.tool;
        var summary = getToolSummary(data.name, data.input);
        var summaryHtml = summary ? '<span class="stream-block__summary">' + summary + '</span>' : '';
        header.innerHTML = '<span class="stream-block__chevron"></span>' +
            '<span class="stream-block__badge">' + icon + ' ' + esc(data.name || "tool") + '</span>' +
            summaryHtml +
            '<span class="stream-block__status"><span class="stream-block__status-dot"></span>Running</span>';
        var body = document.createElement("div");
        body.className = "stream-block__body" + (isReadOnly ? " is-hidden" : "");
        if (!isReadOnly) {
            this._makeCollapsible(header, body);
        }
        wrapper.appendChild(header);
        wrapper.appendChild(body);
    }
    // text blocks have no header/body structure — just a markdown container

    this.contentEl.appendChild(wrapper);
    this.blockElements[idx] = wrapper;
};

StreamRenderer.prototype._renderBlockDelta = function(data) {
    var el = this.blockElements[data.index];
    if (!el) return;

    if (data.blockType === "text") {
        if (!this.accumulatedMarkdown[data.index]) {
            this.accumulatedMarkdown[data.index] = "";
        }
        this.accumulatedMarkdown[data.index] += data.text;

        // Immediate plain-text render — fast, no markdown parsing
        var mdContainer = el.querySelector(".stream-markdown");
        if (!mdContainer) {
            mdContainer = document.createElement("div");
            mdContainer.className = "stream-markdown stream-marking-cursor";
            el.appendChild(mdContainer);
        }
        // Append delta as plain text span (immediate DOM update)
        var span = document.createElement("span");
        span.className = "stream-md-plain";
        span.textContent = data.text;
        mdContainer.appendChild(span);

        // Debounced rich markdown render (200ms)
        var self = this;
        var capturedIdx = data.index;
        this._scheduleRender("md-" + capturedIdx, function() {
            var container = self.blockElements[capturedIdx];
            if (!container) return;
            var md = container.querySelector(".stream-markdown");
            if (!md) return;
            md.innerHTML = renderMarkdown(self.accumulatedMarkdown[capturedIdx]);
            md.classList.add("stream-marking-cursor");
            self._highlightCodeBlocks(md);
            self._autoScroll();
        }, 200);

    } else if (data.blockType === "thinking") {
        var body = el.querySelector(".stream-block__body");
        if (!body) return;
        var text = this.parser.blocks[data.index].accumulated;
        body.textContent = text;

        // Debounced counter update (500ms)
        var self = this;
        var capturedIdx = data.index;
        this._scheduleRender("think-" + capturedIdx, function() {
            var block = self.blockElements[capturedIdx];
            if (!block) return;
            var counter = block.querySelector(".stream-thinking-counter");
            if (counter) {
                var t = self.parser.blocks[capturedIdx];
                if (t) counter.textContent = "(" + wordCount(t.accumulated) + " words)";
            }
        }, 500);

    } else if (data.blockType === "tool_use") {
        var body = el.querySelector(".stream-block__body");
        if (!body) return;
        var acc = this.parser.blocks[data.index].accumulated;
        var name = this.parser.blocks[data.index].name;

        // Immediate plain-text update on existing <pre> (no DOM recreation)
        var pre = body.querySelector(".stream-tool-input");
        if (!pre) {
            pre = document.createElement("pre");
            pre.className = "stream-tool-input";
            body.appendChild(pre);
        }
        pre.textContent = acc;

        // Debounced formatted render (300ms)
        var self = this;
        var capturedIdx = data.index;
        this._scheduleRender("tool-" + capturedIdx, function() {
            var block = self.blockElements[capturedIdx];
            if (!block) return;
            var b = block.querySelector(".stream-block__body");
            if (!b) return;
            var p = b.querySelector(".stream-tool-input");
            if (!p) return;
            var blk = self.parser.blocks[capturedIdx];
            if (!blk) return;
            p.textContent = self._formatToolInput(blk.name, blk.accumulated);
            self._autoScroll();
        }, 300);
    }
};

StreamRenderer.prototype._renderBlockEnd = function(data) {
    var el = this.blockElements[data.index];
    if (!el) return;

    // Cancel any pending debounced renders for this block
    this._cancelRender("md-" + data.index);
    this._cancelRender("tool-" + data.index);
    this._cancelRender("think-" + data.index);

    if (data.blockType === "text") {
        // Final full markdown render, remove cursor
        var mdContainer = el.querySelector(".stream-markdown");
        if (mdContainer) {
            mdContainer.classList.remove("stream-marking-cursor");
            mdContainer.innerHTML = renderMarkdown(data.fullText);
            this._highlightCodeBlocks(mdContainer);
        }
        delete this.accumulatedMarkdown[data.index];

    } else if (data.blockType === "tool_use") {
        // Final formatted render
        var body = el.querySelector(".stream-block__body");
        if (body) {
            var pre = body.querySelector(".stream-tool-input");
            if (pre) {
                pre.textContent = this._formatToolInput(data.name || "", data.fullText || "");
            }
        }
        // Update status to waiting for result
        var statusEl = el.querySelector(".stream-block__status");
        if (statusEl && !statusEl.querySelector(".stream-block__status-dot--success") && !statusEl.querySelector(".stream-block__status-dot--error")) {
            statusEl.innerHTML = '<span class="stream-block__status-dot"></span>Waiting for result';
        }
        // Add summary to header if not already present (stream_event blocks may not have input at BLOCK_START)
        var summaryEl = el.querySelector(".stream-block__summary");
        if (!summaryEl) {
            var name = data.name || "";
            var summary = getToolSummary(name, data.fullText);
            if (summary) {
                var badge = el.querySelector(".stream-block__badge");
                if (badge) {
                    var span = document.createElement("span");
                    span.className = "stream-block__summary";
                    span.textContent = summary;
                    badge.insertAdjacentElement("afterend", span);
                }
            }
        }
    }
};

StreamRenderer.prototype._renderToolResult = function(data) {
    if (data.blockIndex == null) return;
    var el = this.blockElements[data.blockIndex];
    if (!el) return;

    // Update tool status
    var statusEl = el.querySelector(".stream-block__status");
    if (statusEl) {
        if (data.isError) {
            statusEl.innerHTML = '<span class="stream-block__status-dot stream-block__status-dot--error"></span>Error';
        } else {
            statusEl.innerHTML = '<span class="stream-block__status-dot stream-block__status-dot--success"></span>Done';
        }
    }

    // Add result block
    var resultEl = document.createElement("div");
    resultEl.className = "stream-tool-result" + (data.isError ? " is-error" : "");

    var output = typeof data.content === "string" ? data.content : JSON.stringify(data.content);
    var lines = output.split("\n");
    var isTruncated = lines.length > 50;

    var pre = document.createElement("pre");
    pre.textContent = isTruncated ? lines.slice(0, 50).join("\n") : output;
    if (isTruncated) pre.classList.add("is-truncated");
    resultEl.appendChild(pre);

    if (isTruncated) {
        var btn = document.createElement("button");
        btn.className = "stream-tool-result__toggle";
        btn.textContent = "Show full output (" + lines.length + " lines)";
        btn.addEventListener("click", function() {
            pre.textContent = output;
            pre.classList.remove("is-truncated");
            btn.remove();
        });
        resultEl.appendChild(btn);
    }

    el.appendChild(resultEl);
    // Move tool result inside the collapsible body so it's revealed on expand
    var body = el.querySelector(".stream-block__body");
    if (body) {
        body.appendChild(resultEl);
    }
};

StreamRenderer.prototype._renderResult = function(data) {
    if (this.resultElement) return;
    this.isComplete = true;
    var classes = "stream-result" + (data.isError ? " is-error" : "");
    var el = document.createElement("div");
    el.className = classes;
    var html =
        '<div class="stream-result__row"><span class="stream-result__label">Duration</span><span class="stream-result__value">' + formatDuration(data.durationMs) + '</span></div>' +
        '<div class="stream-result__row"><span class="stream-result__label">API Time</span><span class="stream-result__value">' + formatDuration(data.apiDurationMs) + '</span></div>' +
        '<div class="stream-result__row"><span class="stream-result__label">Cost</span><span class="stream-result__value">$' + (data.costUsd || 0).toFixed(4) + '</span></div>' +
        '<div class="stream-result__row"><span class="stream-result__label">Turns</span><span class="stream-result__value">' + data.numTurns + '</span></div>';
    if (data.isError) {
        html += '<div class="stream-result__row"><span class="stream-result__label">Status</span><span class="stream-result__value stream-result__value--error">Error</span></div>';
    }
    el.innerHTML = html;
    this.contentEl.appendChild(el);
    this.resultElement = el;
};

StreamRenderer.prototype._formatToolInput = function(name, rawInput) {
    try {
        var parsed = typeof rawInput === "string" ? JSON.parse(rawInput) : rawInput;
    } catch(e) { return rawInput; }
    switch(name) {
        case "Bash":
            return "$ " + (parsed.command || JSON.stringify(parsed, null, 2));
        case "Glob":
            return "Pattern: " + (parsed.pattern || "") + (parsed.path ? "\nPath: " + parsed.path : "");
        case "Grep":
            var s = "Pattern: " + (parsed.pattern || "");
            if (parsed.path) s += "\nPath: " + parsed.path;
            if (parsed.output_mode) s += "\nOutput mode: " + parsed.output_mode;
            return s;
        case "Read":
            return (parsed.file_path || "") + (parsed.offset ? "\nOffset: " + parsed.offset : "") + (parsed.limit ? "\nLimit: " + parsed.limit : "");
        default:
            return JSON.stringify(parsed, null, 2);
    }
};

StreamRenderer.prototype._renderRaw = function(data) {
    var el = document.createElement("div");
    el.className = "stream-raw";
    el.textContent = data;
    this.contentEl.appendChild(el);
};

StreamRenderer.prototype._makeCollapsible = function(headerEl, bodyEl) {
    headerEl.addEventListener("click", function() {
        var isHidden = bodyEl.classList.contains("is-hidden");
        bodyEl.classList.toggle("is-hidden", !isHidden);
        headerEl.classList.toggle("is-collapsed", isHidden);
    });
};

StreamRenderer.prototype._scheduleRender = function(key, renderFn, delay) {
    if (this._pendingRenders[key]) {
        clearTimeout(this._pendingRenders[key]);
    }
    var self = this;
    this._pendingRenders[key] = setTimeout(function() {
        delete self._pendingRenders[key];
        renderFn();
    }, delay);
};

StreamRenderer.prototype._cancelRender = function(key) {
    if (this._pendingRenders[key]) {
        clearTimeout(this._pendingRenders[key]);
        delete this._pendingRenders[key];
    }
};

StreamRenderer.prototype._scheduleMarkdownRender = function(idx) {
    this.pendingMarkdownBlocks[idx] = true;
    if (this.renderPendingTimer) return;
    var self = this;
    this.renderPendingTimer = setTimeout(function() {
        self.renderPendingTimer = null;
        self._flushMarkdown();
    }, 150);
};

StreamRenderer.prototype._flushMarkdown = function() {
    this.pendingMarkdownBlocks = {};
};

StreamRenderer.prototype._highlightCodeBlocks = function(container) {
    if (typeof hljs === "undefined") return;
    var codeEls = container.querySelectorAll("pre code");
    for (var i = 0; i < codeEls.length; i++) {
        var codeEl = codeEls[i];
        // Already highlighted by marked + hljs extension
        if (codeEl.classList.contains("hljs")) continue;
        var lang = (codeEl.className.match(/language-(\w+)/) || [])[1];
        codeEl.innerHTML = highlightCode(codeEl.textContent, lang);
    }
};

StreamRenderer.prototype._autoScroll = function() {
    if (this.isAutoScroll) {
        this.container.scrollTop = this.container.scrollHeight;
    }
};

export default StreamRenderer;
