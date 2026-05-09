CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS message_tags (
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES tags(id),
    PRIMARY KEY (message_id, tag_id)
);

CREATE TABLE IF NOT EXISTS call_tags (
    call_id INTEGER NOT NULL REFERENCES calls(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id),
    PRIMARY KEY (call_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_message_tags_tag ON message_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_call_tags_tag    ON call_tags(tag_id);
