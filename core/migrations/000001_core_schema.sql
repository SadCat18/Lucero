-- Lucero
-- Migration: 000001_core_schema.sql
-- Core Domain Schema v0.1
--
-- All timestamps are Unix milliseconds (UTC).
-- All public domain IDs are UUIDv7 strings.
--
-- Runtime should execute on every connection:
-- PRAGMA foreign_keys = ON;
-- PRAGMA busy_timeout = 5000;
--
-- Recommended database-level settings:
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = FULL;
PRAGMA busy_timeout = 5000;

BEGIN IMMEDIATE;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version             INTEGER PRIMARY KEY,
    name                TEXT NOT NULL,
    applied_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE principals (
    id                  TEXT PRIMARY KEY,
    principal_type      TEXT NOT NULL CHECK (principal_type IN (
                            'USER','SYSTEM','ADAPTER','PLUGIN','DEVICE','API_TOKEN'
                        )),
    display_name        TEXT NOT NULL,
    enabled             INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE trust_domains (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL UNIQUE,
    description         TEXT,
    security_policy_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(security_policy_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    archived_at         INTEGER
) STRICT;

CREATE TABLE projects (
    id                  TEXT PRIMARY KEY,
    trust_domain_id     TEXT REFERENCES trust_domains(id) ON DELETE SET NULL,
    name                TEXT NOT NULL,
    canonical_path      TEXT,
    project_type        TEXT NOT NULL DEFAULT 'UNKNOWN',
    status              TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN (
                            'ACTIVE','ARCHIVED','UNAVAILABLE','UNKNOWN'
                        )),
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    archived_at         INTEGER
) STRICT;

CREATE UNIQUE INDEX idx_projects_canonical_path
    ON projects(canonical_path)
    WHERE canonical_path IS NOT NULL AND archived_at IS NULL;

CREATE TABLE project_paths (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path                TEXT NOT NULL,
    path_kind           TEXT NOT NULL CHECK (path_kind IN (
                            'PRIMARY','ALIAS','DETECTED'
                        )),
    normalized_path     TEXT NOT NULL,
    source              TEXT,
    created_at          INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX idx_project_paths_normalized
    ON project_paths(normalized_path);

CREATE INDEX idx_project_paths_project
    ON project_paths(project_id);

CREATE TABLE adapter_instances (
    id                  TEXT PRIMARY KEY,
    adapter_key         TEXT NOT NULL UNIQUE,
    display_name        TEXT NOT NULL,
    adapter_version     TEXT,
    protocol_version    TEXT,
    software_version    TEXT,
    enabled             INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    health              TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (health IN (
                            'UNKNOWN','STARTING','HEALTHY','DEGRADED',
                            'UNAVAILABLE','STOPPED'
                        )),
    effective_backend   TEXT,
    last_heartbeat_at   INTEGER,
    last_healthcheck_at INTEGER,
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE adapter_capabilities (
    id                  TEXT PRIMARY KEY,
    adapter_instance_id TEXT NOT NULL REFERENCES adapter_instances(id) ON DELETE CASCADE,
    capability_name     TEXT NOT NULL,
    support_level       TEXT NOT NULL CHECK (support_level IN (
                            'NATIVE','COMPATIBLE','CLI','LOCAL_DATA',
                            'UI_AUTOMATION','READ_ONLY','EXPERIMENTAL',
                            'UNSUPPORTED'
                        )),
    effective_backend   TEXT,
    health              TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (health IN (
                            'UNKNOWN','HEALTHY','DEGRADED','UNAVAILABLE'
                        )),
    last_verified_at    INTEGER,
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE(adapter_instance_id, capability_name)
) STRICT;

CREATE INDEX idx_adapter_capabilities_adapter
    ON adapter_capabilities(adapter_instance_id);

CREATE TABLE models (
    id                  TEXT PRIMARY KEY,
    adapter_instance_id TEXT NOT NULL REFERENCES adapter_instances(id) ON DELETE CASCADE,
    remote_model_id     TEXT NOT NULL,
    canonical_key       TEXT NOT NULL UNIQUE,
    display_name        TEXT NOT NULL,
    availability        TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (availability IN (
                            'AVAILABLE','UNAVAILABLE','DEGRADED','UNKNOWN'
                        )),
    capabilities_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(capabilities_json)),
    parameter_schema_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(parameter_schema_json)),
    provider_raw_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(provider_raw_json)),
    discovered_at       INTEGER NOT NULL,
    last_seen_at        INTEGER,
    updated_at          INTEGER NOT NULL,
    UNIQUE(adapter_instance_id, remote_model_id)
) STRICT;

CREATE INDEX idx_models_adapter
    ON models(adapter_instance_id);

CREATE TABLE sessions (
    id                  TEXT PRIMARY KEY,
    adapter_instance_id TEXT NOT NULL REFERENCES adapter_instances(id) ON DELETE RESTRICT,
    project_id          TEXT REFERENCES projects(id) ON DELETE SET NULL,
    remote_session_id   TEXT,
    title               TEXT,
    managed             INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0,1)),
    sync_state          TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (sync_state IN (
                            'UNKNOWN','SYNCED','SYNCING','LOCAL_AHEAD',
                            'REMOTE_AHEAD','CONFLICT','DEGRADED','UNAVAILABLE'
                        )),
    sync_cursor         TEXT,
    remote_revision     TEXT,
    state_confidence    TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (state_confidence IN (
                            'HIGH','MEDIUM','LOW','UNKNOWN'
                        )),
    provider_status_raw TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(provider_status_raw)),
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    last_message_at     INTEGER,
    last_synced_at      INTEGER,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    archived_at         INTEGER
) STRICT;

CREATE UNIQUE INDEX idx_sessions_remote_unique
    ON sessions(adapter_instance_id, remote_session_id)
    WHERE remote_session_id IS NOT NULL;

CREATE INDEX idx_sessions_project
    ON sessions(project_id);

CREATE INDEX idx_sessions_managed
    ON sessions(managed, archived_at);

CREATE TABLE messages (
    id                  TEXT PRIMARY KEY,
    session_id          TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    remote_message_id   TEXT,
    role                TEXT NOT NULL CHECK (role IN (
                            'USER','ASSISTANT','SYSTEM','TOOL'
                        )),
    kind                TEXT NOT NULL DEFAULT 'MESSAGE' CHECK (kind IN (
                            'MESSAGE','TOOL_CALL','TOOL_RESULT','STATUS','SUMMARY'
                        )),
    sequence_no         INTEGER,
    text_content        TEXT,
    content_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(content_json)),
    provider_raw_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(provider_raw_json)),
    created_at          INTEGER NOT NULL,
    synced_at           INTEGER
) STRICT;

CREATE UNIQUE INDEX idx_messages_remote_unique
    ON messages(session_id, remote_message_id)
    WHERE remote_message_id IS NOT NULL;

CREATE INDEX idx_messages_session_created
    ON messages(session_id, created_at);

CREATE INDEX idx_messages_session_sequence
    ON messages(session_id, sequence_no)
    WHERE sequence_no IS NOT NULL;

CREATE TABLE tasks (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT REFERENCES projects(id) ON DELETE SET NULL,
    parent_task_id      TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    title               TEXT NOT NULL,
    description         TEXT,
    task_type           TEXT NOT NULL DEFAULT 'GENERAL',
    mode                TEXT NOT NULL DEFAULT 'EXECUTE',
    priority            TEXT NOT NULL DEFAULT 'NORMAL' CHECK (priority IN (
                            'LOW','NORMAL','HIGH','CRITICAL'
                        )),
    status              TEXT NOT NULL DEFAULT 'CREATED' CHECK (status IN (
                            'CREATED','QUEUED','PREPARING','RUNNING',
                            'WAITING_APPROVAL','WAITING_INPUT','BLOCKED',
                            'PAUSED','TRANSFERRING','AGENT_COMPLETED',
                            'VERIFYING','WAITING_ACCEPTANCE','ACCEPTED',
                            'FAILED','CANCELLED','INTERRUPTED','UNKNOWN'
                        )),
    risk_level          TEXT NOT NULL DEFAULT 'LOW' CHECK (risk_level IN (
                            'LOW','MEDIUM','HIGH','CRITICAL','UNKNOWN'
                        )),
    requested_by        TEXT REFERENCES principals(id) ON DELETE SET NULL,
    budget_json         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(budget_json)),
    policy_snapshot_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(policy_snapshot_json)),
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    accepted_at         INTEGER,
    archived_at         INTEGER
) STRICT;

CREATE INDEX idx_tasks_project_status
    ON tasks(project_id, status, priority);

CREATE INDEX idx_tasks_status_priority
    ON tasks(status, priority, created_at);

CREATE TABLE task_dependencies (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id  TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    dependency_type     TEXT NOT NULL CHECK (dependency_type IN (
                            'BLOCKS','WAITS_FOR','USES_RESULT','RELATED'
                        )),
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    CHECK (task_id <> depends_on_task_id),
    UNIQUE(task_id, depends_on_task_id, dependency_type)
) STRICT;

CREATE INDEX idx_task_dependencies_task
    ON task_dependencies(task_id);

CREATE INDEX idx_task_dependencies_upstream
    ON task_dependencies(depends_on_task_id);

CREATE TABLE task_session_links (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    session_id          TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    role                TEXT NOT NULL CHECK (role IN (
                            'ORIGIN','EXECUTION','HANDOFF','REVIEW',
                            'COLLAB','REFERENCE'
                        )),
    ordinal             INTEGER NOT NULL DEFAULT 0,
    created_at          INTEGER NOT NULL,
    UNIQUE(task_id, session_id, role)
) STRICT;

CREATE INDEX idx_task_session_links_task
    ON task_session_links(task_id, ordinal);

CREATE INDEX idx_task_session_links_session
    ON task_session_links(session_id);

CREATE TABLE executions (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
    adapter_instance_id TEXT NOT NULL REFERENCES adapter_instances(id) ON DELETE RESTRICT,
    session_id          TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    model_id            TEXT REFERENCES models(id) ON DELETE SET NULL,
    predecessor_execution_id TEXT REFERENCES executions(id) ON DELETE SET NULL,
    attempt_no          INTEGER NOT NULL DEFAULT 1 CHECK (attempt_no > 0),
    status              TEXT NOT NULL DEFAULT 'CREATED' CHECK (status IN (
                            'CREATED','STARTING','RUNNING','WAITING_APPROVAL',
                            'WAITING_INPUT','PAUSED','TRANSFERRING',
                            'SUCCEEDED','FAILED','CANCELLED','INTERRUPTED',
                            'UNKNOWN'
                        )),
    trigger_type        TEXT NOT NULL DEFAULT 'INITIAL' CHECK (trigger_type IN (
                            'INITIAL','RETRY','HANDOFF','RESUME',
                            'MANUAL_SWITCH','AUTO_SWITCH','REVIEW','RECOVERY'
                        )),
    state_confidence    TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (state_confidence IN (
                            'HIGH','MEDIUM','LOW','UNKNOWN'
                        )),
    model_snapshot_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(model_snapshot_json)),
    model_config_json   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(model_config_json)),
    permission_snapshot_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(permission_snapshot_json)),
    environment_snapshot_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(environment_snapshot_json)),
    provider_execution_id TEXT,
    started_at          INTEGER,
    heartbeat_at        INTEGER,
    finished_at         INTEGER,
    result_code         TEXT,
    error_code          TEXT,
    error_message       TEXT,
    error_details_json  TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(error_details_json)),
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_executions_task_created
    ON executions(task_id, created_at);

CREATE INDEX idx_executions_active
    ON executions(status, heartbeat_at);

CREATE INDEX idx_executions_adapter
    ON executions(adapter_instance_id, status);

CREATE TABLE execution_leases (
    execution_id        TEXT PRIMARY KEY REFERENCES executions(id) ON DELETE CASCADE,
    owner_id            TEXT NOT NULL,
    acquired_at         INTEGER NOT NULL,
    heartbeat_at        INTEGER NOT NULL,
    expires_at          INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_execution_leases_expiry
    ON execution_leases(expires_at);

CREATE TABLE execution_output_chunks (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id        TEXT NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
    sequence_no         INTEGER NOT NULL,
    channel             TEXT NOT NULL CHECK (channel IN (
                            'ASSISTANT','STDOUT','STDERR','TOOL','STATUS'
                        )),
    content             TEXT NOT NULL,
    created_at          INTEGER NOT NULL,
    UNIQUE(execution_id, sequence_no)
) STRICT;

CREATE INDEX idx_execution_output_chunks_exec
    ON execution_output_chunks(execution_id, sequence_no);

CREATE TABLE blobs (
    id                  TEXT PRIMARY KEY,
    storage_class       TEXT NOT NULL DEFAULT 'HOT' CHECK (storage_class IN (
                            'HOT','COLD','CACHE'
                        )),
    relative_path       TEXT NOT NULL UNIQUE,
    sha256              TEXT NOT NULL,
    size_bytes          INTEGER NOT NULL CHECK (size_bytes >= 0),
    mime_type           TEXT,
    created_at          INTEGER NOT NULL,
    archived_at         INTEGER
) STRICT;

CREATE INDEX idx_blobs_sha256
    ON blobs(sha256);

CREATE TABLE handoffs (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
    source_execution_id TEXT NOT NULL REFERENCES executions(id) ON DELETE RESTRICT,
    target_adapter_instance_id TEXT NOT NULL REFERENCES adapter_instances(id) ON DELETE RESTRICT,
    target_session_id   TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    target_execution_id TEXT REFERENCES executions(id) ON DELETE SET NULL,
    reason              TEXT,
    state               TEXT NOT NULL DEFAULT 'CREATED' CHECK (state IN (
                            'CREATED','PREPARING','READY','STARTING_TARGET',
                            'COMPLETED','FAILED','CANCELLED'
                        )),
    package_blob_id     TEXT REFERENCES blobs(id) ON DELETE SET NULL,
    package_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(package_json)),
    created_at          INTEGER NOT NULL,
    completed_at        INTEGER
) STRICT;

CREATE INDEX idx_handoffs_task
    ON handoffs(task_id, created_at);

CREATE INDEX idx_handoffs_source_execution
    ON handoffs(source_execution_id);

CREATE TABLE approvals (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    execution_id        TEXT REFERENCES executions(id) ON DELETE SET NULL,
    session_id          TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    adapter_instance_id TEXT REFERENCES adapter_instances(id) ON DELETE SET NULL,
    approval_type       TEXT NOT NULL,
    risk_level          TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (risk_level IN (
                            'LOW','MEDIUM','HIGH','CRITICAL','UNKNOWN'
                        )),
    action              TEXT NOT NULL,
    resource            TEXT,
    request_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(request_json)),
    state               TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN (
                            'PENDING','APPROVED','REJECTED','EXPIRED','CANCELLED'
                        )),
    requested_at        INTEGER NOT NULL,
    expires_at          INTEGER,
    resolved_at         INTEGER,
    resolved_by         TEXT REFERENCES principals(id) ON DELETE SET NULL,
    resolution_json     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(resolution_json))
) STRICT;

CREATE INDEX idx_approvals_pending
    ON approvals(state, risk_level, requested_at);

CREATE INDEX idx_approvals_task
    ON approvals(task_id, state);

CREATE TABLE temporary_grants (
    id                  TEXT PRIMARY KEY,
    principal_id        TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    scope_type          TEXT NOT NULL CHECK (scope_type IN (
                            'GLOBAL','TRUST_DOMAIN','PROJECT','TASK',
                            'SESSION','ADAPTER','MODEL'
                        )),
    scope_id            TEXT,
    action              TEXT NOT NULL,
    resource_pattern    TEXT,
    effect              TEXT NOT NULL DEFAULT 'ALLOW' CHECK (effect IN ('ALLOW','DENY')),
    constraints_json    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(constraints_json)),
    starts_at           INTEGER NOT NULL,
    expires_at          INTEGER,
    created_at          INTEGER NOT NULL,
    revoked_at          INTEGER
) STRICT;

CREATE INDEX idx_temporary_grants_lookup
    ON temporary_grants(principal_id, scope_type, scope_id, action, expires_at);

CREATE TABLE policy_rules (
    id                  TEXT PRIMARY KEY,
    scope_type          TEXT NOT NULL CHECK (scope_type IN (
                            'GLOBAL','TRUST_DOMAIN','PROJECT','TASK_MODE',
                            'TASK','SESSION','ADAPTER','MODEL'
                        )),
    scope_id            TEXT,
    action_pattern      TEXT NOT NULL,
    resource_pattern    TEXT,
    effect              TEXT NOT NULL CHECK (effect IN (
                            'ALLOW','DENY','REQUIRE_APPROVAL'
                        )),
    priority            INTEGER NOT NULL DEFAULT 0,
    condition_json      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(condition_json)),
    enabled             INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_policy_rules_scope
    ON policy_rules(scope_type, scope_id, enabled, priority DESC);

CREATE TABLE quota_accounts (
    id                  TEXT PRIMARY KEY,
    adapter_instance_id TEXT NOT NULL REFERENCES adapter_instances(id) ON DELETE CASCADE,
    external_account_ref TEXT,
    display_label       TEXT,
    quota_source        TEXT,
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX idx_quota_accounts_external
    ON quota_accounts(adapter_instance_id, external_account_ref)
    WHERE external_account_ref IS NOT NULL;

CREATE TABLE quota_snapshots (
    id                  TEXT PRIMARY KEY,
    quota_account_id    TEXT NOT NULL REFERENCES quota_accounts(id) ON DELETE CASCADE,
    quota_type          TEXT NOT NULL CHECK (quota_type IN (
                            'PERCENT','CREDITS','TOKENS','REQUESTS',
                            'CURRENCY','TIME_WINDOW','CUSTOM'
                        )),
    quota_key           TEXT NOT NULL,
    raw_value_json      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(raw_value_json)),
    used_value          REAL,
    remaining_value     REAL,
    percentage_used     REAL CHECK (percentage_used IS NULL OR (percentage_used >= 0 AND percentage_used <= 100)),
    percentage_remaining REAL CHECK (percentage_remaining IS NULL OR (percentage_remaining >= 0 AND percentage_remaining <= 100)),
    window_start        INTEGER,
    window_end          INTEGER,
    reset_at            INTEGER,
    observation_kind    TEXT NOT NULL DEFAULT 'OBSERVED' CHECK (observation_kind IN (
                            'OBSERVED','ESTIMATED'
                        )),
    source              TEXT,
    confidence          TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (confidence IN (
                            'HIGH','MEDIUM','LOW','UNKNOWN'
                        )),
    observed_at         INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_quota_snapshots_latest
    ON quota_snapshots(quota_account_id, quota_key, observed_at DESC);

CREATE TABLE attachments (
    id                  TEXT PRIMARY KEY,
    blob_id             TEXT NOT NULL REFERENCES blobs(id) ON DELETE RESTRICT,
    filename            TEXT NOT NULL,
    attachment_type     TEXT NOT NULL DEFAULT 'FILE',
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE message_attachments (
    message_id          TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    attachment_id       TEXT NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    ordinal             INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(message_id, attachment_id)
) STRICT;

CREATE TABLE task_attachments (
    task_id             TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    attachment_id       TEXT NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    role                TEXT NOT NULL DEFAULT 'INPUT' CHECK (role IN (
                            'INPUT','OUTPUT','HANDOFF','DIAGNOSTIC'
                        )),
    ordinal             INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(task_id, attachment_id, role)
) STRICT;

CREATE TABLE project_attachments (
    project_id          TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    attachment_id       TEXT NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    category            TEXT,
    created_at          INTEGER NOT NULL,
    PRIMARY KEY(project_id, attachment_id)
) STRICT;

CREATE TABLE process_instances (
    id                  TEXT PRIMARY KEY,
    process_type        TEXT NOT NULL CHECK (process_type IN (
                            'ADAPTER','TERMINAL','WORKER','PROJECT_SERVICE',
                            'BROWSER','SANDBOX'
                        )),
    owner_type          TEXT,
    owner_id            TEXT,
    pid                 INTEGER,
    executable          TEXT,
    status              TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK (status IN (
                            'STARTING','RUNNING','STOPPING','EXITED',
                            'FAILED','UNKNOWN'
                        )),
    restart_policy      TEXT NOT NULL DEFAULT 'ON_FAILURE' CHECK (restart_policy IN (
                            'NEVER','ON_FAILURE','ALWAYS'
                        )),
    started_at          INTEGER,
    heartbeat_at        INTEGER,
    exited_at           INTEGER,
    exit_code           INTEGER,
    metadata_json       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metadata_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_process_instances_active
    ON process_instances(process_type, status);

CREATE TABLE notifications (
    id                  TEXT PRIMARY KEY,
    notification_type   TEXT NOT NULL,
    priority            TEXT NOT NULL DEFAULT 'MEDIUM' CHECK (priority IN (
                            'LOW','MEDIUM','HIGH','CRITICAL'
                        )),
    state               TEXT NOT NULL DEFAULT 'UNREAD' CHECK (state IN (
                            'UNREAD','READ','PENDING','HANDLED',
                            'IGNORED','EXPIRED'
                        )),
    title               TEXT NOT NULL,
    body                TEXT,
    project_id          TEXT REFERENCES projects(id) ON DELETE SET NULL,
    task_id             TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    approval_id         TEXT REFERENCES approvals(id) ON DELETE SET NULL,
    actions_json        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(actions_json)),
    created_at          INTEGER NOT NULL,
    read_at             INTEGER,
    handled_at          INTEGER,
    expires_at          INTEGER
) STRICT;

CREATE INDEX idx_notifications_inbox
    ON notifications(state, priority, created_at DESC);

CREATE TABLE event_journal (
    seq                 INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id            TEXT NOT NULL UNIQUE,
    event_type          TEXT NOT NULL,
    aggregate_type      TEXT NOT NULL,
    aggregate_id        TEXT NOT NULL,
    aggregate_version   INTEGER NOT NULL CHECK (aggregate_version > 0),
    correlation_id      TEXT,
    causation_id        TEXT,
    principal_id        TEXT REFERENCES principals(id) ON DELETE SET NULL,
    payload_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload_json)),
    created_at          INTEGER NOT NULL,
    UNIQUE(aggregate_type, aggregate_id, aggregate_version)
) STRICT;

CREATE INDEX idx_event_journal_aggregate
    ON event_journal(aggregate_type, aggregate_id, seq);

CREATE INDEX idx_event_journal_correlation
    ON event_journal(correlation_id, seq)
    WHERE correlation_id IS NOT NULL;

CREATE INDEX idx_event_journal_type
    ON event_journal(event_type, seq);

CREATE TABLE audit_entries (
    seq                 INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_id            TEXT NOT NULL UNIQUE,
    principal_id        TEXT REFERENCES principals(id) ON DELETE SET NULL,
    action              TEXT NOT NULL,
    resource_type       TEXT,
    resource_id         TEXT,
    project_id          TEXT REFERENCES projects(id) ON DELETE SET NULL,
    task_id             TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    execution_id        TEXT REFERENCES executions(id) ON DELETE SET NULL,
    correlation_id      TEXT,
    outcome             TEXT NOT NULL CHECK (outcome IN (
                            'SUCCESS','FAILURE','DENIED','UNKNOWN'
                        )),
    details_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details_json)),
    previous_hash       TEXT,
    entry_hash          TEXT NOT NULL,
    signature           TEXT,
    created_at          INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_audit_entries_task
    ON audit_entries(task_id, seq);

CREATE INDEX idx_audit_entries_project
    ON audit_entries(project_id, seq);

CREATE INDEX idx_audit_entries_correlation
    ON audit_entries(correlation_id, seq)
    WHERE correlation_id IS NOT NULL;

CREATE TABLE idempotency_keys (
    key                 TEXT PRIMARY KEY,
    principal_id        TEXT REFERENCES principals(id) ON DELETE SET NULL,
    operation           TEXT NOT NULL,
    request_hash        TEXT NOT NULL,
    state               TEXT NOT NULL CHECK (state IN (
                            'IN_PROGRESS','COMPLETED','FAILED'
                        )),
    response_json       TEXT CHECK (response_json IS NULL OR json_valid(response_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    expires_at          INTEGER
) STRICT;

CREATE INDEX idx_idempotency_keys_expiry
    ON idempotency_keys(expires_at)
    WHERE expires_at IS NOT NULL;

CREATE TABLE settings (
    id                  TEXT PRIMARY KEY,
    scope_type          TEXT NOT NULL CHECK (scope_type IN (
                            'GLOBAL','TRUST_DOMAIN','PROJECT','ADAPTER',
                            'TASK_MODE','SESSION'
                        )),
    scope_id            TEXT,
    setting_key         TEXT NOT NULL,
    value_json          TEXT NOT NULL CHECK (json_valid(value_json)),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

-- SQLite treats NULL values as distinct inside UNIQUE constraints.
-- Use an expression index so GLOBAL settings (scope_id IS NULL)
-- still have exactly one value per key.
CREATE UNIQUE INDEX idx_settings_scope_key
    ON settings(scope_type, COALESCE(scope_id, ''), setting_key);

-- FTS5 indexes for the first MVP search surface.
CREATE VIRTUAL TABLE messages_fts USING fts5(
    text_content,
    content='messages',
    content_rowid='rowid'
);

CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, text_content)
    VALUES (new.rowid, COALESCE(new.text_content, ''));
END;

CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, text_content)
    VALUES ('delete', old.rowid, COALESCE(old.text_content, ''));
END;

CREATE TRIGGER messages_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, text_content)
    VALUES ('delete', old.rowid, COALESCE(old.text_content, ''));
    INSERT INTO messages_fts(rowid, text_content)
    VALUES (new.rowid, COALESCE(new.text_content, ''));
END;

CREATE VIRTUAL TABLE tasks_fts USING fts5(
    title,
    description,
    content='tasks',
    content_rowid='rowid'
);

CREATE TRIGGER tasks_ai AFTER INSERT ON tasks BEGIN
    INSERT INTO tasks_fts(rowid, title, description)
    VALUES (new.rowid, new.title, COALESCE(new.description, ''));
END;

CREATE TRIGGER tasks_ad AFTER DELETE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, description)
    VALUES ('delete', old.rowid, old.title, COALESCE(old.description, ''));
END;

CREATE TRIGGER tasks_au AFTER UPDATE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, description)
    VALUES ('delete', old.rowid, old.title, COALESCE(old.description, ''));
    INSERT INTO tasks_fts(rowid, title, description)
    VALUES (new.rowid, new.title, COALESCE(new.description, ''));
END;

INSERT INTO schema_migrations(version, name, applied_at)
VALUES (1, 'core_schema_v0_1', CAST(strftime('%s','now') AS INTEGER) * 1000);

COMMIT;
