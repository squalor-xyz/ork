---
{
  "id": 149,
  "title": "How should the Overview lay out Needs you, and which keys drive it?",
  "status": "open",
  "key": "ui.overview-layout",
  "tags": [
    "ui"
  ],
  "asked_by": "claude",
  "created": "2026-10-22",
  "context": "UI review pass 2026-10-22 (rule u10) from an owner screenshot of the Overview. Found: the project column repeats on every row and is sized to the longest registered name (internal/tui/overview.go:191-194); decision titles are ragged and every row repeats '? decision'; held rows repeat '(needs-owner)'; stopped runs show a ticking elapsed time; the detail line repeats the row; sidebar rows show 0; 'pending 0' and slice-number placeholder text show to the user; the cursor bar stops at the text end; 1-9 jump in registry order while the sidebar is sorted needs-first (record 82); enter on a decision or held row only selects its project. Owner chose the grouped option in session; this record holds it for decision. It also asks to revise record 82 (1-9 registry order) and add keys beside record 112.",
  "options": [
    {
      "label": "grouped",
      "summary": "Needs you splits into Decisions, Held, Stopped (and Paused, Approve) sub-groups with counts; each row keeps a one-char symbol (?, #, x); aligned columns; project column only on the all-projects view, sized to projects with rows; headings carry counts; quiet sidebar (no zero counts, Needs you total, right-aligned badges); box-drawing rules with an ASCII fallback; richer detail line (decision status, option count, recommendation; engine run id and wait); full-width cursor bar; status line hides pending at 0. Keys: a context hint bar; enter on a decision opens its options and recommendation in the pane, on a held item its Roadmap detail; 1-9 follow the visible sidebar order and 0 selects Needs you; g/G and home/end first/last row, pgup/pgdown page rows when no pane is open, { and } jump between groups",
      "tradeoffs": "Clearest scan and keys that match what is on screen; rewrites most goldens and revises record 82"
    },
    {
      "label": "flat-tidy",
      "summary": "Keep one flat Needs you list with the kind column; align columns, drop the redundant project column and '(needs-owner)', add heading counts; keys unchanged",
      "tradeoffs": "Smaller diff; the repeated kind labels and the 1-9 mismatch stay"
    },
    {
      "label": "keep",
      "summary": "Keep the current layout and keys",
      "tradeoffs": "No work; the problems above stay"
    }
  ],
  "recommendation": {
    "label": "grouped",
    "reasoning": "Needs-you first is the dashboard's first principle; grouping and aligned columns make it scannable, and keys that follow the screen remove the 1-9 surprise."
  },
  "refs": [
    "S84",
    "S85",
    "S33",
    "adj 82",
    "adj 112"
  ],
  "history": [
    {
      "date": "2026-10-22",
      "event": "ask"
    }
  ]
}
---
