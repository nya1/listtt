// app.js: state, the live event stream, render orchestration, and navigation.
import { installActions } from './actions.js';
import { installDnD } from './dnd.js';
import { counts, renderCounters, renderHealth, renderRows, renderSidebar } from './render.js';

export const state = {
  snap: { groups: [], sessions: [], lastPollAt: null, pollError: null, home: '' },
  connected: false,
  ui: {
    view: 'all',            // 'all' | 'ungrouped' | 'archived' | group id
    status: null,           // null | 'waiting' | 'busy' | 'idle' | 'ended'
    search: '',
    collapsed: new Set(),   // group ids ('' = Ungrouped) collapsed in the All view
    editingNote: null,      // session id whose note editor is open
    renaming: null,         // group id being renamed in the sidebar
    dragging: false,        // true while a row is being dragged (Task 11)
  },
};

const $ = (id) => document.getElementById(id);

export function render() {
  const { snap, ui } = state;
  if (ui.dragging) return; // dnd.js calls render() again when the drag ends
  if (!['all', 'ungrouped', 'archived'].includes(ui.view) && !snap.groups.some((g) => g.id === ui.view)) {
    ui.view = 'all';
  }
  const now = Date.now();
  $('counters').innerHTML = renderCounters(counts(snap.sessions), ui.status);
  $('health').innerHTML = renderHealth(snap, state.connected, now);
  if (!ui.renaming) $('nav').innerHTML = renderSidebar(snap, ui);

  const rows = $('rows');
  // An open group <select> survives a poll update, like an open note editor below --
  // replacing it mid-selection would close the browser's native dropdown. A merely
  // focused button (e.g. Focus, just clicked) has no state to protect and must not
  // block the row from updating.
  const active = document.activeElement;
  if (!ui.editingNote && active.tagName === 'SELECT' && rows.contains(active)) return;
  const fresh = document.createElement('tbody');
  fresh.innerHTML = renderRows(snap, ui, now);
  // The editor row is the one that already contains the open textarea.
  const editor = ui.editingNote
    ? rows.querySelector(`textarea.note-editor[data-id="${CSS.escape(ui.editingNote)}"]`)?.closest('tr')
    : null;
  if (!editor) {
    rows.replaceChildren(...fresh.children);
    return;
  }
  // Keep the edited row attached (so it keeps focus) and swap the rows around it.
  const replacement = fresh.querySelector(`tr[data-id="${CSS.escape(ui.editingNote)}"]`);
  if (!replacement) return; // the row would disappear: leave the table alone until the editor closes
  const before = [];
  const after = [];
  let seen = false;
  for (const node of [...fresh.children]) {
    if (node === replacement) seen = true;
    else (seen ? after : before).push(node);
  }
  for (const node of [...rows.children]) if (node !== editor) node.remove();
  editor.before(...before);
  editor.after(...after);
}

function connect() {
  const events = new EventSource('/events');
  events.onopen = () => { state.connected = true; render(); };
  events.onmessage = (e) => {
    state.snap = JSON.parse(e.data);
    state.connected = true;
    render();
  };
  events.onerror = () => { state.connected = false; render(); }; // EventSource reconnects by itself
}

document.addEventListener('click', (e) => {
  const el = e.target.closest('[data-action]');
  if (!el) return;
  const { ui } = state;
  switch (el.dataset.action) {
    case 'select-view':
      if (e.target.closest('.nav-tools, .nav-rename')) return;
      ui.view = el.dataset.view;
      render();
      break;
    case 'filter-status':
      ui.status = ui.status === el.dataset.status ? null : el.dataset.status;
      render();
      break;
    case 'toggle-group':
      if (ui.collapsed.has(el.dataset.group)) ui.collapsed.delete(el.dataset.group);
      else ui.collapsed.add(el.dataset.group);
      render();
      break;
  }
});

$('search').addEventListener('input', (e) => {
  state.ui.search = e.target.value;
  render();
});

// Keep "polled Ns ago" current between snapshots.
setInterval(() => { $('health').innerHTML = renderHealth(state.snap, state.connected, Date.now()); }, 1000);

installActions({ state, render });
installDnD({ state, render });
connect();
