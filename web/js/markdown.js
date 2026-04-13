import { esc } from './utils.js';

// --- Marked.js configuration ---
if (typeof marked !== "undefined") {
    marked.setOptions({ breaks: true, gfm: true });
}

export function renderMarkdown(text) {
    if (typeof marked !== "undefined") {
        try { return marked.parse(text); } catch(e) { /* fall through */ }
    }
    // Fallback: escape HTML and convert basic markdown
    var html = esc(text)
        .replace(/```(\w*)\n([\s\S]*?)```/g, "<pre><code>$2</code></pre>")
        .replace(/`([^`]+)`/g, "<code>$1</code>")
        .replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>")
        .replace(/\*(.+?)\*/g, "<em>$1</em>")
        .replace(/\n/g, "<br>");
    return html;
}

export function highlightCode(code, lang) {
    if (typeof hljs !== "undefined") {
        try {
            if (lang && hljs.getLanguage(lang)) {
                return hljs.highlight(code, { language: lang }).value;
            }
            return hljs.highlightAuto(code).value;
        } catch(e) { /* fall through */ }
    }
    return esc(code);
}
