// render.js: pure functions that turn a snapshot plus UI state into HTML strings.
// Every dynamic value goes through esc().

const STATE_ORDER = { waiting: 0, busy: 1, idle: 2, other: 2, ended: 3 };
const STATE_LABEL = { waiting: 'Needs input', busy: 'Working', idle: 'Idle', ended: 'Ended' };

export function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

export function relTime(iso, now = Date.now()) {
  if (!iso) return '—';
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}

// statusKey maps a session to a counter/filter key. 'other' counts as idle.
export function statusKey(s) {
  return s.state === 'other' ? 'idle' : s.state;
}

export function counts(sessions) {
  const c = { waiting: 0, busy: 0, idle: 0, ended: 0 };
  for (const s of sessions) {
    const key = statusKey(s);
    if (key in c) c[key]++;
  }
  return c;
}

export function sortSessions(list) {
  return [...list].sort((a, b) =>
    (STATE_ORDER[a.state] - STATE_ORDER[b.state]) || (Date.parse(b.startedAt) - Date.parse(a.startedAt)));
}

export function visibleSessions(snap, ui) {
  const q = ui.search.trim().toLowerCase();
  return snap.sessions.filter((s) => {
    if (ui.view === 'ungrouped' && s.groupId !== '') return false;
    if (ui.view !== 'all' && ui.view !== 'ungrouped' && s.groupId !== ui.view) return false;
    if (ui.status && statusKey(s) !== ui.status) return false;
    if (q && ![s.name, s.cwd, s.note].some((v) => (v || '').toLowerCase().includes(q))) return false;
    return true;
  });
}

export function renderCounters(c, active) {
  const chip = (key, label) =>
    `<button type="button" class="chip st-${key} ${active === key ? 'is-active' : ''}" data-action="filter-status" data-status="${key}">` +
    `<span class="chip-dot"></span>${label}<b>${c[key]}</b></button>`;
  return chip('waiting', 'Needs input') + chip('busy', 'Working') + chip('idle', 'Idle') + chip('ended', 'Ended');
}

export function renderHealth(snap, connected, now) {
  if (!connected) return '<span class="health is-bad">● Dashboard disconnected</span>';
  if (snap.pollError) {
    return `<span class="health is-bad" title="${esc(snap.pollError)}">● claude CLI unreachable: ${esc(snap.pollError)}</span>`;
  }
  if (!snap.lastPollAt) return '<span class="health">● Waiting for the first poll…</span>';
  return `<span class="health is-ok">● Live · polled ${relTime(snap.lastPollAt, now)}</span>`;
}

export function renderSidebar(snap, ui) {
  const item = (view, label, list, { dropGroup, groupId } = {}) => {
    const waiting = list.some((s) => s.state === 'waiting');
    const renaming = groupId && ui.renaming === groupId;
    const labelHTML = renaming
      ? `<input class="nav-rename" data-group="${esc(groupId)}" value="${esc(label)}" maxlength="60">`
      : `<span class="nav-label">${esc(label)}</span>`;
    const tools = groupId && !renaming
      ? `<span class="nav-tools"><button type="button" class="icon-btn" data-action="rename-group" data-group="${esc(groupId)}" title="Rename">✎</button>` +
        `<button type="button" class="icon-btn" data-action="delete-group" data-group="${esc(groupId)}" title="Delete">×</button></span>`
      : '';
    return `<li class="nav-item ${ui.view === view ? 'is-active' : ''}" data-action="select-view" data-view="${esc(view)}"` +
      `${dropGroup !== undefined ? ` data-drop-group="${esc(dropGroup)}"` : ''}>` +
      `${labelHTML}${waiting ? '<span class="attn-dot" title="Needs input"></span>' : ''}${tools}` +
      `<span class="nav-count">${list.length}</span></li>`;
  };
  let html = item('all', 'All', snap.sessions);
  html += item('ungrouped', 'Ungrouped', snap.sessions.filter((s) => s.groupId === ''), { dropGroup: '' });
  for (const g of snap.groups) {
    // The synthesized Archived pseudo-group (id 'archived') isn't a real,
    // renameable/deletable group, so it gets no rename/delete tools.
    const groupId = g.id === 'archived' ? undefined : g.id;
    html += item(g.id, g.name, snap.sessions.filter((s) => s.groupId === g.id), { dropGroup: g.id, groupId });
  }
  return html;
}

export function resumeCommand(s) {
  return `cd '${s.cwd.replace(/'/g, "'\\''")}' && claude --resume ${s.id}`;
}

export function renderRow(s, snap, ui, now, dropGroup) {
  const key = statusKey(s);
  const label = s.state === 'other' ? (s.rawStatus || 'unknown') : STATE_LABEL[s.state];
  const editing = ui.editingNote === s.id;
  const id = esc(s.id);
  const options = ['<option value="">Ungrouped</option>']
    .concat(snap.groups.map((g) => `<option value="${esc(g.id)}"${g.id === s.groupId ? ' selected' : ''}>${esc(g.name)}</option>`))
    .join('');
  const actions = [];
  if (s.canFocus) actions.push(`<button type="button" class="btn" data-action="focus" data-id="${id}">Focus</button>`);
  if (s.recap) actions.push(`<button type="button" class="btn" data-action="view-recap" data-id="${id}">Recap</button>`);
  if (s.state === 'ended') {
    actions.push(`<button type="button" class="btn" data-action="copy-resume" data-id="${id}">Copy resume command</button>`);
  }
  if (s.groupId === 'archived' && s.state === 'ended') {
    actions.push(`<button type="button" class="btn btn-danger" data-action="delete-session" data-id="${id}">Delete</button>`);
  }
  const lastSeen = s.state === 'ended' ? ` <span class="muted small">last seen ${relTime(s.lastSeenAt, now)}</span>` : '';
  const note = editing
    ? `<textarea class="note-editor" data-id="${id}" maxlength="10000">${esc(s.note)}</textarea>`
    : (s.note ? `<span class="note">${esc(s.note)}</span>` : '<span class="muted">—</span>');
  const checked = ui.selected?.has(s.id) ? ' checked' : '';
  return `<tr class="session-row st-row-${key}${s.canFocus ? ' can-focus' : ''}" data-id="${id}"` +
    `${dropGroup !== undefined ? ` data-drop-group="${esc(dropGroup)}"` : ''} draggable="${editing ? 'false' : 'true'}"` +
    ` title="${s.canFocus ? 'Click to focus the terminal' : 'no terminal focus'}">` +
    `<td class="c-select"><input type="checkbox" class="row-select" data-action="select-row" data-id="${id}"${checked}></td>` +
    `<td><span class="pill st-${key}">${esc(label)}</span>${lastSeen}</td>` +
    `<td class="name" title="${esc(s.name)}">${esc(s.name)}</td>` +
    `<td title="${esc(s.title)}">${s.title ? esc(s.title) : '<span class="muted">—</span>'}</td>` +
    `<td class="note-cell" data-action="edit-note" data-id="${id}" title="${esc(s.note)}">${note}</td>` +
    `<td><select class="group-select" data-action="move" data-id="${id}">${options}</select></td>` +
    `<td class="muted">${relTime(s.startedAt, now)}</td>` +
    `<td class="actions">${actions.join('')}</td></tr>`;
}

export function renderRows(snap, ui, now) {
  const list = visibleSessions(snap, ui);
  const empty = '<tr class="empty-row"><td colspan="8">No sessions match.</td></tr>';
  if (ui.view !== 'all') {
    return list.length ? sortSessions(list).map((s) => renderRow(s, snap, ui, now)).join('') : empty;
  }
  const filtering = Boolean(ui.status || ui.search.trim());
  let html = '';
  for (const g of [{ id: '', name: 'Ungrouped' }, ...snap.groups]) {
    const rows = sortSessions(list.filter((s) => s.groupId === g.id));
    // Empty groups stay visible (so they accept drops) unless a filter is active.
    if (rows.length === 0 && (g.id === '' || filtering)) continue;
    const waiting = rows.filter((s) => s.state === 'waiting').length;
    const collapsed = ui.collapsed.has(g.id);
    html += `<tr class="group-row" data-action="toggle-group" data-group="${esc(g.id)}" data-drop-group="${esc(g.id)}">` +
      `<td colspan="8"><span class="caret">${collapsed ? '▸' : '▾'}</span> <strong>${esc(g.name)}</strong> ` +
      `<span class="muted">${rows.length} session${rows.length === 1 ? '' : 's'}</span>` +
      `${waiting ? `<span class="badge">● ${waiting} need${waiting === 1 ? 's' : ''} input</span>` : ''}</td></tr>`;
    if (!collapsed) html += rows.map((s) => renderRow(s, snap, ui, now, g.id)).join('');
  }
  return html || empty;
}

export function renderSelectionBar(snap, ui) {
  if (ui.selected.size === 0) return '';
  const options = snap.groups.map((g) => `<option value="${esc(g.id)}">${esc(g.name)}</option>`).join('');
  return `<div class="selection-bar">` +
    `<span>${ui.selected.size} selected</span>` +
    `<select class="bulk-move" data-action="bulk-move"><option value="" selected disabled>Move to…</option>` +
    `<option value="">Ungrouped</option>${options}</select>` +
    `<button type="button" class="btn" data-action="clear-selection">Clear</button>` +
    `</div>`;
}
