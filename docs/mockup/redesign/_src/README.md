# Mockup sources

The redesign mockups and the implementation tracker are generated from these
Python scripts; edit them, not the HTML.

- `chrome.py` — the shared head, banner, app bar and left-hand pane; `OUT` is
  where pages are written.
- One script per screen (`today.py`, `ahead.py`, …), `plan_data.py` and
  `plan.py` for the tracker (`plan.html`).
- `python3 track.py p3 2 done "note"` sets a task's status and re-renders the
  tracker.
- `mockcheck.cjs` runs axe on every page, light and dark, at 1440 and 390 px.
  Its paths point at a local axe copy and a Playwright Chromium; adjust them.

Run a script from this folder: `python3 today.py`. `OUT` in `chrome.py` must
point at the redesign folder on your machine.
