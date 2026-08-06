# OPERATIONAL LOCATION AUDIT - COMPREHENSIVE FINDINGS

## Executive Summary
Audit of Goat OS codebase for OperationalLocation handling across all surfaces. Scope: feed direction, health, herd/passport/search, inventory, actions/alerts, timetable, procurement, breeding, shared read models.

**Critical Finding**: Goat Passport API completely missing location information (should carry park_id, park_name, shed_id, shed_name, partition_label, operational_location_display per AGENTS.md).

---

## FINDINGS TABLE (Ordered Worst-First)

| Surface | File:Line | Current Render | Partition Present? | Severity | Owned By | Status |
|---------|-----------|-----------------|-------------------|----------|----------|--------|
| **Goat Passport API** | backend/internal/passport/app/service.go:79-86 | Returns only GoatID; no location in Passport struct at all | ✗ MISSING | CRITICAL | passport (can edit) | FIXABLE - Requires adding LocationReader or similar |
| Passport DueItem | backend/internal/passport/app/service.go:40-54 | Obligation details only; no partition or location | ✗ MISSING | CRITICAL | passport (can edit) | FIXABLE - Requires struct fields + location resolution |
| Passport HistoryItem | backend/internal/passport/app/service.go:57-70 | Administered dose details; no partition or location | ✗ MISSING | CRITICAL | passport (can edit) | FIXABLE - Requires struct fields + location resolution |
| Health Filter Options | backend/internal/health/adapters/postgres/repository.go:348 | `GROUP BY hc.shed_id, l.name` returning only `l.name` for shed label | ✗ MISSING | HIGH | health (can edit) | BLOCKED - health_cases table lacks partition_label column (migration needed) |
| Health WorkItem | backend/internal/health/adapters/postgres/repository.go:378 | Returns `hc.park_id::text, pl.name, hc.shed_id::text, sl.name` - name only | ✗ MISSING | HIGH | health (can edit) | BLOCKED - health_cases table lacks partition_label column (migration needed) |
| Feed Transport Filter | backend/internal/feeddirection/adapters/postgres/transport.go:107-113 | `GROUP BY t.shed_id, s.name` - groups by name (brittle) | ✗ NONE | MEDIUM | feeddirection (can edit) | BLOCKED - feed_transport_tasks table lacks partition_label column |
| Feed Transport Tasks | backend/internal/feeddirection/adapters/postgres/transport.go:54-68 | Selects shed.name only - partition-unaware | ✗ NONE | MEDIUM | feeddirection (can edit) | BLOCKED - feed_transport_tasks table lacks partition_label column |
| Vaccination Execution Assignments | backend/internal/vaccinationexecution/adapters/postgres/repository.go:varies | Multiple queries `GROUP BY shed.name` in keyset pagination | ✗ PARTIAL | MEDIUM | vaccinationexecution (can edit) | NEEDS REVIEW - uses name in ORDER BY for pagination keyset |
| Calendar Event Summary | backend/internal/calendar/adapters/postgres/canonical_read.go:varies | `COALESCE(NULLIF(l.name, ''), l.location_code, m.shed_id::text)` for display | ✗ PARTIAL | MEDIUM | calendar (DO NOT EDIT - touches obligations) | REPORTED ONLY - owned by obligation module |
| Obligation Read Query | backend/internal/obligation/adapters/postgres/sqlc/query.sql:127-131 | Hand-rolled CASE with correct comment referencing oploc.Display() | ✓ YES | LOW | obligation (DO NOT EDIT) | VERIFIED CORRECT - matches oploc.Display() logic |
| CEO/AI Reporting | backend/internal/ceoai/ (various) | Per migration 000114, partition_label is carried | ✓ YES | PASS | ceoai (can edit) | VERIFIED - comments indicate partition_label in views |
| Verification Module | backend/internal/verification/adapters/postgres/repository.go:varies | GROUP BY includes partition_label; shed_loc.name also grouped | ✓ PARTIAL | PASS | verification (DO NOT EDIT) | VERIFIED - includes partition in GROUP BY |

---

## DETAILED FINDINGS

### CRITICAL (Blocks Correct Display)

#### 1. Goat Passport - Missing All Location Information
- **Surface**: GET /goats/{goat_id}/passport API response
- **Root Cause**: Passport struct only carries GoatID; no location context at all
- **Impact**: Goat detail screen cannot show where animals should be vaccinated
- **Current Behavior**: Returns obligations without location context
- **Required Fix**: Add to Passport and DueItem/HistoryItem:
  - park_id, park_name
  - shed_id, shed_name  
  - partition_label (raw label, may be nil/empty)
  - operational_location_display (rendered as per oploc.Display())
- **Blocker**: Passport service only has access to VaccinationReader + ObligationReader interfaces; neither carries location. Would need LocationReader or to fetch location separately from goats table.
- **FIXABLE**: Yes - can add location resolution in passport service

#### 2. Health Module - No Partition Support at Table Level
- **Surface**: Filter options + work item detail for health treatment sessions
- **Root Cause**: health_cases table has no partition_label column; never added during migrations 000111-000114
- **Current Behavior**: Returns shed name only; cannot distinguish Castro 1 vs Castro 2
- **Files Affected**:
  - loadFilterOptions (line 348): `GROUP BY hc.shed_id, l.name` returning only name
  - GetWorkItem (line 378): Returns `sl.name` only for shed label
- **Required Fix**: 
  - Add migration adding partition_label to health_cases
  - Update filter options query to join locations and build operational_location_display
  - Update GetWorkItem query similarly
- **BLOCKED**: Needs migration (outside audit scope for now)

### HIGH (Display May Not Distinguish Partitions)

#### 3. Feed Transport Tasks - Partition-Unaware Table
- **Surface**: Feed transport task list and filter options
- **Root Cause**: feed_transport_tasks table never added partition_label column; partition support only added to weighing_campaign_sheds in migration 000121
- **Current Pattern**: `GROUP BY t.shed_id, s.name` (line 113)
- **Issue**: Brittle grouping - technically shed_id is unique, but including name makes keyset pagination unclear
- **Files**: 
  - listTransportFilterOptions (line 113): `GROUP BY t.shed_id, s.name`
  - ListTransportTasks (line 68): Selects shed.name only
- **Required Fix**: Either remove name from GROUP BY, or add partition_label to table via migration
- **BLOCKED**: If partitions should be supported, requires migration

#### 4. Health Filter Options - Shed-Only Dropdown Display
- **Pattern**: Returns shed_id + shed.name only, no partition distinction
- **Issue**: Dropdown labeled "shed" could show "Castro" twice if animals exist in Castro 1 and Castro 2
- **Expectation per AGENTS.md**: Should show "Castro 1", "Castro 2" if partitions exist
- **Status**: BLOCKED - needs table-level partition support

### MEDIUM (Brittle Patterns / Name-Based Grouping)

#### 5. Vaccination Execution Queries - Name in Pagination Keyset
- **File**: backend/internal/vaccinationexecution/adapters/postgres/repository.go
- **Pattern**: Multiple queries use `ORDER BY park.name, shed.name, stage` for keyset pagination
- **Issue**: Names repeat across parks; pagination keyset comparison could skip/duplicate rows when parks have sheds with same names
- **Affected Queries**: Lines with `GROUP BY windowed.park_uuid, park.name, windowed.shed_uuid, shed.name` followed by keyset pagination
- **Status**: NEEDS REVIEW - Keyset comparison logic may handle this correctly despite the risk, but pattern is brittle
- **Fix**: Use park_id + shed_id in keyset, never names

#### 6. Calendar Events - Shed Name as Display Key
- **File**: backend/internal/calendar/adapters/postgres/canonical_read.go
- **Pattern**: `GROUP BY m.shed_id, COALESCE(NULLIF(l.name, ''), l.location_code, m.shed_id::text)`
- **Issue**: Uses shed.name for display, not operational_location_display; no partition awareness
- **Status**: REPORTED ONLY - touches obligations module (do-not-edit), cannot fix

---

## VERIFICATION - SURFACES ALREADY CORRECT

### Obligation Module (Owned by other agent, verified correct)
- **File**: backend/internal/obligation/adapters/postgres/sqlc/query.sql:127-131
- **Pattern**: Hand-rolled CASE statement with explicit comment:
  ```sql
  -- Must match backend/internal/platform/oploc.OperationalLocation.Display() exactly
  CASE WHEN COALESCE(shed.name, '') = '' THEN ...
       WHEN COALESCE(gsp.partition_label, 'whole') = 'whole' THEN COALESCE(shed.name, '')::text
       WHEN gsp.partition_label ~* '^parts?([[:space:]]|$)' THEN ... || ' - ' || ...
       ELSE ... || ' ' || ...
  END
  ```
- **Status**: ✓ CORRECT - Matches oploc.Display() logic byte-for-byte

### CEO/AI Reporting Views
- **File**: backend/internal/ceoai/ (various)
- **Status**: ✓ CORRECT - Per comments, migration 000114 adds partition_label to vaccination_shed_status and vaccination_dose_pickup views

### Verification Module (Owned by other agent, verified correct)
- **File**: backend/internal/verification/adapters/postgres/repository.go
- **Pattern**: `GROUP BY vi.shed_id, vi.partition_label, shed_loc.name` - includes partition in grouping key
- **Status**: ✓ CORRECT - Includes partition in GROUP BY

---

## SURFACES NOT FOUND / NOT YET LOCATION-BEARING

- **Inventory module**: Has location-related code but no user-facing location surfaces
- **Procurement module**: Has location-related code but no user-facing location surfaces  
- **Breeding module**: Empty - no location surfaces found
- **Actions/Alerts**: Not yet scanned for detailed location handling
- **Timetable/HRMS**: Module not found in codebase
- **Herd Register**: In counts module (do-not-edit), owned by other agent

---

## RECOMMENDATIONS

### Priority 1 - Critical (Blocks Product Requirements)
1. **Goat Passport**: Add location fields to Passport/DueItem/HistoryItem DTOs
   - Add GoatLocation (ParkID, ParkName, ShedID, ShedName, PartitionLabel, OperationalLocationDisplay)
   - Fetch location from goats table (current_shed_id or equivalent) or add LocationReader interface
   - Estimated complexity: MEDIUM (needs reader wiring)

### Priority 2 - High (Blocks Correct Partition Display)
2. **Health Module**: Add partition support
   - Requires migration adding partition_label to health_cases table
   - Update filter options and work item queries to use operational_location_display
   - Estimated complexity: HIGH (requires table schema change + multiple query updates)

### Priority 3 - Medium (Brittle Patterns)
3. **Feed Transport**: Remove name from GROUP BY or add partition support
   - Short term: Change `GROUP BY t.shed_id, s.name` to `GROUP BY t.shed_id` (s.name is 1:1 with shed_id, not needed in GROUP BY)
   - Long term: Add partition_label to feed_transport_tasks if partitions should be supported
   - Estimated complexity: LOW (short term) / MEDIUM (long term)

4. **Vaccination Execution**: Audit pagination keyset logic for name-based ordering
   - Review if keyset comparisons handle park-level name repeats correctly
   - Consider using shed_id in sort order instead of name
   - Estimated complexity: MEDIUM (review required)

### Priority 4 - Blocked (Requires Coordination)
5. **Calendar Events**: Migrate display to use operational_location_display
   - **Status**: Owned by obligation module (do-not-edit), report for other agent

---

## GUARD STATUS

**operational-location-guard**: ✓ PASS (1483 files)

**Note**: Guard does not catch:
- Missing partition_label in new API responses (Passport, etc.)
- Absence of partition support at table level (health_cases, feed_transport_tasks)
- Name-based pagination keysets in queries

These are semantic/architectural gaps outside the guard's regex-based checks.

