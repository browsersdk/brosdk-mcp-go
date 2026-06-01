// Package mcp — embedded MCP Inspector web UI.
//
// GET /inspector returns a self-contained HTML page that can:
//   - list all registered MCP tools with search/filter
//   - call any tool with a JSON parameter editor
//   - display real-time SSE events (sdk-event + message)
//   - keep a scrollable call history
//
// Zero external dependencies — all CSS/JS embedded in the page.
package mcp

import (
	"encoding/json"
	"net/http"
)

// inspectorHTML is the self-contained single-page application.
const inspectorHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>BroSDK MCP Inspector</title>
<style>
:root {
  --bg: #0d1117;
  --bg2: #161b22;
  --bg3: #21262d;
  --border: #30363d;
  --text: #c9d1d9;
  --text2: #8b949e;
  --accent: #58a6ff;
  --green: #3fb950;
  --red: #f85149;
  --yellow: #d2991d;
  --font: -apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;
  --mono: "SF Mono","Fira Code","Cascadia Code",Consolas,monospace;
}
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:var(--font);background:var(--bg);color:var(--text);height:100vh;display:flex;flex-direction:column}
header{background:var(--bg2);border-bottom:1px solid var(--border);padding:10px 16px;display:flex;align-items:center;justify-content:space-between;flex-shrink:0}
header h1{font-size:15px;font-weight:600;display:flex;align-items:center;gap:8px}
header h1 span.version{font-size:11px;color:var(--text2);font-weight:400;background:var(--bg3);padding:2px 6px;border-radius:4px}
header .status{font-size:12px;display:flex;align-items:center;gap:6px}
header .status .dot{width:8px;height:8px;border-radius:50%}
header .status .dot.on{background:var(--green)}
header .status .dot.off{background:var(--red)}
main{flex:1;display:grid;grid-template-columns:280px 1fr;grid-template-rows:1fr 200px;gap:1px;background:var(--border);overflow:hidden}
.panel{background:var(--bg2);overflow:hidden;display:flex;flex-direction:column}
.panel-header{font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.5px;color:var(--text2);padding:10px 12px;border-bottom:1px solid var(--border);flex-shrink:0;display:flex;align-items:center;gap:8px}
.panel-body{flex:1;overflow:auto;padding:0}
/* ── Tools panel (left sidebar) ── */
#tools-panel{grid-row:1/3}
#tool-search{width:100%;background:var(--bg);border:none;border-bottom:1px solid var(--border);color:var(--text);font-size:12px;padding:8px 12px;outline:none}
#tool-search::placeholder{color:var(--text2)}
#tool-list{overflow:auto;flex:1}
.tool-item{padding:6px 12px;font-size:12px;cursor:pointer;border-bottom:1px solid transparent;transition:background .1s;display:flex;align-items:center;gap:6px}
.tool-item:hover{background:var(--bg3)}
.tool-item.active{background:rgba(88,166,255,.12);border-left:2px solid var(--accent);padding-left:10px}
.tool-item .name{font-family:var(--mono);font-size:11px;font-weight:500;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.tool-item .desc{color:var(--text2);font-size:10px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;flex:1}
.tool-item .badge{font-size:9px;padding:1px 5px;border-radius:3px;background:var(--bg3);color:var(--text2);flex-shrink:0}
/* ── Call panel (top-right) ── */
#call-panel .panel-body{padding:12px;display:flex;flex-direction:column;gap:10px}
#call-panel label{font-size:11px;color:var(--text2);font-weight:500}
#call-tool-name{font-family:var(--mono);font-size:13px;color:var(--accent)}
#call-tool-desc{font-size:12px;color:var(--text2)}
.param-section{display:flex;flex-direction:column;gap:4px}
#params-editor{width:100%;min-height:120px;background:var(--bg);border:1px solid var(--border);border-radius:6px;color:var(--text);font-family:var(--mono);font-size:12px;padding:10px;resize:vertical;outline:none;tab-size:2}
#params-editor:focus{border-color:var(--accent)}
.btn-row{display:flex;gap:8px}
.btn{padding:6px 14px;border-radius:6px;border:1px solid var(--border);background:var(--bg3);color:var(--text);font-size:12px;cursor:pointer;transition:all .15s;display:flex;align-items:center;gap:5px}
.btn:hover{background:var(--border)}
.btn.primary{background:var(--accent);color:#fff;border-color:var(--accent)}
.btn.primary:hover{opacity:.9}
.btn.danger{color:var(--red)}
#call-result{flex:1;overflow:auto}
.result-item{margin-bottom:8px;border:1px solid var(--border);border-radius:6px;overflow:hidden}
.result-item .meta{padding:5px 10px;background:var(--bg3);font-size:11px;color:var(--text2);display:flex;justify-content:space-between}
.result-item .meta .ts{font-family:var(--mono)}
.result-item .meta .status.ok{color:var(--green)}
.result-item .meta .status.err{color:var(--red)}
.result-item pre{font-family:var(--mono);font-size:11px;padding:10px;margin:0;background:var(--bg);overflow:auto;max-height:300px;white-space:pre-wrap;word-break:break-all}
/* ── SSE panel (bottom) ── */
#sse-panel{grid-column:2}
#sse-log{flex:1;overflow:auto;font-family:var(--mono);font-size:11px;padding:4px 0}
.sse-entry{border-bottom:1px solid rgba(48,54,61,.5)}
.sse-entry .sse-header{padding:3px 12px;display:flex;gap:8px;align-items:flex-start;cursor:pointer;transition:background .1s}
.sse-entry .sse-header:hover{background:var(--bg3)}
.sse-entry .time{color:var(--text2);flex-shrink:0;font-family:var(--mono);font-size:11px}
.sse-entry .event-type{font-size:11px;color:var(--yellow);flex-shrink:0;font-weight:600;min-width:80px}
.sse-entry .event-preview{font-size:11px;color:var(--text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:1}
.sse-entry .event-expand{font-size:10px;color:var(--text2);flex-shrink:0;transition:transform .15s}
.sse-entry.open .event-expand{transform:rotate(180deg)}
.sse-entry .sse-detail{display:none;padding:0 12px 8px;font-family:var(--mono);font-size:11px;color:var(--text);overflow:auto;max-height:300px}
.sse-entry .sse-detail pre{margin:0;white-space:pre-wrap;word-break:break-all;background:var(--bg);padding:8px;border-radius:4px}
.sse-entry.open .sse-detail{display:block}
/* ── JSON syntax highlighting ── */
.json-key{color:var(--accent)}
.json-string{color:#a5d6ff}
.json-number{color:var(--green)}
.json-bool{color:var(--yellow)}
.json-null{color:var(--text2)}
/* ── Responsive ── */
@media(max-width:800px){
  main{grid-template-columns:1fr;grid-template-rows:auto 1fr 200px}
  #tools-panel{grid-row:1;max-height:200px}
  #call-panel{grid-row:2}
  #sse-panel{grid-column:1;grid-row:3}
}
/* ── Scrollbar ── */
::-webkit-scrollbar{width:6px;height:6px}
::-webkit-scrollbar-track{background:transparent}
::-webkit-scrollbar-thumb{background:var(--border);border-radius:3px}
::-webkit-scrollbar-thumb:hover{background:var(--text2)}
</style>
</head>
<body>
<header>
  <h1>&#x1F50D; BroSDK MCP Inspector <span class="version" id="server-version">v0.1.0</span></h1>
  <div class="status">
    <span class="dot on" id="sse-dot"></span>
    <span id="sse-status">Connected</span>
    &middot;
    <span id="event-count">0 events</span>
  </div>
</header>
<main>
  <!-- Tools Panel -->
  <div class="panel" id="tools-panel">
    <div class="panel-header">&#x1F4E6; Tools <span id="tool-count" style="font-weight:400">(0)</span></div>
    <input type="text" id="tool-search" placeholder="Filter tools...">
    <div id="tool-list"></div>
  </div>
  <!-- Call Panel -->
  <div class="panel" id="call-panel">
    <div class="panel-header">&#x26A1; Tool Call</div>
    <div class="panel-body">
      <div>
        <div id="call-tool-name" style="margin-bottom:2px">Select a tool</div>
        <div id="call-tool-desc"></div>
      </div>
      <div class="param-section">
        <label>Arguments (JSON)</label>
        <textarea id="params-editor" placeholder='{ }' spellcheck="false"></textarea>
        <div class="btn-row">
          <button class="btn" id="btn-format" title="Format JSON">&#x1F4C4; Format</button>
          <button class="btn" id="btn-reset" title="Reset to empty object">&#x21BA; Reset</button>
          <button class="btn primary" id="btn-call">&#x25B6; Call Tool</button>
        </div>
      </div>
      <div class="panel-header" style="margin:0 -12px;padding-left:0;border-top:1px solid var(--border);justify-content:space-between">
        <span>&#x1F4C3; History</span>
        <button class="btn" id="btn-clear-history" style="font-size:10px;padding:2px 8px">Clear</button>
      </div>
      <div id="call-result"><div style="color:var(--text2);font-size:12px;padding:8px 0">No calls yet. Select a tool and click "Call Tool".</div></div>
    </div>
  </div>
  <!-- SSE Panel -->
  <div class="panel" id="sse-panel">
    <div class="panel-header" style="justify-content:space-between">
      <span>&#x1F4E1; SSE Events</span>
      <button class="btn" id="btn-clear-sse" style="font-size:10px;padding:2px 8px">Clear</button>
    </div>
    <div id="sse-log"><div style="color:var(--text2);font-size:11px;padding:8px 12px">Connecting to SSE...</div></div>
  </div>
</main>
<script>
// ── State ──
let tools = [];
let selectedTool = null;
let sseSource = null;
let eventCount = 0;
let callHistory = [];
let historyIdx = 0;

// ── DOM refs ──
const $ = id => document.getElementById(id);
const toolList = $('tool-list');
const toolSearch = $('tool-search');
const toolCount = $('tool-count');
const callToolName = $('call-tool-name');
const callToolDesc = $('call-tool-desc');
const paramsEditor = $('params-editor');
const callResult = $('call-result');
const sseLog = $('sse-log');
const sseDot = $('sse-dot');
const sseStatus = $('sse-status');
const eventCountEl = $('event-count');

// ── Time helper ──
function now() {
  const d = new Date();
  return d.toTimeString().slice(0,8) + '.' + String(d.getMilliseconds()).padStart(3,'0');
}

// ── SSE Connection ──
function connectSSE() {
  if (sseSource) sseSource.close();
  sseSource = new EventSource('/sse');

  sseSource.onopen = () => {
    sseDot.className = 'dot on';
    sseStatus.textContent = 'Connected';
    appendSSE('system', 'SSE connection established');
  };

  sseSource.addEventListener('endpoint', (e) => {
    appendSSE('endpoint', e.data);
  });

  sseSource.addEventListener('message', (e) => {
    try {
      const d = JSON.parse(e.data);
      appendSSE('message', formatJSON(d));
    } catch(_) {
      appendSSE('message', e.data);
    }
  });

  sseSource.addEventListener('sdk-event', (e) => {
    try {
      const d = JSON.parse(e.data);
      appendSSE('sdk-event', formatJSON(d));
    } catch(_) {
      appendSSE('sdk-event', e.data);
    }
  });

  sseSource.onerror = () => {
    sseDot.className = 'dot off';
    sseStatus.textContent = 'Disconnected';
    appendSSE('system', 'SSE disconnected — retrying in 3s...');
    sseSource.close();
    setTimeout(connectSSE, 3000);
  };
}

function appendSSE(type, data) {
  eventCount++;
  eventCountEl.textContent = eventCount + ' events';

  // Remove placeholder
  const ph = sseLog.querySelector(':scope > div:only-child');
  if (ph && ph.style && ph.style.color === 'rgb(139, 148, 158)') ph.remove();

  // Generate a one-line preview (first 120 chars)
  const preview = data.length > 120 ? data.slice(0,120).replace(/\n/g,' ') + '...' : data;

  const entry = document.createElement('div');
  entry.className = 'sse-entry';
  entry.innerHTML =
    '<div class="sse-header">' +
      '<span class="time">' + now() + '</span>' +
      '<span class="event-type">' + esc(type) + '</span>' +
      '<span class="event-preview">' + esc(preview) + '</span>' +
      '<span class="event-expand">&#x25BC;</span>' +
    '</div>' +
    '<div class="sse-detail"><pre>' + esc(data) + '</pre></div>';

  entry.querySelector('.sse-header').addEventListener('click', () => {
    // Close all other open entries
    sseLog.querySelectorAll('.sse-entry.open').forEach(el => {
      if (el !== entry) el.classList.remove('open');
    });
    entry.classList.toggle('open');
  });
  sseLog.appendChild(entry);
  sseLog.scrollTop = sseLog.scrollHeight;

  // Limit to 500 entries
  while (sseLog.children.length > 500) sseLog.firstChild.remove();
}

// ── Fetch tools via JSON-RPC ──
async function fetchTools() {
  try {
    const resp = await fetch('/message', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({jsonrpc:'2.0',id:'init-ts',method:'tools/list'})
    });
    const j = await resp.json();
    return j.result.tools || [];
  } catch(e) {
    appendSSE('system', 'Failed to fetch tools: ' + e.message);
    return [];
  }
}

// ── Render tool list ──
function renderTools(filter) {
  const f = (filter || '').toLowerCase();
  const filtered = tools.filter(t =>
    !f || t.name.toLowerCase().includes(f) || (t.description||'').toLowerCase().includes(f)
  );
  toolCount.textContent = '(' + filtered.length + '/' + tools.length + ')';

  toolList.innerHTML = '';
  filtered.forEach(t => {
    const div = document.createElement('div');
    div.className = 'tool-item' + (selectedTool && selectedTool.name === t.name ? ' active' : '');
    div.innerHTML = '<span class="name">' + esc(t.name) + '</span>' +
      '<span class="desc">' + esc(t.description||'') + '</span>';
    div.addEventListener('click', () => selectTool(t));
    toolList.appendChild(div);
  });
}

function selectTool(t) {
  selectedTool = t;
  callToolName.textContent = t.name;
  callToolDesc.textContent = t.description || '';

  // Build default params from schema
  let defaultParams = '{}';
  if (t.inputSchema) {
    try {
      const schema = typeof t.inputSchema === 'string' ? JSON.parse(t.inputSchema) : t.inputSchema;
      const props = schema.properties || {};
      const required = schema.required || [];
      const defaults = {};
      for (const [k, v] of Object.entries(props)) {
        if (v.default !== undefined) defaults[k] = v.default;
        else if (required.includes(k)) {
          switch(v.type) {
            case 'string': defaults[k] = ''; break;
            case 'number': case 'integer': defaults[k] = 0; break;
            case 'boolean': defaults[k] = false; break;
            case 'array': defaults[k] = []; break;
            case 'object': defaults[k] = {}; break;
            default: defaults[k] = null;
          }
        }
      }
      defaultParams = JSON.stringify(defaults, null, 2);
    } catch(_) {}
  }
  paramsEditor.value = defaultParams;
  renderTools(toolSearch.value);
}

// ── Tool Call ──
async function callTool() {
  if (!selectedTool) return;
  let params;
  try {
    params = JSON.parse(paramsEditor.value);
  } catch(e) {
    alert('Invalid JSON: ' + e.message);
    return;
  }

  const id = 'call-' + (++historyIdx);
  const reqBody = {
    jsonrpc: '2.0',
    id: id,
    method: 'tools/call',
    params: { name: selectedTool.name, arguments: params }
  };

  const startTime = now();
  try {
    const resp = await fetch('/message', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify(reqBody)
    });
    const j = await resp.json();
    addHistory(selectedTool.name, params, j, startTime, false);
  } catch(e) {
    addHistory(selectedTool.name, params, {error:{message:e.message}}, startTime, true);
  }
}

function addHistory(toolName, params, result, ts, isError) {
  callHistory.unshift({toolName, params, result, ts, isError});
  if (callHistory.length > 50) callHistory.pop();
  renderHistory();
}

function renderHistory() {
  if (callHistory.length === 0) {
    callResult.innerHTML = '<div style="color:var(--text2);font-size:12px;padding:8px 0">No calls yet.</div>';
    return;
  }
  callResult.innerHTML = callHistory.map((h,i) =>
    '<div class="result-item">' +
      '<div class="meta">' +
        '<span>' + esc(h.toolName) + '</span>' +
        '<span class="ts">' + h.ts + '</span>' +
        '<span class="status ' + (h.result.error ? 'err' : 'ok') + '">' +
          (h.result.error ? 'ERROR' : 'OK') +
        '</span>' +
      '</div>' +
      '<pre>' + esc(formatJSON(h.result)) + '</pre>' +
    '</div>'
  ).join('');
  if (callResult.firstChild) callResult.firstChild.scrollIntoView({behavior:'smooth'});
}

// ── Helpers ──
function esc(s) {
  if (s === null || s === undefined) return '';
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

function formatJSON(obj) {
  try {
    return JSON.stringify(obj, null, 2);
  } catch(_) {
    return String(obj);
  }
}

function formatParams() {
  try {
    const obj = JSON.parse(paramsEditor.value);
    paramsEditor.value = JSON.stringify(obj, null, 2);
  } catch(e) {
    alert('Invalid JSON: ' + e.message);
  }
}

function resetParams() {
  if (selectedTool) selectTool(selectedTool);
  else paramsEditor.value = '{}';
}

// ── Event bindings ──
toolSearch.addEventListener('input', () => renderTools(toolSearch.value));
$('btn-format').addEventListener('click', formatParams);
$('btn-reset').addEventListener('click', resetParams);
$('btn-call').addEventListener('click', callTool);
$('btn-clear-sse').addEventListener('click', () => {
  sseLog.innerHTML = '';
  eventCount = 0;
  eventCountEl.textContent = '0 events';
});
$('btn-clear-history').addEventListener('click', () => {
  callHistory = [];
  renderHistory();
});

// Ctrl+Enter to call
paramsEditor.addEventListener('keydown', (e) => {
  if (e.ctrlKey && e.key === 'Enter') callTool();
});

// ── Initialize ──
async function init() {
  connectSSE();
  tools = await fetchTools();
  renderTools();
  if (tools.length > 0) selectTool(tools[0]);
  // Try to get server version
  try {
    const resp = await fetch('/health');
    const j = await resp.json();
    $('server-version').textContent = 'v' + (j.version || '0.1.0');
  } catch(_) {}
}
init();
</script>
</body>
</html>`

// handleInspector serves the embedded MCP Inspector HTML page.
func (s *Server) handleInspector(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(inspectorHTML))
}

// inspectorToolsJSON returns the tool list as JSON for the UI to consume via /message.
// The inspector page calls tools/list over POST /message, so no extra endpoint needed.
func inspectorToolsJSON(tools []ToolDef) []byte {
	type toolEntry struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	entries := make([]toolEntry, len(tools))
	for i, t := range tools {
		entries[i] = toolEntry{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	data, _ := json.Marshal(entries)
	return data
}
