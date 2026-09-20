-- Lucero
-- Migration: 000003_grant_policy_version.sql
-- R-002 重开（T-12）：三张可变表此前不在 ADR-017 的 10 张聚合表里，没有 version 列，
-- 于是 Touch 用不了、Append 只能插 —— UPDATE 在平台层没有任何合法表达。
-- 同 000002：不加 CHECK (version > 0)，单调性由取号语句与 event_journal 唯一约束共同保证。
--
-- 记账约定（specs/contracts-internal-v0.1.md §1.5）：本文件自己 INSERT schema_migrations，
-- 迁移器只查表决定执行哪个文件、不再包一层事务。
--
-- Runtime 仍需在每条新建连接上执行：
--   PRAGMA foreign_keys = ON;
--   PRAGMA busy_timeout = 5000;

PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

BEGIN IMMEDIATE;

ALTER TABLE temporary_grants    ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE policy_rules        ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE adapter_capabilities ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

INSERT INTO schema_migrations(version, name, applied_at)
VALUES (3, 'grant_policy_version', CAST(strftime('%s','now') AS INTEGER) * 1000);

COMMIT;
