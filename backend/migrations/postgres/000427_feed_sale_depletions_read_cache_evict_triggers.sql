-- +goose Up
-- SOLD FEED EVICTS THE ALERTS LOW-STOCK READ CACHE ON EVERY API INSTANCE.
--
-- 000416 made the feed-stock inputs of feedLowStockSQL announce their own writes so the per-tenant
-- alerts low-stock cache stays exact. 000422 added a new input: feedLowStockSQL subtracts sold feed
-- (feed_sale_depletions, written by the sales deal repository when a feed line is recorded,
-- edited or removed). Without a trigger here a feed sale would leave the alerts stock rule
-- serving the pre-sale balance until the entry aged out. Same functions and channel as 000416
-- (caches=["alerts"]); statement-level with transition tables.
--
-- LOCK SAFETY: CREATE TRIGGER takes SHARE ROW EXCLUSIVE briefly on one small table.
SET lock_timeout = '5s';

-- +goose StatementBegin
DO $do$
BEGIN
  DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_ins ON public.feed_sale_depletions;
  CREATE TRIGGER feed_stock_read_cache_evict_ins AFTER INSERT ON public.feed_sale_depletions
    REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_stock_read_cache_notify_new_rows();
  DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_upd ON public.feed_sale_depletions;
  CREATE TRIGGER feed_stock_read_cache_evict_upd AFTER UPDATE ON public.feed_sale_depletions
    REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_stock_read_cache_notify_new_rows();
  DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_del ON public.feed_sale_depletions;
  CREATE TRIGGER feed_stock_read_cache_evict_del AFTER DELETE ON public.feed_sale_depletions
    REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_stock_read_cache_notify_old_rows();
END
$do$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_ins ON public.feed_sale_depletions;
DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_upd ON public.feed_sale_depletions;
DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_del ON public.feed_sale_depletions;
