-- Lucero
-- Migration: 000002_aggregate_version.sql
-- ADR-017: 给聚合根表加 version 列，使 event_journal.aggregate_version 有可信来源
--
-- 记账约定（与 000001 一致，见 specs/contracts-internal-v0.1.md §1.5）：
--   每个 .sql 自己 INSERT schema_migrations；迁移器只查表、只按版本号决定是否执行，
--   不得再包一层事务，不得重复记账。
--
-- 不加 CHECK (version > 0)：ALTER TABLE ADD COLUMN 对约束的支持面比 CREATE TABLE 窄，
-- 且在 STRICT 表上徒增失败面；version 单调递增由「唯一的取号语句 version = version + 1」
-- 与 event_journal 的 UNIQUE(aggregate_type, aggregate_id, aggregate_version) 共同保证。
--
-- Runtime 仍需在每条新建连接上执行：
--   PRAGMA foreign_keys = ON;
--   PRAGMA busy_timeout = 5000;

PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

BEGIN IMMEDIATE;

ALTER TABLE projects          ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE adapter_instances ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE models            ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE sessions          ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE tasks             ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE executions        ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE approvals         ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE quota_accounts    ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE notifications     ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE handoffs          ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

INSERT INTO schema_migrations(version, name, applied_at)
VALUES (2, 'aggregate_version', CAST(strftime('%s','now') AS INTEGER) * 1000);

COMMIT;
