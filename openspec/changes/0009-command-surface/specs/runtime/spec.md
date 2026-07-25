# Runtime Specification (delta — change 0009)

## Purpose
A command surface reaching the built-but-unreachable milestone subsystems:
explicit `/skill` invocation, compose mode, `/dream` and `/distill`, and the
`MEMORY.md` file memory they write to.

## Requirements

### Requirement: Command dispatch
Input beginning with `/` SHALL be dispatched as a command; anything else SHALL run
as a normal turn.

#### Scenario: Plain input is still a turn
- **WHEN** the user submits text not beginning with `/`
- **THEN** the agent loop runs exactly as before

#### Scenario: Known command dispatches without a model call
- **WHEN** the user submits a known command
- **THEN** the command runs and no provider request is made

#### Scenario: Unknown command is actionable
- **WHEN** the user submits `/nonsense`
- **THEN** an error naming the unknown command and listing the known ones is
  returned

#### Scenario: A command never silently succeeds
- **WHEN** any command completes
- **THEN** it returns either a description of what it changed or an error naming
  what was missing, never empty success

### Requirement: Explicit skill invocation
`/skill <name>` SHALL load a discovered skill by exact name (ADR-0002 #8), and
`/skills` SHALL list what is discoverable.

#### Scenario: Named skill is returned
- **WHEN** a skill named `deploy` is discoverable and the user runs `/skill deploy`
- **THEN** that skill's instructions are returned

#### Scenario: Missing skill names what is available
- **WHEN** the user invokes a skill that does not exist
- **THEN** the error says so and lists the discoverable skill names

#### Scenario: Listing with no skills is not an error
- **WHEN** `/skills` runs in a project with none
- **THEN** it reports that none were found rather than failing

### Requirement: Compose mode
`/compose <feature>` SHALL start a compose session, and phase verbs SHALL drive it
through the gates, refusing any transition whose gate is unsatisfied
(ADR-0002 #6).

#### Scenario: Compose starts at grill
- **WHEN** `/compose widget-api` runs
- **THEN** a compose session for that feature begins at the grill phase

#### Scenario: The spec gate is enforced through the command surface
- **WHEN** the user tries to advance past spec without an approved spec
- **THEN** the command reports the blocked gate and the phase does not change

#### Scenario: Phase verbs require an active compose session
- **WHEN** a phase verb runs with no compose session started
- **THEN** an error says to start one first

### Requirement: Dream extracts to file memory
`/dream` SHALL extract durable facts from the session transcript and append them
to `MEMORY.md`, reporting how many were added (ADR-0002 #9 and the file-memory
half of #1).

#### Scenario: Extracted facts are appended
- **WHEN** `/dream` runs after a session with durable facts in it
- **THEN** those facts are appended to `MEMORY.md` and the count is reported

#### Scenario: Existing memory is never rewritten
- **WHEN** `MEMORY.md` already has hand-written content and `/dream` runs
- **THEN** the existing content is intact and the new facts follow it

#### Scenario: Nothing worth remembering is reported honestly
- **WHEN** `/dream` extracts no facts
- **THEN** it says so and `MEMORY.md` is unchanged

#### Scenario: Dream requires a transcript
- **WHEN** `/dream` runs with no session history
- **THEN** an error says there is nothing to extract from

### Requirement: Distill mines runs into scaffolds
`/distill` SHALL mine recorded tool sequences and write a scaffold for the
strongest pattern, choosing skill, subagent, or command by pattern shape
(ADR-0002 #9).

#### Scenario: A repeated pattern becomes a discoverable scaffold
- **WHEN** a tool sequence recurs across recorded runs and `/distill` runs
- **THEN** a scaffold is written and the skill index discovers it

#### Scenario: No pattern is reported honestly
- **WHEN** no sequence recurs often enough
- **THEN** `/distill` says so and writes nothing

#### Scenario: An existing scaffold is never clobbered
- **WHEN** a scaffold of that name already exists
- **THEN** `/distill` refuses rather than overwriting a hand-edited file
