-- +goose Up
-- Validate separately from 000166. Adding the CHECK as NOT VALID is short-lock DDL; validating it in
-- the same goose transaction would keep that DDL lock open across the existing-row scan on goats.
ALTER TABLE public.goats
  VALIDATE CONSTRAINT goats_milk_cohort_check;

-- +goose Down
-- The constraint remains installed by 000166; validation has no separate reversible state.
