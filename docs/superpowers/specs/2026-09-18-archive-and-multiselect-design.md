# Archive group + multi-select design

## Goal

Every session can be archived. Archiving just means moving the session into a
special "Archived" group that is collapsed by default. Users can select
multiple sessions (click, and shift-click for a range, like email clients)
and move them all at once — most commonly into Archived.

No migration is needed for existing stores.

## Background

The codebase already has:
- Persisted, user-created groups (`store.Group`, CRUD via `Dashboard`).
- A per-session `GroupID` (empty string = "Ungrouped").
- Drag-and-drop and a per-row `<select>` to move a session between groups.
- Client-side, non-persisted group collapse state (`ui.collapsed`).
- A *different*, pre-existing "archived" concept: `dashboard.StateArchived`,
  a derived `State` value computed at view time for any session that ended
  7+ days ago (`archiveAfter` in `internal/dashboard/dashboard.go`). This
  gates the Delete button and shows as a separate top-level nav filter. It is
  unrelated to `GroupID`.

This spec replaces that derived state with a real group membership, and adds
multi-select.

## Data model

- Reserve the group id `"archived"` as a pseudo-group, the same way `""`
  already means "Ungrouped" — it is **never** stored in `store.Data.Groups`.
  Real groups get ids of the form `g_<hex>` (see `newGroupID`), so `"archived"`
  never collides.
- `Dashboard.snapshotLocked` always appends
  `GroupView{ID: "archived", Name: "Archived"}` as the **last** entry of
  `Snapshot.Groups`. This makes it appear in the sidebar, the per-row group
  `<select>`, and drag/drop targets with no extra frontend plumbing.
- `cleanGroupNameLocked` rejects the name "Archived" (case-insensitive, same
  rule already used for duplicate names) so a user can't create a second,
  real group that collides in the sidebar.
- Frontend hides the rename/delete icons for `g.id === 'archived'`, the same
  way "Ungrouped" (which isn't a real group either) has no tools today.
- Add `store.SessionRecord.ArchiveExempt bool` (json `archiveExempt`,
  `omitempty`). Existing stores decode this as `false`, which is the correct
  default (no migration needed).

## Removing the old derived archived state

- Delete `dashboard.StateArchived` and the 7-day check inside `viewLocked`.
  `SessionView.State` becomes purely the live status (waiting/busy/idle/ended)
  again, independent of grouping, exactly like membership in any other group.
- `archiveAfter` (7 days) moves from being a view-time check to a real
  mutation performed once per poll in `ApplyPoll`: for every session with
  `EndedAt != nil`, `now.Sub(LastSeenAt) >= archiveAfter`,
  `GroupID != "archived"`, and `!ArchiveExempt`, set `GroupID = "archived"`
  and mark the poll `changed` (same pattern as the existing ended-sweep loop
  a few lines above it), so it gets persisted.
- In `UpdateSession`, whenever a session's `GroupID` is being changed away
  from `"archived"`, set `ArchiveExempt = true` first. This makes "drag it
  back out of Archived" permanent: the session is never swept back in by the
  timer again. An ended session that was never touched keeps getting
  auto-archived after 7 days even if it currently sits in some other,
  user-chosen group — matching today's reach (the timer used to apply
  regardless of group, since it was orthogonal to `GroupID`).
- `DeleteSession`'s guard changes from `State != StateArchived` to
  `rec.GroupID != "archived" || rec.EndedAt == nil` (delete only allowed for
  sessions that are both archived and no longer live).

## API

No new HTTP routes. `PATCH /api/sessions/:id {groupId}` already exists and is
reused for: manual archive/un-archive, drag-and-drop, and bulk move (fired
once per selected session id from the client).

## Frontend

### Archived as a real group, not a special view

- Remove the separate top-level "Archived" nav item, the `nav-sep` above it,
  and the `ui.view === 'archived'` special case in `visibleSessions` —
  "archived" is just one more id flowing through the existing
  `for (const g of snap.groups)` loop in `renderSidebar`, and one more
  section in the grouped "All" table view (rendered last, since it's last in
  `snap.Groups`).
- `state.ui.collapsed` initializes to `new Set(['archived'])` instead of
  `new Set()`, so it renders collapsed on first load. This is not persisted
  across reloads, consistent with how every other group's collapse state
  already works.
- Simplify `visibleSessions`: drop the two `s.state === 'archived'`
  exclusions; the existing `ui.view.startsWith('g_')` check generalizes to
  `ui.view !== 'all' && ui.view !== 'ungrouped' && s.groupId !== ui.view`,
  which correctly matches both real groups and `'archived'`.
- `renderRow`: drop the `state === 'archived'` checks. The Delete button
  condition becomes `s.groupId === 'archived' && s.state === 'ended'`.
  Copy-resume keeps its existing `state === 'ended'` condition.
- `STATE_ORDER` / `STATE_LABEL` drop the `archived` entry (no longer a
  possible `State` value).

### Multi-select (click, shift-click)

- Add a checkbox column (leftmost) to the table header and to `renderRow`.
- New UI state: `ui.selected: Set<string>` (session ids), `ui.selectAnchor:
  string | null`.
- Plain click on a row's checkbox toggles that row's membership in
  `ui.selected` and sets it as the anchor.
- Shift-click on a checkbox selects the inclusive visible range between the
  anchor and the clicked row, based on the currently rendered row order (read
  `#rows tr.session-row` DOM order at click time — this naturally respects
  whatever grouping/sort/filter is currently displayed).
- When `ui.selected.size > 0`, render a small toolbar above the table: a
  "N selected" label, a `<select>` with the same group options as the
  per-row one (Ungrouped, real groups, Archived) to bulk-move, and a "Clear"
  button.
- Bulk move: on selecting a value in the toolbar's `<select>`, fire
  `PATCH /api/sessions/:id {groupId}` once per selected id (`Promise.all`).
  Clear `ui.selected` after the moves settle, and also whenever `ui.view`
  changes (selection doesn't follow the user across views) or a selected
  session disappears from the next snapshot.

## Testing

- Go: `internal/dashboard` — auto-archive sweep moves ended+idle+7d+exempt-free
  sessions into `"archived"`; a manual move out sets `ArchiveExempt` and
  survives a later poll past the 7-day mark; `DeleteSession` gating; the
  synthesized `"archived"` group appears in every snapshot and can't be
  renamed/deleted/duplicated by name.
- JS: no existing test harness for `internal/web/static` — verify manually
  via `go build` + the dev run command (archive a live session, un-archive
  it, drag-select a range, bulk-move, reload to confirm collapse-by-default).
