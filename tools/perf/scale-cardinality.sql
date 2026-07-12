SELECT json_build_object(
  'generated_at', now(),
  'process_integrity_projection_rows', (
    SELECT count(*) FROM process_integrity_projection_rows
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001 AND row_id LIKE 'scale:%'
  ),
  'process_integrity_unique_due_at', (
    SELECT count(DISTINCT due_at) FROM process_integrity_projection_rows
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001 AND row_id LIKE 'scale:%'
  ),
  'process_integrity_projection_summaries', (
    SELECT count(*) FROM process_integrity_projection_summaries
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001 AND owner_id = ''
  ),
  'process_integrity_summary_covered_rows', (
    SELECT COALESCE(sum(row_count), 0) FROM process_integrity_projection_summaries
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001 AND owner_id = ''
  ),
  'process_integrity_summary_business_dates', (
    SELECT count(DISTINCT due_business_date) FROM process_integrity_projection_summaries
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001 AND owner_id = ''
  ),
  'calendar_event_projections', (
    SELECT count(*) FROM calendar_event_projections
    WHERE tenant_id = :'tenant_id'::uuid AND event_id LIKE 'scale:%'
  ),
  'vaccination_shed_projection_rows', (
    SELECT count(*) FROM vaccination_shed_projection_rows
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001
  ),
  'vaccination_shed_projection_animals', (
    SELECT COALESCE(sum(animals), 0) FROM vaccination_shed_projection_rows
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001
  ),
  'vaccination_execution_projection_rows', (
    SELECT count(*) FROM vaccination_execution_projection_rows
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001
  ),
  'vaccination_operations_projection_rows', (
    SELECT count(*) FROM vaccination_operations_projection_rows
    WHERE tenant_id = :'tenant_id'::uuid AND projection_version = 9001
  ),
  'projection_metadata', json_build_object(
    'process_integrity', (
      SELECT json_build_object(
        'projection_version', projection_version,
        'serving_projection_version', serving_projection_version,
        'declared_row_count', row_count,
        'projection_age_seconds', extract(epoch FROM now() - projected_at),
        'as_of_lag_seconds', extract(epoch FROM now() - as_of),
        'freshness_status', freshness_status,
        'serving_state', serving_state
      )
      FROM process_integrity_projection_state
      WHERE tenant_id = :'tenant_id'::uuid
    ),
    'calendar', (
      SELECT json_build_object(
        'projection_version', projection_version,
        'serving_projection_version', projection_version,
        'declared_row_count', (
          SELECT count(*) FROM calendar_event_projections events
          WHERE events.tenant_id = state.tenant_id AND events.event_id LIKE 'scale:%'
        ),
        'projection_age_seconds', extract(epoch FROM now() - projected_at),
        'as_of_lag_seconds', extract(epoch FROM now() - projected_at),
        'freshness_status', freshness_status,
        'serving_state', serving_state
      )
      FROM calendar_projection_state state
      WHERE tenant_id = :'tenant_id'::uuid AND slice_key = 'vaccination'
    ),
    'vaccination_shed', (
      SELECT json_build_object(
        'projection_version', projection_version,
        'serving_projection_version', serving_projection_version,
        'declared_row_count', row_count,
        'projection_age_seconds', extract(epoch FROM now() - projected_at),
        'as_of_lag_seconds', extract(epoch FROM now() - as_of),
        'freshness_status', freshness_status,
        'serving_state', serving_state
      )
      FROM vaccination_shed_projection_state
      WHERE tenant_id = :'tenant_id'::uuid
    ),
    'vaccination_execution', (
      SELECT json_build_object(
        'projection_version', projection_version,
        'serving_projection_version', serving_projection_version,
        'declared_row_count', row_count,
        'projection_age_seconds', extract(epoch FROM now() - projected_at),
        'as_of_lag_seconds', extract(epoch FROM now() - as_of),
        'freshness_status', freshness_status,
        'serving_state', serving_state
      )
      FROM vaccination_execution_projection_state
      WHERE tenant_id = :'tenant_id'::uuid
    ),
    'vaccination_operations', (
      SELECT json_build_object(
        'projection_version', projection_version,
        'serving_projection_version', serving_projection_version,
        'declared_row_count', row_count,
        'projection_age_seconds', extract(epoch FROM now() - projected_at),
        'as_of_lag_seconds', extract(epoch FROM now() - as_of),
        'freshness_status', freshness_status,
        'serving_state', serving_state
      )
      FROM vaccination_operations_projection_state
      WHERE tenant_id = :'tenant_id'::uuid
    )
  )
);
