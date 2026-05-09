-- 004: Indexes for the background pruner.
--
-- The pruner (app/prune) deletes oldest-first by created_at to enforce
-- MaxMessages / MaxCalls caps and to enforce the MaxAge TTL. Without these
-- indexes, both queries do full table scans, which is fine for a few
-- hundred rows but degrades quickly past that.
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_calls_created_at    ON calls(created_at);
