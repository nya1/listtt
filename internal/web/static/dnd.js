// dnd.js: drag a session row onto a group in the sidebar, or onto a group section in the All view.
import { api, toast } from './api.js';

export function installDnD({ state, render }) {
  let draggedId = null;
  let targetKey = null; // `${containerId}:${groupId}`, identifies the whole highlighted section
  let targetEls = [];

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

  function endDrag() {
    setTarget(null);
    document.querySelectorAll('.is-dragging').forEach((node) => node.classList.remove('is-dragging'));
    draggedId = null;
    state.ui.dragging = false;
    render(); // apply any snapshot that arrived during the drag
  }

  document.addEventListener('dragstart', (e) => {
    const row = e.target.closest?.('tr.session-row');
    if (!row || row.getAttribute('draggable') !== 'true') return;
    draggedId = row.dataset.id;
    state.ui.dragging = true;
    row.classList.add('is-dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', draggedId);
  });

  document.addEventListener('dragover', (e) => {
    if (!draggedId) return;
    const el = e.target.closest?.('[data-drop-group]');
    setTarget(el);
    if (el) {
      e.preventDefault(); // allow the drop here; everywhere else shows "not allowed"
      e.dataTransfer.dropEffect = 'move';
    }
  });

  document.addEventListener('drop', async (e) => {
    const el = e.target.closest?.('[data-drop-group]');
    if (!draggedId || !el) return;
    e.preventDefault();
    const id = draggedId;
    const groupId = el.dataset.dropGroup;
    const session = state.snap.sessions.find((s) => s.id === id);
    endDrag();
    if (!session || session.groupId === groupId) return; // dropping on the current group does nothing
    try {
      await api('PATCH', `/api/sessions/${encodeURIComponent(id)}`, { groupId });
    } catch (err) {
      toast(err.message);
    }
  });

  // Fires after drop, and also when a drag is cancelled (Esc, or released outside a target).
  document.addEventListener('dragend', () => {
    if (draggedId) endDrag();
  });
}
