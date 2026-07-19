-- +goose Up
-- P0 (rebase recovery): create public.department_module_grants.
--
-- The counts/nav PR originally added this table to the 000001 baseline. When the branch was rebased
-- onto a main whose baseline had been independently rewritten, the auto-merge of 000001 dropped the
-- table -- so bootstrap_copy.go composes nav LIVE from department_module_grants
-- (ListGrantedModuleKeys), but no migration created it, and every non-leadership operator's
-- /app/bootstrap would fail or compose a blank bottom bar. This restores it as a FORWARD migration
-- (departments and workforce_members.department_id already exist in the current baseline), so fresh
-- AND already-migrated environments converge. Additive only.
--
-- department_module_grants: the module side of "a person's job = role x granted modules".
-- Canonical rule: docs/decisions/role-module-nav-composition.md. module_key is deliberately NOT an
-- enum: the moduleNavRegistry in bootstrap_copy.go is the semantic source of truth for which module
-- keys mean anything; an unrecognised key simply contributes no nav. Adding a module is a registry
-- entry plus a grant row, never a schema migration. The CHECK only enforces the shared code shape.

CREATE TABLE IF NOT EXISTS public.department_module_grants (
    department_module_grant_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    department_id uuid NOT NULL,
    module_key text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT department_module_grants_module_key_check CHECK ((module_key ~ '^[a-z][a-z0-9_]*$'::text)),
    CONSTRAINT department_module_grants_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))
);

-- Guarded so a re-run (or a base that already had the table) is a no-op.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_module_grants_pkey') THEN
        ALTER TABLE ONLY public.department_module_grants
            ADD CONSTRAINT department_module_grants_pkey PRIMARY KEY (department_module_grant_id);
    END IF;
    -- One row per (tenant, department, module); re-granting is an idempotent upsert.
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_module_grants_tenant_department_module_key') THEN
        ALTER TABLE ONLY public.department_module_grants
            ADD CONSTRAINT department_module_grants_tenant_department_module_key
            UNIQUE (tenant_id, department_id, module_key);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_module_grants_department_id_fkey') THEN
        ALTER TABLE ONLY public.department_module_grants
            ADD CONSTRAINT department_module_grants_department_id_fkey
            FOREIGN KEY (tenant_id, department_id)
            REFERENCES public.departments(tenant_id, department_id) ON DELETE CASCADE;
    END IF;
END $$;
-- +goose StatementEnd

-- Serves ListGrantedModuleKeys (the two-key /app/bootstrap lookup).
CREATE INDEX IF NOT EXISTS department_module_grants_tenant_department_active_idx
    ON public.department_module_grants USING btree (tenant_id, department_id)
    INCLUDE (module_key) WHERE (status = 'active'::text);

-- +goose Down
DROP TABLE IF EXISTS public.department_module_grants;
