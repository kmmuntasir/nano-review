// =========================================================
// StreamParser: JSONL → structured render commands
// =========================================================

function StreamParser() {
    this.blocks = {};
    this.toolUseMap = {};
    this.lineBuffer = "";
    this.systemInfo = null;
    this.resultInfo = null;
}

StreamParser.prototype.feed = function(jsonlChunk) {
    this.lineBuffer += jsonlChunk;
    var commands = [];
    var lines = this.lineBuffer.split("\n");
    // Last element may be incomplete — keep in buffer
    this.lineBuffer = lines.pop();
    for (var i = 0; i < lines.length; i++) {
        var line = lines[i].trim();
        if (!line) continue;
        try {
            var obj = JSON.parse(line);
            var cmds = this._parseEvent(obj);
            if (cmds) {
                if (Array.isArray(cmds)) {
                    for (var j = 0; j < cmds.length; j++) commands.push(cmds[j]);
                } else {
                    commands.push(cmds);
                }
            }
        } catch(e) {
            commands.push({ type: "RAW", data: line });
        }
    }
    return commands;
};

StreamParser.prototype._parseEvent = function(obj) {
    switch(obj.type) {
        case "system": return this._parseSystem(obj);
        case "stream_event": return this._parseStreamEvent(obj);
        case "assistant": return this._parseAssistant(obj);
        case "user": return this._parseUser(obj);
        case "result": return this._parseResult(obj);
        default: return { type: "RAW", data: JSON.stringify(obj) };
    }
};

StreamParser.prototype._parseAssistant = function(obj) {
    var message = obj.message;
    if (!message || !message.content) return null;
    var items = Array.isArray(message.content) ? message.content : [message.content];
    var commands = [];
    for (var i = 0; i < items.length; i++) {
        var item = items[i];
        // Skip blocks already created by stream_event deltas
        if (this.blocks[i]) continue;
        var blockType = item.type;
        if (blockType === "thinking") {
            var text = item.thinking || "";
            this.blocks[i] = { type: "thinking", accumulated: text, name: "", toolUseId: "" };
            commands.push({ type: "BLOCK_START", data: { index: i, blockType: "thinking", name: "" } });
            if (text) {
                commands.push({ type: "BLOCK_DELTA", data: { index: i, blockType: "thinking", text: text } });
                commands.push({ type: "BLOCK_END", data: { index: i, blockType: "thinking", fullText: text, name: "", toolUseId: "" } });
            }
        } else if (blockType === "text") {
            var text = item.text || "";
            this.blocks[i] = { type: "text", accumulated: text, name: "", toolUseId: "" };
            commands.push({ type: "BLOCK_START", data: { index: i, blockType: "text", name: "" } });
            if (text) {
                commands.push({ type: "BLOCK_DELTA", data: { index: i, blockType: "text", text: text } });
                commands.push({ type: "BLOCK_END", data: { index: i, blockType: "text", fullText: text, name: "", toolUseId: "" } });
            }
        } else if (blockType === "tool_use") {
            var name = item.name || "";
            var toolUseId = item.id || "";
            var input = typeof item.input === "string" ? item.input : JSON.stringify(item.input || {});
            this.blocks[i] = { type: "tool_use", accumulated: input, name: name, toolUseId: toolUseId };
            if (toolUseId) this.toolUseMap[toolUseId] = i;
            commands.push({ type: "BLOCK_START", data: { index: i, blockType: "tool_use", name: name, input: input } });
            if (input) {
                commands.push({ type: "BLOCK_DELTA", data: { index: i, blockType: "tool_use", text: input } });
                commands.push({ type: "BLOCK_END", data: { index: i, blockType: "tool_use", fullText: input, name: name, toolUseId: toolUseId } });
            }
        }
    }
    return commands.length === 1 ? commands[0] : (commands.length > 1 ? commands : null);
};

StreamParser.prototype._parseSystem = function(obj) {
    if (obj.subtype === "init") {
        this.systemInfo = {
            model: obj.model || "unknown",
            tools: (obj.tools || []).length,
            version: obj.claude_code_version || "unknown",
            sessionId: obj.session_id || ""
        };
        return { type: "SYSTEM_INIT", data: this.systemInfo };
    }
    return null;
};

StreamParser.prototype._parseStreamEvent = function(obj) {
    var event = obj.event;
    if (!event || !event.type) return null;
    switch(event.type) {
        case "content_block_start": return this._handleBlockStart(event);
        case "content_block_delta": return this._handleBlockDelta(event);
        case "content_block_stop": return this._handleBlockStop(event);
        default: return null;
    }
};

StreamParser.prototype._handleBlockStart = function(event) {
    var cb = event.content_block;
    if (!cb) return null;
    var idx = event.index;
    var blockType = cb.type;
    this.blocks[idx] = {
        type: blockType,
        accumulated: "",
        name: cb.name || "",
        toolUseId: cb.id || ""
    };
    if (blockType === "tool_use" && cb.id) {
        this.toolUseMap[cb.id] = idx;
    }
    return { type: "BLOCK_START", data: { index: idx, blockType: blockType, name: cb.name || "" } };
};

StreamParser.prototype._handleBlockDelta = function(event) {
    var delta = event.delta;
    if (!delta) return null;
    var idx = event.index;
    var block = this.blocks[idx];
    if (!block) return null;
    switch(delta.type) {
        case "thinking_delta":
            block.accumulated += (delta.thinking || "");
            return { type: "BLOCK_DELTA", data: { index: idx, blockType: "thinking", text: delta.thinking || "" } };
        case "text_delta":
            block.accumulated += (delta.text || "");
            return { type: "BLOCK_DELTA", data: { index: idx, blockType: "text", text: delta.text || "" } };
        case "input_json_delta":
            block.accumulated += (delta.partial_json || "");
            return { type: "BLOCK_DELTA", data: { index: idx, blockType: "tool_use", text: delta.partial_json || "" } };
        default: return null;
    }
};

StreamParser.prototype._handleBlockStop = function(event) {
    var idx = event.index;
    var block = this.blocks[idx];
    if (!block) return null;
    return {
        type: "BLOCK_END",
        data: {
            index: idx,
            blockType: block.type,
            fullText: block.accumulated,
            name: block.name,
            toolUseId: block.toolUseId
        }
    };
};

StreamParser.prototype._parseUser = function(obj) {
    var content = obj.message ? obj.message.content : null;
    if (!content) return null;
    var items = Array.isArray(content) ? content : [content];
    var commands = [];
    for (var i = 0; i < items.length; i++) {
        var item = items[i];
        if (item.type === "tool_result" && item.tool_use_id) {
            commands.push({
                type: "TOOL_RESULT",
                data: {
                    toolUseId: item.tool_use_id,
                    blockIndex: this.toolUseMap[item.tool_use_id],
                    content: item.content || "",
                    isError: item.is_error || false
                }
            });
        }
    }
    return commands.length === 1 ? commands[0] : (commands.length > 1 ? commands : null);
};

StreamParser.prototype._parseResult = function(obj) {
    this.resultInfo = {
        costUsd: obj.cost_usd || 0,
        durationMs: obj.duration_ms || 0,
        apiDurationMs: obj.duration_api_ms || 0,
        isError: obj.is_error || false,
        numTurns: obj.num_turns || 0
    };
    return { type: "RESULT", data: this.resultInfo };
};

export default StreamParser;
