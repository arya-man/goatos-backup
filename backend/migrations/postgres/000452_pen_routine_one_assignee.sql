-- +goose Up
-- seed-fixture-guard:ignore: one nullable column on the routine definition; no seed, source
-- fixture or read-model table changes (routines are authored on /routines, never seeded).
--
-- A ROUTINE IS FOR ONE PERSON (maintainer decision 2026-09-26, docs/decisions/pen-routines.md,
-- revising the 2026-09-17 assign-by-role rule): "pick people, like Tasks ... it is just like tasks,
-- it will go to one only". The routine names the person it is for; `assignee_roles` stays and is
-- written by the server as the roles through which that person may do it at the routine's park
-- (so a park head can only be given their own park's routines, and a person who loses the role
-- stops being owed the routine rather than the work falling to someone else).
--
-- NULL = a routine written before this revision: it keeps the role semantics it was created with
-- (goatos-stg held no routines on 2026-09-26, so nothing live changes meaning).
ALTER TABLE pen_routine_definitions ADD COLUMN assignee_user_id uuid;

-- +goose Down
ALTER TABLE pen_routine_definitions DROP COLUMN IF EXISTS assignee_user_id;
