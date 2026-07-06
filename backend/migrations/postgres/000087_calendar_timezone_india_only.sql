ALTER TABLE calendar_event_projections
  ALTER COLUMN timezone SET DEFAULT 'Asia/Kolkata',
  ALTER COLUMN timezone_source SET DEFAULT 'india_only';

UPDATE calendar_event_projections
SET timezone = 'Asia/Kolkata',
    timezone_source = 'india_only'
WHERE timezone <> 'Asia/Kolkata'
   OR timezone_source <> 'india_only';
