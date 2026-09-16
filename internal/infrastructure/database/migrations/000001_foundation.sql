CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    checksum TEXT NOT NULL CHECK (length(checksum) = 64),
    applied_at TEXT NOT NULL
);

CREATE TABLE file_objects (
    hash TEXT PRIMARY KEY CHECK (length(hash) = 64),
    storage_key TEXT NOT NULL UNIQUE CHECK (length(storage_key) = 64),
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at TEXT NOT NULL
);

CREATE TABLE file_references (
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    file_hash TEXT NOT NULL REFERENCES file_objects(hash) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_type, owner_id, file_hash)
);
