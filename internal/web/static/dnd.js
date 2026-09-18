// dnd.js: drag a session row (or, if it's part of the current checkbox
// selection, every selected row) onto a group in the sidebar, or onto a
// group section in the All view.
import { api, toast } from './api.js';
import { esc } from './render.js';

export function installDnD({ state, render }) {
  let draggedIds = null;
  let targetKey = null; // `${containerId}:${groupId}`, identifies the whole highlighted section
  let targetEls = [];
  let ghostEl = null;

  // Highlights every element sharing el's data-drop-group within the same
  // container (the sidebar nav, or the main table), so a group's header row
  // and all its session rows light up together as one section.
  function setTarget(el) {
    const container = el?.closest('#nav, #rows');
    const key = container ? `${container.id}:${el.dataset.dropGroup}` : null;
    if (key === targetKey) return;
    targetEls.forEach((n) => n.classList.remove('drop-target'));
    targetEls = [];
    targetKey = key;
    if (!container) return;
    targetEls = Array.from(container.querySelectorAll(`[data-drop-group="${CSS.escape(el.dataset.dropGroup)}"]`));
    targetEls.forEach((n) => n.classList.add('drop-target'));
  }

  // A custom drag image so dragging more than one row shows a small stack of
  // cards (one per dragged session, capped, plus a "+N more" card) trailing
  // the cursor, instead of the browser's default single-row snapshot.
  function setDragGhost(e, ids) {
    const shown = ids.slice(0, 4);
    const ghost = document.createElement('div');
    ghost.className = 'drag-ghost';
    ghost.innerHTML = shown.map((id, i) => {
      const name = state.snap.sessions.find((s) => s.id === id)?.name ?? id;
      return `<div class="drag-ghost-card" style="--i:${i}">${esc(name)}</div>`;
    }).join('') + (ids.length > shown.length ? `<div class="drag-ghost-card drag-ghost-more">+${ids.length - shown.length} more</div>` : '');
    document.body.appendChild(ghost);
    e.dataTransfer.setDragImage(ghost, 16, 16);
    ghostEl = ghost;
  }

  function endDrag() {
    setTarget(null);
    document.querySelectorAll('.is-dragging').forEach((node) => node.classList.remove('is-dragging'));
    ghostEl?.remove();
    ghostEl = null;
    draggedIds = null;
    state.ui.dragging = false;
    render(); // apply any snapshot that arrived during the drag
  }

  document.addEventListener('dragstart', (e) => {
    const row = e.target.closest?.('tr.session-row');
    if (!row || row.getAttribute('draggable') !== 'true') return;
    const id = row.dataset.id;
    draggedIds = state.ui.selected.has(id) && state.ui.selected.size > 1 ? [...state.ui.selected] : [id];
    state.ui.dragging = true;
    for (const did of draggedIds) {
      document.querySelector(`tr.session-row[data-id="${CSS.escape(did)}"]`)?.classList.add('is-dragging');
    }
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', draggedIds.join(','));
    if (draggedIds.length > 1) setDragGhost(e, draggedIds);
    // The drag image is captured synchronously; the ghost node isn't needed after this tick.
    setTimeout(() => { ghostEl?.remove(); ghostEl = null; }, 0);
  });

  document.addEventListener('dragover', (e) => {
    if (!draggedIds) return;
    const el = e.target.closest?.('[data-drop-group]');
    setTarget(el);
    if (el) {
      e.preventDefault(); // allow the drop here; everywhere else shows "not allowed"
      e.dataTransfer.dropEffect = 'move';
    }
  });

  document.addEventListener('drop', async (e) => {
    const el = e.target.closest?.('[data-drop-group]');
    if (!draggedIds || !el) return;
    e.preventDefault();
    const ids = draggedIds;
    const groupId = el.dataset.dropGroup;
    endDrag();
    state.ui.selected.clear();
    state.ui.selectAnchor = null;
    const moving = ids.filter((id) => state.snap.sessions.find((s) => s.id === id)?.groupId !== groupId);
    if (!moving.length) return; // dropping on the current group does nothing
    const results = await Promise.allSettled(moving.map((id) => api('PATCH', `/api/sessions/${encodeURIComponent(id)}`, { groupId })));
    const failed = results.filter((r) => r.status === 'rejected');
    if (failed.length) toast(failed[0].reason?.message ?? 'Move failed');
    render();
  });

  // Fires after drop, and also when a drag is cancelled (Esc, or released outside a target).
  document.addEventListener('dragend', () => {
    if (draggedIds) endDrag();
  });
}
