/* =========================================================
   BunkrDownloader · Web Control Panel · App Logic
   ========================================================= */

const API = {
  tasks:        () => '/api/tasks',
  task:    (id) => `/api/tasks/${id}`,
  taskFiles:    (id) => `/api/tasks/${id}/files`,
  taskEvents:   (id) => `/api/tasks/${id}/events`,
  start:    (id) => `/api/tasks/${id}/start`,
  pause:    (id) => `/api/tasks/${id}/pause`,
  resume:   (id) => `/api/tasks/${id}/resume`,
  cancel:   (id) => `/api/tasks/${id}/cancel`,
  retryTask:   (id) => `/api/tasks/${id}/retry`,
  delete:   (id) => `/api/tasks/${id}`,
  fileRetry:   (id) => `/api/files/${id}/retry`,
  events:        () => '/api/events',
  stats:        () => '/api/stats',
  health:       () => '/api/health',
};

const state = {
  tasks: [],
  selectedTaskId: null,
  selectedTask: null,
  selectedFiles: [],
  selectedFilesPage: 0,
  selectedFilesTotal: 0,
  selectedEvents: [],
  ws: null,
  wsRetry: 0,
};

// ============== Utilities ==============
const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

function formatBytes(n) {
  if (!n || n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const exp = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  return `${(n / Math.pow(1024, exp)).toFixed(exp ? 1 : 0)} ${units[exp]}`;
}

function formatTime(iso) {
  if (!iso) return '—';
  try {
    const d = new Date(iso);
    if (isNaN(d.getTime())) return iso;
    return d.toLocaleString();
  } catch (e) { return iso; }
}

function formatTimeShort(iso) {
  if (!iso) return '—';
  try {
    const d = new Date(iso);
    if (isNaN(d.getTime())) return iso;
    return d.toLocaleTimeString();
  } catch (e) { return iso; }
}

function escapeHtml(s) {
  if (s == null) return '';
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function truncateUrl(url, max = 60) {
  if (!url) return '';
  if (url.length <= max) return url;
  return url.slice(0, max - 3) + '...';
}

// ============== Toast ==============
function toast(message, kind = 'info') {
  const el = document.createElement('div');
  el.className = `toast toast--${kind}`;
  el.textContent = message;
  $('#toasts').appendChild(el);
  setTimeout(() => {
    el.style.transition = 'opacity 0.2s, transform 0.2s';
    el.style.opacity = '0';
    el.style.transform = 'translateX(20px)';
    setTimeout(() => el.remove(), 200);
  }, 3000);
}

// ============== API calls ==============
async function api(url, options = {}) {
  try {
    const res = await fetch(url, {
      headers: { 'Content-Type': 'application/json' },
      ...options,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      throw new Error(err.error || `HTTP ${res.status}`);
    }
    return await res.json();
  } catch (e) {
    toast(`API error: ${e.message}`, 'err');
    throw e;
  }
}

async function loadTasks() {
  const data = await api(API.tasks());
  state.tasks = data.tasks || [];
  renderTaskList();
  renderGlobalStats();
}

async function loadTask(id) {
  const task = await api(API.task(id));
  state.selectedTask = task;
  state.selectedTaskId = id;
  state.selectedFilesPage = 0;
  state.selectedFiles = await loadFiles(id, 0);
  state.selectedEvents = await loadEvents(id);
  renderDetail();
  highlightActive();
}

async function loadFiles(id, page = 0) {
  const data = await api(`${API.taskFiles(id)}?offset=${page * 50}&limit=50`);
  state.selectedFilesPage = page;
  state.selectedFilesTotal = data.total || 0;
  return data.files || [];
}

async function loadEvents(id) {
  const data = await api(API.taskEvents(id));
  return data.events || [];
}

async function startTask(id) {
  await api(API.start(id), { method: 'POST' });
  toast('Task started', 'ok');
  await loadTasks();
  if (state.selectedTaskId === id) await loadTask(id);
}

async function pauseTask(id) {
  await api(API.pause(id), { method: 'POST' });
  toast('Task paused', 'warn');
  await loadTasks();
  if (state.selectedTaskId === id) await loadTask(id);
}

async function resumeTask(id) {
  await api(API.resume(id), { method: 'POST' });
  toast('Task resumed', 'ok');
  await loadTasks();
  if (state.selectedTaskId === id) await loadTask(id);
}

async function cancelTask(id) {
  if (!confirm('Cancel this task? Running downloads will be aborted.')) return;
  await api(API.cancel(id), { method: 'POST' });
  toast('Task canceled', 'warn');
  await loadTasks();
  if (state.selectedTaskId === id) await loadTask(id);
}

async function deleteTask(id) {
  if (!confirm('Delete this task and all its history? This cannot be undone.')) return;
  await api(API.delete(id), { method: 'DELETE' });
  toast('Task deleted', 'ok');
  if (state.selectedTaskId === id) {
    state.selectedTaskId = null;
    state.selectedTask = null;
    renderDetail();
  }
  await loadTasks();
}

async function retryTask(id) {
  await api(API.retryTask(id), { method: 'POST' });
  toast('Retrying failed files', 'ok');
  await loadTasks();
  if (state.selectedTaskId === id) await loadTask(id);
}

async function retryFile(fileId) {
  await api(API.fileRetry(fileId), { method: 'POST' });
  toast('Retrying file', 'ok');
  if (state.selectedTaskId) await loadTask(state.selectedTaskId);
}

async function createTask(url, options) {
  const data = await api(API.tasks(), {
    method: 'POST',
    body: JSON.stringify({ url, options, auto_start: true }),
  });
  // 支持批量创建：返回 task_ids 数组或单个 task_id
  const ids = data.task_ids;
  const count = data.count || 1;
  if (count > 1) {
    toast(`Created ${count} tasks (#${Array.isArray(ids) ? ids.join(', #') : ids})`, 'ok');
  } else {
    toast(`Task created (#${ids})`, 'ok');
  }
  await loadTasks();
  return count > 1 ? ids : ids;
}

// ============== Renderers ==============
function renderTaskList() {
  const list = $('#taskList');
  if (state.tasks.length === 0) {
    list.innerHTML = `
      <div class="empty-state">
        <div class="empty-state__icon">▢</div>
        <div class="empty-state__text">No tasks yet</div>
        <div class="empty-state__hint">Create one with the button above</div>
      </div>`;
    return;
  }
  // 运行中 / 暂停 的任务置顶
  const active = ['running', 'paused'];
  const sorted = [...state.tasks].sort((a, b) => {
    const ao = active.indexOf(a.status);
    const bo = active.indexOf(b.status);
    if (ao !== bo) return (ao === -1 ? 1 : 0) - (bo === -1 ? 1 : 0);
    return b.id - a.id;
  });
  list.innerHTML = sorted.map(t => {
    const total = t.total_files || 0;
    const completed = t.completed_files || 0;
    const failed = t.failed_files || 0;
    const progress = total > 0 ? (completed / total) * 100 : 0;
    const active = t.id === state.selectedTaskId ? 'active' : '';
    return `
      <div class="task-card status-${t.status} ${active}" data-id="${t.id}">
        <div class="task-card__head">
          <span class="task-card__id">#${String(t.id).padStart(4, '0')}</span>
          <span class="task-card__status">${t.status}</span>
        </div>
        <div class="task-card__url" title="${escapeHtml(t.url)}">${escapeHtml(truncateUrl(t.url, 50))}</div>
        <div class="task-card__progress">
          <div class="progress-bar">
            <div class="progress-bar__fill" style="width: ${progress.toFixed(1)}%"></div>
          </div>
          <span class="task-card__counts">${completed}/${total}${failed ? ` · ${failed}✗` : ''}</span>
        </div>
      </div>`;
  }).join('');

  $$('#taskList .task-card').forEach(card => {
    card.addEventListener('click', () => {
      const id = parseInt(card.dataset.id, 10);
      loadTask(id);
    });
  });
}

function renderGlobalStats() {
  const counts = { running: 0, pending: 0, paused: 0, completed: 0, failed: 0 };
  state.tasks.forEach(t => {
    if (counts[t.status] !== undefined) counts[t.status]++;
  });
  $('#statRunning').textContent   = counts.running;
  $('#statPending').textContent   = counts.pending;
  $('#statPaused').textContent    = counts.paused;
  $('#statCompleted').textContent = counts.completed;
  $('#statFailed').textContent    = counts.failed;
}

function highlightActive() {
  $$('#taskList .task-card').forEach(c => {
    const id = parseInt(c.dataset.id, 10);
    c.classList.toggle('active', id === state.selectedTaskId);
  });
}

function renderDetail() {
  const view = $('#detailView');
  const title = $('#detailTitle');
  const index = $('#detailIndex');
  const actions = $('#detailActions');

  if (!state.selectedTask) {
    view.innerHTML = `
      <div class="empty-state">
        <div class="empty-state__icon">◇</div>
        <div class="empty-state__text">Select a task to view details</div>
      </div>`;
    title.textContent = 'DETAIL';
    index.textContent = '02';
    actions.innerHTML = '';
    return;
  }
  const t = state.selectedTask;
  title.textContent = `TASK #${String(t.id).padStart(4, '0')}`;
  index.textContent = '●';

  // Action buttons
  const actHtml = [];
  if (t.status === 'pending' || t.status === 'paused' || t.status === 'failed') {
    actHtml.push(`<button class="btn btn--primary btn--sm" data-action="start">▶ START</button>`);
  }
  if (t.status === 'running') {
    actHtml.push(`<button class="btn btn--sm" data-action="pause">⏸ PAUSE</button>`);
  }
  if (t.status === 'paused') {
    actHtml.push(`<button class="btn btn--primary btn--sm" data-action="resume">▶ RESUME</button>`);
  }
  if (t.status === 'failed' || t.failed_files > 0) {
    actHtml.push(`<button class="btn btn--sm" data-action="retry">↻ RETRY FAILED</button>`);
  }
  if (t.status === 'running' || t.status === 'paused') {
    actHtml.push(`<button class="btn btn--danger btn--sm" data-action="cancel">■ CANCEL</button>`);
  }
  actHtml.push(`<button class="btn btn--danger btn--sm" data-action="delete">✕ DELETE</button>`);
  actions.innerHTML = actHtml.join('');

  // Bind action handlers
  actions.querySelectorAll('[data-action]').forEach(btn => {
    btn.addEventListener('click', () => {
      const action = btn.dataset.action;
      if (action === 'start')  startTask(t.id);
      if (action === 'pause')  pauseTask(t.id);
      if (action === 'resume') resumeTask(t.id);
      if (action === 'cancel') cancelTask(t.id);
      if (action === 'delete') deleteTask(t.id);
      if (action === 'retry')  retryTask(t.id);
    });
  });

  // Detail body
  const total = t.total_files || 0;
  const completed = t.completed_files || 0;
  const failed = t.failed_files || 0;
  const skipped = t.skipped_files || 0;
  const pending = Math.max(0, total - completed - failed - skipped);
  const downloaded = t.downloaded_bytes || 0;
  const totalBytes = t.total_bytes || 0;
  const progress = total > 0 ? (completed / total) * 100 : 0;
  const downloadedFmt = formatBytes(downloaded);
  const totalBytesFmt = formatBytes(totalBytes);

  const files = (state.selectedFiles || []).sort((a, b) => {
    const aDown = a.status === 'downloading' ? 1 : 0;
    const bDown = b.status === 'downloading' ? 1 : 0;
    return bDown - aDown;
  });
  const events = state.selectedEvents || [];

  view.innerHTML = `
    <div class="detail__body">
      <div class="detail-section">
        <div class="detail-section__title">URL</div>
        <div class="detail-meta">
          <div class="detail-meta__item" style="grid-column: 1 / -1;">
            <div class="detail-meta__value">${escapeHtml(t.url)}</div>
          </div>
        </div>
      </div>

      <div class="detail-section">
        <div class="detail-section__title">META</div>
        <div class="detail-meta">
          <div class="detail-meta__item">
            <div class="detail-meta__label">STATUS</div>
            <div class="detail-meta__value">${t.status.toUpperCase()}</div>
          </div>
          <div class="detail-meta__item">
            <div class="detail-meta__label">ALBUM</div>
            <div class="detail-meta__value">${escapeHtml(t.album_name || '—')}${t.album_id ? ` (${escapeHtml(t.album_id)})` : ''}</div>
          </div>
          <div class="detail-meta__item">
            <div class="detail-meta__label">PATH</div>
            <div class="detail-meta__value">${escapeHtml(t.download_path || '—')}</div>
          </div>
          <div class="detail-meta__item">
            <div class="detail-meta__label">CREATED</div>
            <div class="detail-meta__value">${formatTime(t.created_at)}</div>
          </div>
          <div class="detail-meta__item">
            <div class="detail-meta__label">STARTED</div>
            <div class="detail-meta__value">${formatTime(t.started_at)}</div>
          </div>
          <div class="detail-meta__item">
            <div class="detail-meta__label">FINISHED</div>
            <div class="detail-meta__value">${formatTime(t.finished_at)}</div>
          </div>
        </div>
      </div>

      <div class="detail-section">
        <div class="detail-section__title">PROGRESS</div>
        <div class="overall-progress">
          <div class="overall-progress__bar">
            <div class="overall-progress__fill" style="width: ${progress.toFixed(1)}%"></div>
          </div>
          <div class="overall-progress__meta">
            <span>${completed} / ${total} files · ${progress.toFixed(1)}%</span>
            <span>${downloadedFmt} / ${totalBytesFmt}</span>
          </div>
        </div>
        <div class="detail-stats" style="margin-top: var(--space-3);">
          <div class="stat-card stat-card--ok">
            <div class="stat-card__label">COMPLETED</div>
            <div class="stat-card__value stat-card__value--ok">${completed}</div>
          </div>
          <div class="stat-card stat-card--err">
            <div class="stat-card__label">FAILED</div>
            <div class="stat-card__value stat-card__value--err">${failed}</div>
          </div>
          <div class="stat-card stat-card--warn">
            <div class="stat-card__label">PENDING</div>
            <div class="stat-card__value stat-card__value--warn">${pending}</div>
          </div>
          <div class="stat-card stat-card--info">
            <div class="stat-card__label">SKIPPED</div>
            <div class="stat-card__value stat-card__value--info">${skipped}</div>
          </div>
        </div>
      </div>

      <div class="detail-section">
        <div class="detail-section__title">FILES (${files.length} / ${state.selectedFilesTotal})</div>
        <div class="file-list">
          <div class="file-list__header">
            <div>NAME</div>
            <div>STATUS</div>
            <div>PROGRESS</div>
            <div>SPEED</div>
            <div>SIZE</div>
            <div>RETRY</div>
          </div>
          ${files.length === 0 ? `
            <div style="padding: var(--space-4); color: var(--text-muted); font-family: var(--font-mono); font-size: 11px; text-align: center;">
              No files registered yet. Start the task to begin.
            </div>
          ` : files.map(f => {
            const fileProgress = f.file_size > 0 ? (f.downloaded_bytes / f.file_size) * 100 : 0;
            const speed = f.speed || 0;
            const speedStr = speed > 0 ? formatBytes(speed) + '/s' : '—';
            const canRetry = f.status === 'failed';
            return `
              <div class="file-row ${f.status === 'downloading' ? 'status-downloading' : ''}" title="${f.download_link ? escapeHtml(f.download_link) : ''}">
                <div class="file-row__name-wrap">
                  <div class="file-row__name" title="${escapeHtml(f.filename || f.item_url)}">
                    ${escapeHtml(f.filename || '(unnamed)')}
                  </div>
                  <div class="file-row__name file-row__name--sub" title="${escapeHtml(f.item_url)}">
                    ${escapeHtml(truncateUrl(f.item_url, 50))}
                  </div>
                </div>
                <div>
                  <span class="file-row__status file-row__status--${f.status}">${f.status}</span>
                </div>
                <div class="file-row__progress">
                  <div class="file-row__progress-bar">
                    <div class="file-row__progress-fill" style="width: ${fileProgress.toFixed(1)}%"></div>
                  </div>
                  <span style="font-size: 9px; color: var(--text-muted); margin-top: 2px; display: block;">${fileProgress.toFixed(0)}%</span>
                </div>
                <div class="file-row__speed" style="font-size:10px;color:var(--text-muted);font-family:var(--font-mono);">${speedStr}</div>
                <div class="file-row__size">${formatBytes(f.file_size)}</div>
                <div>
                  <span style="color: var(--text-muted);">${f.retry_count || 0}</span>
                  ${canRetry ? `<button class="btn btn--sm" data-retry-file="${f.id}" style="margin-left: 4px; padding: 0 4px; height: 18px; font-size: 9px;">↻</button>` : ''}
                </div>
              </div>
            `;
          }).join('')}
        </div>
        ${state.selectedFilesTotal > 50 ? `
          <div class="pagination">
            <button class="btn btn--sm ${state.selectedFilesPage === 0 ? 'disabled' : ''}" id="filePagePrev">‹ Prev</button>
            <span class="pagination__info">Page ${state.selectedFilesPage + 1} / ${Math.ceil(state.selectedFilesTotal / 50)}</span>
            <button class="btn btn--sm ${state.selectedFilesPage * 50 + 50 >= state.selectedFilesTotal ? 'disabled' : ''}" id="filePageNext">Next ›</button>
          </div>
        ` : ''}
      </div>

      <div class="detail-section">
        <div class="detail-section__title">EVENTS (${events.length})</div>
        <div class="logs">
          ${events.length === 0 ? `
            <div style="color: var(--text-muted); font-size: 11px; text-align: center; padding: var(--space-3);">No events yet</div>
          ` : events.map(e => `
            <div class="log-entry">
              <span class="log-entry__time">${formatTimeShort(e.created_at)}</span>
              <span class="log-entry__level log-entry__level--${e.level}">${e.level}</span>
              <span class="log-entry__event">${escapeHtml(e.event)}</span>
              <span class="log-entry__details">${escapeHtml(e.details || '')}</span>
            </div>
          `).join('')}
        </div>
      </div>
    </div>
  `;

  // Bind retry file buttons
  view.querySelectorAll('[data-retry-file]').forEach(btn => {
    btn.addEventListener('click', () => {
      const fileId = parseInt(btn.dataset.retryFile, 10);
      retryFile(fileId);
    });
  });

  // Bind pagination buttons
  const prevBtn = view.querySelector('#filePagePrev');
  const nextBtn = view.querySelector('#filePageNext');
  if (prevBtn) {
    prevBtn.addEventListener('click', async () => {
      if (state.selectedFilesPage > 0) {
        state.selectedFilesPage--;
        state.selectedFiles = await loadFiles(state.selectedTaskId, state.selectedFilesPage);
        renderDetail();
      }
    });
  }
  if (nextBtn) {
    nextBtn.addEventListener('click', async () => {
      const totalPages = Math.ceil(state.selectedFilesTotal / 50);
      if (state.selectedFilesPage + 1 < totalPages) {
        state.selectedFilesPage++;
        state.selectedFiles = await loadFiles(state.selectedTaskId, state.selectedFilesPage);
        renderDetail();
      }
    });
  }
}

// ============== WebSocket ==============
function connectWS() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws`;
  const ind = $('#wsIndicator');
  ind.classList.remove('connected', 'error');

  try {
    state.ws = new WebSocket(wsUrl);
  } catch (e) {
    ind.classList.add('error');
    ind.querySelector('.ws-text').textContent = 'ERROR';
    scheduleReconnect();
    return;
  }

  state.ws.onopen = () => {
    state.wsRetry = 0;
    ind.classList.add('connected');
    ind.querySelector('.ws-text').textContent = 'LIVE';
  };

  state.ws.onmessage = (ev) => {
    let payload;
    try { payload = JSON.parse(ev.data); } catch (e) { return; }
    handleWSEvent(payload);
  };

  state.ws.onerror = () => {
    ind.classList.remove('connected');
    ind.classList.add('error');
    ind.querySelector('.ws-text').textContent = 'ERROR';
  };

  state.ws.onclose = () => {
    ind.classList.remove('connected');
    ind.querySelector('.ws-text').textContent = 'OFFLINE';
    scheduleReconnect();
  };
}

function scheduleReconnect() {
  state.wsRetry = Math.min(state.wsRetry + 1, 10);
  const delay = Math.min(1000 * Math.pow(2, state.wsRetry), 30000);
  setTimeout(connectWS, delay);
}

function handleWSEvent(ev) {
  // Refresh task list on any task-level event
  if (ev.type && (
    ev.type.startsWith('task_') ||
    ev.type === 'file_registered' ||
    ev.type === 'stats'
  )) {
    // Debounced refresh
    scheduleRefresh();
  }
  // Refresh detail if event is for the currently selected task
  if (state.selectedTaskId && ev.task_id === state.selectedTaskId) {
    scheduleDetailRefresh();
  }
}

let _refreshTimer = null;
function scheduleRefresh() {
  if (_refreshTimer) clearTimeout(_refreshTimer);
  _refreshTimer = setTimeout(() => {
    loadTasks();
    _refreshTimer = null;
  }, 200);
}

let _detailTimer = null;
function scheduleDetailRefresh() {
  if (_detailTimer) clearTimeout(_detailTimer);
  _detailTimer = setTimeout(async () => {
    if (state.selectedTaskId) {
      try { await loadTask(state.selectedTaskId); } catch (e) { /* ignore */ }
    }
    _detailTimer = null;
  }, 300);
}

// ============== Modal ==============
function openModal() {
  $('#newTaskModal').setAttribute('aria-hidden', 'false');
  setTimeout(() => $('input[name="url"]').focus(), 50);
}
function closeModal() {
  $('#newTaskModal').setAttribute('aria-hidden', 'true');
  $('#newTaskForm').reset();
}

// ============== Init ==============
function init() {
  // New task button
  $('#newTaskBtn').addEventListener('click', openModal);

  // Modal close handlers
  document.querySelectorAll('[data-close]').forEach(el => {
    el.addEventListener('click', closeModal);
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeModal();
  });

  // Form submit
  $('#newTaskForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const form = e.target;
    const data = new FormData(form);
    const rawUrl = (data.get('url') || '').toString().trim();
    const urls = rawUrl.split(/\n/).map(u => u.trim()).filter(Boolean);
    if (urls.length === 0) {
      toast('URL is required', 'err');
      return;
    }
    const options = {};
    const maxRetries = data.get('max_retries');
    const connections = data.get('connections');
    const rateLimit = data.get('rate_limit');
    if (maxRetries) options.max_retries = parseInt(maxRetries, 10);
    if (connections) options.connections = parseInt(connections, 10);
    if (rateLimit)  options.rate_limit  = parseFloat(rateLimit);
    if (data.get('custom_path'))  options.custom_path = data.get('custom_path');
    if (data.get('clean_name'))  options.clean_name = true;
    if (data.get('no_album_folder'))  options.no_album_folder = true;
    if (data.get('no_download_folder')) options.no_download_folder = true;
    const ignore = (data.get('ignore') || '').toString().trim();
    const include = (data.get('include') || '').toString().trim();
    if (ignore)  options.ignore  = ignore.split(/\s+/);
    if (include) options.include = include.split(/\s+/);

    try {
      await createTask(rawUrl, options);
      closeModal();
    } catch (e) { /* toast already shown */ }
  });

  // Initial load
  loadTasks();
  connectWS();

  // Periodic refresh as a fallback (in case WS disconnects silently)
  setInterval(() => {
    if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
      loadTasks();
    }
  }, 10000);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
