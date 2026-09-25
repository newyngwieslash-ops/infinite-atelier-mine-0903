-- WP-18 (P3 item 22): FR-120's summary ladder gets its third rung, and every rung gets an
-- explicit level.
--
-- # Why a column, when the memory TYPE already separates the rungs
--
-- It did, while there were two. Level one read EPISODIC rows and level two read SUMMARY rows, so
-- `memory_type` was the discriminator and `UnsummarisedItems` took a type for exactly that reason.
-- A third rung breaks the coincidence: level two and level three BOTH read summaries, so the type
-- can no longer say which rung a window is for.
--
-- # What the alternative would have cost
--
-- The rung is derivable from `memory_summary_sources` — a level-two summary's children are episodic
-- memories, a level-three summary's children are summaries — and the derivation is what this
-- migration's backfill uses. But as a QUERY it is recursive: telling rung 3 from rung 4 means asking
-- what a summary's children's children are, and the build only has three rungs today while a fourth
-- would silently mean "whatever the query happened to match". A column states the fact once, at the
-- write, where the caller already knows it.
--
-- # Why the CHECK is safe here
--
-- `summary_level` is a small closed set, and the CHECK is what keeps a later writer from inventing a
-- fifth rung in a column three readers trust. SQLite accepts a CHECK in ADD COLUMN (it refuses one
-- only on a table being rebuilt with a violation), and the default is what every non-summary row
-- gets — 0 means "not a summary", which is the honest value for an episodic memory.
--
-- # The backfill
--
-- Rows written before this migration carry level 0, which would make every existing summary
-- unattachable to a rung and invisible to every window. The backfill DERIVES the level the way the
-- old code decided it: a summary whose children are not themselves summaries is level one, and one
-- whose children are summaries is level two. Level three cannot exist yet — it is what this package
-- adds — so no row is left needing it.
ALTER TABLE memory_items ADD COLUMN summary_level INTEGER NOT NULL DEFAULT 0
    CHECK (summary_level BETWEEN 0 AND 3);

UPDATE memory_items SET summary_level = 1
WHERE memory_type = 'summary'
  AND NOT EXISTS (
    SELECT 1 FROM memory_summary_sources s
    JOIN memory_items child ON child.id = s.source_memory_id
    WHERE s.summary_id = memory_items.id AND child.memory_type = 'summary');

UPDATE memory_items SET summary_level = 2
WHERE memory_type = 'summary'
  AND EXISTS (
    SELECT 1 FROM memory_summary_sources s
    JOIN memory_items child ON child.id = s.source_memory_id
    WHERE s.summary_id = memory_items.id AND child.memory_type = 'summary');

-- The ladder's reads are "the uncondensed rows of one level in one scope", which is this index.
CREATE INDEX idx_memory_items_summary_level
    ON memory_items(scope_project, summary_level, summarized);
