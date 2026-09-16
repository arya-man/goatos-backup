# Health SOP source review

Read-only Google Drive/Sheets connector verification on 2026-09-15 as [source account omitted].

Source: [Health DB](https://docs.google.com/spreadsheets/d/1uvDO_vipNsLcB4S0O7Bj-L8VCSMCd0eX5U9cJS8F-QE/edit).
- Adults SOP: sheetId 582297317.
- Kids SOP: sheetId 541230517.
- Reads: A1:AZ25 sentinel, A1:IK100 bounded structure; Dog Bite remaining rows DQ101:DX240 (Adults), DP101:DW240 (Kids).
- 27 disease blocks in each tab. Each repeats Day, Session, Record Type, Medicine, Dosage, Dosage Denominator, Medicine Route, Treatment.

## What changes the design

This is a course/session protocol, not only a question form or instantaneous branch graph.

- Day/session groups contain multiple ordered actions and medication rows.
- Source examples span Fever 3 days, Acidosis 7, Lumps/ORF 28, Dog Bite 30, Fracture 40. Do not flatten repeated days into a huge canvas; show a course outline with day/session subflows.
- Kids Diarrhea includes Morning/Afternoon/Evening and refers to initial intake in later sessions. Answers need case-level persistence.
- Some Kids Diarrhea medication Session cells are blank. They require explicit resolution before scheduling, not an inferred Morning default.
- Fever contains a referral to the Not Eating SOP. Reusable sub-protocol calls need scope/version and return/completion behavior.
- Conditional instructions can discontinue, continue, defer, or escalate a course. Store these as explicit author-validated transitions.
- Item identity belongs to Items Config. Course-specific quantity text, denominator, route, timing, instructions and clinical approval belong to the protocol action.
- Dosage has no universally explicit numerator unit in the columns. Preserve raw text; do not convert it to ml or compute a dose automatically.
- Raw instructions mention location moves and identifier replacement. Those become separate module-owned actions with authorization, not free-text automatic writes.

## Source versus repo snapshot

the checked-in health source findings JSON cites the same spreadsheet and exact tab names, captured 2026-07-30; it contains 54 age-band protocols and at most 28 steps per protocol.

It is not a complete representation of current sheet courses: Dog Bite snapshot says 4 days/28 steps whereas live source has 30 days/134 populated step rows; Fracture snapshot says 13 days/28 steps whereas live source has 40 days/57 rows. Do not seed complete courses from this capped snapshot.

## Bounded prototype example

Fever C2:J18 is identical across both live tabs: three days, Morning sessions, 16 ordered rows. Exact raw fixture is `health-fever-source.json`. Preserve each source row, blanks, spelling, denominator and route as source text. The Fever block does not specify the earlier user example threshold 103°F; that threshold must not be presented as sourced.

Clinical content was inspected only as source structure. No medication, dose or treatment correctness was evaluated or recommended.
