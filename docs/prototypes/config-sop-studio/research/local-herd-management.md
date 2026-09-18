# Actual local animal and pen management inspection

2026-09-16, actual application source e06d27bf6 in isolated checkout; local web3307/API8087, existing OCI tunnel15432. Sidebar Herd Register was commented out and restored only locally. No database migration, grant seed or CRUD submission was performed.

Chrome rendered `/counts/herd`, 1,681 total records and visible Import pens / Register pen / Import sheet / Register animal controls. Opened and inspected both register dialogs.

Register pen: existing park selection, pen code, required name, display order and notes. No capacity field and no Add park control. The dialog explicitly says this path creates an active pen usable for counts, vaccination and SOP, not feed/holding/quarantine/ICU. This is partial coverage of voice V12, not a general location setup interface.

Register animal: Tag1 required, Tag2 optional, species, park, pen, farm optional, breed, management stage, sex, origin, DOB, weight, entry date, estimated DOB, dam and sire/lot and provenance reference. Tag values are not reusable; display ID is not RFID. No values submitted.

Readiness endpoint503: OCI schema319 versus binary migration326. The requested Herd Register and forms render, but this is not certification of every route or write path. The real goatos-stg audit remains separately targeted to GCP Cloud SQL and was not replaced by OCI.
