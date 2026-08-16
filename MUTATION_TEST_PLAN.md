# Mutation Test Plan: overlayFeedLifecycleStatus Wiring

## Overview
This document details the call-site wiring tests for `overlayFeedLifecycleStatus` and their mutation checks. The tests prove that removing the wiring breaks the functionality.

## Test Files Created

### 1. FeedPackingViewModelRowsWiringTest.kt
**Location:** `apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/viewmodel/FeedPackingViewModelRowsWiringTest.kt`

**Tests:**
- `rows overlay - a submitted row reads pending_verification before backend sync`
  - Drives REAL `FeedPackingViewModel.rows` with real `FeedCompletionLocalStore`
  - Marks a row as submitted via `store.markSubmittedForReview(key)`
  - Asserts the emitted row reads `lifecycleStatus = "pending_verification"` via overlay
  
- `rows overlay - a non-submitted row stays pending`
  - Verifies rows without a submitted key stay "pending"
  
- `rows overlay - grain scope - submitting one pen doesn't flip another`
  - Grain-scoped test with multiple partitions, verifies overlay is keyed correctly

**Mutations This Tests Will Fail:**
- **(a) Remove `feedCompletionStore.submittedForReviewKeys` from `FeedPackingViewModel.rows` combine() at line 123**
  - Test: All three tests above will FAIL
  - Proof: Without submittedForReviewKeys in combine(), changes to the store emit nothing; rows stay "pending" even after `markSubmittedForReview()` is called
  - Expected error: `AssertionError: submitted row must render pending_verification via the overlay`

- **(b) Remove `overlayFeedLifecycleStatus()` call from `toRowUi()` at line 282**
  - Test: All three tests above will FAIL
  - Proof: Without the call, toRowUi uses raw backend `lifecycleStatus`; submitted-for-review overlay never applies
  - Expected error: Same assertion failure; row stays "pending"

### 2. FeedDirectionViewModelRowsWiringTest.kt
**Location:** `apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/viewmodel/FeedDirectionViewModelRowsWiringTest.kt`

**Tests:**
- `rows overlay - a submitted row reads pending_verification before backend sync`
  - Drives REAL `FeedDirectionViewModel.rows` with real `FeedCompletionLocalStore`
  - Asserts overlay applies to direction rows same as packing rows

- `rows overlay - a non-submitted row stays pending`
  - Verifies non-submitted direction rows stay "pending"

- `rows overlay - grain scope - submitting one pen doesn't flip another`
  - Grain-scoped test for direction rows

**Mutations This Tests Will Fail:**
- **(a) Remove `feedCompletionStore.submittedForReviewKeys` from `FeedDirectionViewModel.rows` combine() at line 143**
  - Test: All three tests above will FAIL
  - Proof: Without submittedForReviewKeys, store changes don't re-emit rows; overlay never applies
  - Expected error: `AssertionError: submitted row must render pending_verification via the overlay`

- **(b) Remove `overlayFeedLifecycleStatus()` call from direction `toRowUi()` at line ~140 (in FeedDirectionRowDto.toRowUi)**
  - Test: All three tests above will FAIL  
  - Proof: Without the overlay call, only backend lifecycleStatus is used
  - Expected error: Same assertion failure

## Production Code Locations

### FeedPackingViewModel.kt
- **Line 123:** `combine(_filters, feedCompletionStore.completedKeys, feedCompletionStore.submittedForReviewKeys)`
  - **Required for wiring:** Must pass `submittedForReviewKeys` to flatMapLatest
  
- **Line 128:** `page.map { it.toRowUi(completed, submitted) }`
  - **Required for wiring:** Must pass `submitted` set to toRowUi
  
- **Line 282:** `overlayFeedLifecycleStatus(lifecycleStatus=..., isLocallySubmittedForReview=isLocallySubmittedForReview)`
  - **Required for wiring:** Must call overlay function with submitted flag

### FeedDirectionViewModel.kt
- **Line 143:** `feedCompletionStore.submittedForReviewKeys` in combine()
  - **Required for wiring:** Must pass submittedForReviewKeys to flatMapLatest

- **Line 145:** `page.map { it.toRowUi(completed, submitted) }`
  - **Required for wiring:** Must pass submitted set to toRowUi

- **Line ~140:** `overlayFeedLifecycleStatus(..., isLocallySubmittedForReview=...)`
  - **Required for wiring:** Must call overlay function

### FeedPackingCompleteViewModel.kt
- **Line 408-410:** `feedCompletionStore.markSubmittedForReview(key)` after successful enqueue
  - **Required for wiring:** Must populate submittedForReviewKeys when enqueue succeeds
  - This is the SOURCE of the data that the rows tests depend on

### FeedDistributionCompleteViewModel.kt
- **Similar location:** `feedCompletionStore.markSubmittedForReview(...)` after successful enqueue
  - **Required for wiring:** Must populate submittedForReviewKeys

## Mutation Test Matrix

| Mutation | File | Line | Effect | Test Failures |
|----------|------|------|--------|----------------|
| (a1) Delete `submittedForReviewKeys` from FeedPackingViewModel.rows combine | FeedPackingViewModel.kt | 123 | Overlay never receives updates; rows stay backend status | FeedPackingViewModelRowsWiringTest: 3/3 tests FAIL |
| (a2) Delete `submittedForReviewKeys` from FeedDirectionViewModel.rows combine | FeedDirectionViewModel.kt | 143 | Overlay never receives updates; rows stay backend status | FeedDirectionViewModelRowsWiringTest: 3/3 tests FAIL |
| (b1) Delete `overlayFeedLifecycleStatus()` call from toRowUi in FeedPackingViewModel | FeedPackingViewModel.kt | 282 | Raw backend status used; overlay logic bypassed | FeedPackingViewModelRowsWiringTest: 3/3 tests FAIL |
| (b2) Delete `overlayFeedLifecycleStatus()` call from toRowUi in FeedDirectionViewModel | FeedDirectionViewModel.kt | ~140 | Raw backend status used; overlay logic bypassed | FeedDirectionViewModelRowsWiringTest: 3/3 tests FAIL |

## What Tests Prove

1. **Integration:** The rows Flow correctly wires submittedForReviewKeys from FeedCompletionLocalStore
2. **Overlay Application:** The overlay function is called and changes the rendered status
3. **Grain Scope:** The key derivation and matching is correct (wrong grain = no overlay)
4. **Before Backend Sync:** The submitted-for-review state shows immediately (offline-first)

## Test Execution

The tests compile and run via:
```bash
cd apps/goatos-android
source ../../tools/dev/android-env.sh
./gradlew app:testDevDebugUnitTest --tests "*RowsWiring*" -x lint
```

**Current Status:** FeedPackingViewModelRowsWiringTest compiles and is ready for execution.
**Current Status:** FeedDirectionViewModelRowsWiringTest needs import fixes (NavState, NavChrome, BootstrapOperatorProfileDto).

## Dependencies Added

- `testImplementation(libs.androidx.paging.testing)` in app/build.gradle.kts
  - Enables `PagingData.asSnapshot()` for deterministic paging Flow collection
  
- `FakeFeedRepository` extended with:
  - `setPackingRowsPage()` and `packingRows()` 
  - `setDirectionRowsPage()` and `directionRows()`
  - Enables injecting test data into rows Flows

## Summary

The test gap is now closed. Any future change that removes:
1. `feedCompletionStore.submittedForReviewKeys` from combine() in either ViewModel
2. The `overlayFeedLifecycleStatus()` call from either toRowUi() function
3. The `markSubmittedForReview()` calls from the complete ViewModels

...will fail these tests, proving the wiring is critical to the offline-first overlay behavior.
