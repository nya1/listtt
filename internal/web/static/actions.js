// actions.js: user actions (focus, notes, groups, moving, resume, delete).
import { api, toast } from './api.js';
import { renderSidebar, resumeCommand } from './render.js';

async function attempt(promise) {
  try {
    return await promise;
  } catch (err) {
    toast(err.message);
    return undefined;
  }
}

const sessionPath = (id) => `/api/sessions/${encodeURIComponent(id)}`;
const groupPath = (id) => `/api/groups/${encodeURIComponent(id)}`;

export function installActions({ state, render }) {
  const { ui } = state;
  const sessionById = (id) => state.snap.sessions.find((s) => s.id === id);
  const groupById = (id) => state.snap.groups.find((g) => g.id === id);

  function openNoteEditor(id) {
    if (ui.editingNote) return; // a click on another note first blurs (and closes) the open editor
    ui.editingNote = id;
    render();
    const textarea = document.querySelector(`textarea.note-editor[data-id="${CSS.escape(id)}"]`);
    if (textarea) {
      textarea.focus();
      textarea.setSelectionRange(textarea.value.length, textarea.value.length);
    }
  }

  async function closeNoteEditor(textarea) {
    const id = textarea.dataset.id;
    const cancelled = textarea.dataset.cancel === '1';
    const note = textarea.value;
    ui.editingNote = null;
    render();
    const session = sessionById(id);
    if (!cancelled && session && note !== session.note) {
      await attempt(api('PATCH', sessionPath(id), { note }));
    }
  }

  const recapModal = document.getElementById('recap-modal');
  const recapText = document.getElementById('recap-text');

  function openRecap(id) {
    const session = sessionById(id);
    if (!session?.recap) return;
    recapText.textContent = session.recap;
    recapModal.hidden = false;
  }

  function closeRecap() {
    recapModal.hidden = true;
  }

  function startRename(groupId) {
    ui.renaming = groupId;
    // Draw the input once. render() leaves #nav alone while ui.renaming is set.
    document.getElementById('nav').innerHTML = renderSidebar(state.snap, ui);
    const input = document.querySelector(`input.nav-rename[data-group="${CSS.escape(groupId)}"]`);
    input?.focus();
    input?.select();
  }

  async function finishRename(input) {
    const id = input.dataset.group;
    const cancelled = input.dataset.cancel === '1';
    const name = input.value;
    ui.renaming = null;
    render();
    const group = groupById(id);
    if (!cancelled && group && name.trim() !== group.name) {
      await attempt(api('PATCH', groupPath(id), { name }));
    }
  }

  document.addEventListener('click', async (e) => {
    const el = e.target.closest('[data-action]');
    const action = el?.dataset.action;

    if (!el) {
      const row = e.target.closest('tr.session-row');
      const session = row && sessionById(row.dataset.id);
      if (session?.canFocus) await attempt(api('POST', `${sessionPath(session.id)}/focus`));
      return;
    }
    switch (action) {
      case 'focus':
        await attempt(api('POST', `${sessionPath(el.dataset.id)}/focus`));
        break;
      case 'edit-note':
        openNoteEditor(el.dataset.id);
        break;
      case 'view-recap':
        openRecap(el.dataset.id);
        break;
      case 'close-recap':
        closeRecap();
        break;
      case 'copy-resume': {
        const session = sessionById(el.dataset.id);
        if (!session) break;
        const command = resumeCommand(session);
        try {
          await navigator.clipboard.writeText(command);
          toast('Resume command copied');
        } catch {
          window.prompt('Copy the resume command:', command);
        }
        break;
      }
      case 'delete-session': {
        const session = sessionById(el.dataset.id);
        if (session && window.confirm(`Delete "${session.name}" and its note permanently?`)) {
          await attempt(api('DELETE', sessionPath(session.id)));
        }
        break;
      }
      case 'rename-group':
        startRename(el.dataset.group);
        break;
      case 'delete-group': {
        const group = groupById(el.dataset.group);
        if (group && window.confirm(`Delete group "${group.name}"? Its sessions move to Ungrouped.`)) {
          await attempt(api('DELETE', groupPath(group.id)));
        }
        break;
      }
    }
  });

  // Moving with the keyboard-accessible select.
  document.addEventListener('change', async (e) => {
    const select = e.target.closest?.('select.group-select');
    if (!select) return;
    // render()'s open-select guard exists to protect a dropdown the user is still
    // choosing from; 'change' means the picker already closed, so release focus now
    // or the guard would keep blocking the row's move until the user clicks away.
    select.blur();
    const result = await attempt(api('PATCH', sessionPath(select.dataset.id), { groupId: select.value }));
    if (result === undefined) render(); // reset the select after a failure
  });

  document.addEventListener('focusout', (e) => {
    const textarea = e.target.closest?.('textarea.note-editor');
    if (textarea) {
      closeNoteEditor(textarea);
      return;
    }
    const input = e.target.closest?.('input.nav-rename');
    if (input) finishRename(input);
  });

  document.addEventListener('keydown', (e) => {
    if (!e.target.matches?.('textarea.note-editor, input.nav-rename')) return;
    if (e.key === 'Escape') {
      e.target.dataset.cancel = '1';
      e.target.blur();
    } else if (e.key === 'Enter' && e.target.matches('input.nav-rename')) {
      e.preventDefault();
      e.target.blur();
    }
  });

  // Recap popup: click the backdrop or press Escape to close.
  recapModal.addEventListener('click', (e) => {
    if (e.target === recapModal) closeRecap();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !recapModal.hidden) closeRecap();
  });

  // New group: an inline input. Enter creates the group, Esc or leaving it empty cancels.
  const form = document.getElementById('new-group-form');
  const nameInput = document.getElementById('new-group-name');
  document.getElementById('new-group-btn').addEventListener('click', () => {
    form.hidden = false;
    nameInput.value = '';
    nameInput.focus();
  });
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!nameInput.value.trim()) {
      form.hidden = true;
      return;
    }
    const group = await attempt(api('POST', '/api/groups', { name: nameInput.value }));
    if (group) {
      nameInput.value = '';
      form.hidden = true;
    }
  });
  nameInput.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') form.hidden = true;
  });
  nameInput.addEventListener('blur', () => {
    if (!nameInput.value.trim()) form.hidden = true;
  });
}
