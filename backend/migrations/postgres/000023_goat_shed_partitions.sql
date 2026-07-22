CREATE TABLE IF NOT EXISTS goat_shed_partitions (
  tenant_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  partition_label text NOT NULL DEFAULT 'whole',
  source_shed_name text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, goat_id),
  CONSTRAINT goat_shed_partitions_goat_fk
    FOREIGN KEY (tenant_id, goat_id)
    REFERENCES goats (tenant_id, goat_id)
    ON DELETE CASCADE,
  CONSTRAINT goat_shed_partitions_shed_fk
    FOREIGN KEY (tenant_id, shed_id)
    REFERENCES locations (tenant_id, location_id)
    ON DELETE RESTRICT,
  CONSTRAINT goat_shed_partitions_partition_nonblank
    CHECK (btrim(partition_label) <> ''),
  CONSTRAINT goat_shed_partitions_source_nonblank
    CHECK (btrim(source_shed_name) <> '')
);

CREATE INDEX IF NOT EXISTS goat_shed_partitions_shed_partition_idx
  ON goat_shed_partitions (tenant_id, shed_id, partition_label);
