CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_sid TEXT UNIQUE NOT NULL,
    provider TEXT NOT NULL,
    from_number TEXT NOT NULL,
    to_number TEXT NOT NULL,
    body TEXT,
    status TEXT NOT NULL,
    callback_url TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS calls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    call_sid TEXT UNIQUE NOT NULL,
    provider TEXT NOT NULL,
    from_number TEXT NOT NULL,
    to_number TEXT NOT NULL,
    status TEXT NOT NULL,
    callback_url TEXT,
    twiml_url TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS delivery_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_sid TEXT,
    call_sid TEXT,
    event_type TEXT NOT NULL,
    status TEXT NOT NULL,
    callback_sent BOOLEAN DEFAULT FALSE,
    callback_response TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS callback_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_url TEXT NOT NULL,
    payload TEXT NOT NULL,
    status_code INTEGER,
    response_body TEXT,
    attempt_number INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
