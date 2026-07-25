---
description: Scaffold an OpenSpec change (PLAN + SPEC delta + TASKS) for a feature.
---
Create `openspec/changes/$ARGUMENTS/` with `proposal.md` (why + what + governing ADR),
`design.md` (module layout + ports + test strategy), `tasks.md` (T-### vertical slices),
and delta specs under `specs/<capability>/spec.md` using `## ADDED/MODIFIED/REMOVED
Requirements` with `### Requirement:` + `#### Scenario:` WHEN/THEN. Then STOP for approval.
