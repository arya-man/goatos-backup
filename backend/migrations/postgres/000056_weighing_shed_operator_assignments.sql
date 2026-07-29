-- +goose Up
ALTER TABLE public.weighing_campaign_sheds
  ADD COLUMN IF NOT EXISTS operator_user_id uuid;

UPDATE public.weighing_campaign_sheds shed
SET operator_user_id = campaign.operator_user_id
FROM public.weighing_campaigns campaign
WHERE shed.tenant_id = campaign.tenant_id
  AND shed.campaign_id = campaign.campaign_id
  AND shed.operator_user_id IS NULL;

ALTER TABLE public.weighing_campaign_sheds
  ALTER COLUMN operator_user_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS weighing_campaign_sheds_operator_status_idx
  ON public.weighing_campaign_sheds (tenant_id, operator_user_id, status, updated_at DESC, campaign_shed_id);

-- +goose Down
DROP INDEX IF EXISTS public.weighing_campaign_sheds_operator_status_idx;

ALTER TABLE public.weighing_campaign_sheds
  DROP COLUMN IF EXISTS operator_user_id;
