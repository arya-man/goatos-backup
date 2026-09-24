-- +goose Up
-- Old mobile outboxes carry names, not codes. Reserve every name against the
-- original product so renames cannot reject or redirect those pending sales.
CREATE TABLE public.sellable_product_names (
    tenant_id uuid NOT NULL,
    name_key text NOT NULL,
    product_code text NOT NULL,
    PRIMARY KEY (tenant_id, name_key),
    FOREIGN KEY (tenant_id, product_code)
        REFERENCES public.sellable_product_catalog (tenant_id, product_code) ON DELETE CASCADE
);
CREATE INDEX sellable_product_names_product_idx
    ON public.sellable_product_names (tenant_id, product_code);
INSERT INTO public.sellable_product_names (tenant_id, name_key, product_code)
SELECT tenant_id, lower(btrim(name)), product_code FROM public.sellable_product_catalog;

-- +goose StatementBegin
CREATE FUNCTION public.reserve_sellable_product_name() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO public.sellable_product_names (tenant_id, name_key, product_code)
    VALUES (NEW.tenant_id, lower(btrim(NEW.name)), NEW.product_code)
    ON CONFLICT (tenant_id, name_key) DO UPDATE
        SET product_code = EXCLUDED.product_code
        WHERE sellable_product_names.product_code = EXCLUDED.product_code;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'product name is reserved by another product'
            USING ERRCODE = '23505', CONSTRAINT = 'sellable_product_names_pkey';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER sellable_product_name_history
AFTER INSERT OR UPDATE OF name ON public.sellable_product_catalog
FOR EACH ROW EXECUTE FUNCTION public.reserve_sellable_product_name();

-- +goose Down
DROP TRIGGER sellable_product_name_history ON public.sellable_product_catalog;
DROP FUNCTION public.reserve_sellable_product_name();
DROP TABLE public.sellable_product_names;
