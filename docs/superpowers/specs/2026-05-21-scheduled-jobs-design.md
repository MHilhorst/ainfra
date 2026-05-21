# Scheduled Jobs Channel — Design

**Date:** 2026-05-21
**Status:** deferred. This channel was built as Iteration 4 and then reverted
from `main`. This document and its implementation plan are retained as the
record of the design for when the channel is revisited.
**Iteration:** 4 (follows the hooks/commands channels of Iteration 3)

## 1. What this is — and why it is different

`ainfra` Iteration 3 added the `hooks` and `commands` channels, bringing it to
eight. This iteration adds a ninth surface: **scheduled jobs** — cron-style
recurring commands, the kind a team runs headlessly (e.g. `claude -p` triage
jobs on a hub machine).

It is added as a deliberately **distinct kind of channel**, not "channel 9 like
the others." Channels 1–8 are *per-developer environment*: every targeted
developer reproduces them on their own machine, and the lockfile's job is to
guarantee everyone converges to the **same** state.

A scheduled job is the opposite shape — **shared infrastructure that must run on
designated machines only.** A 4-hourly triage job running on six developer
laptops means six processes racing on one API and one state file. For a
scheduled job, "everyone reproduces it" is the failure mode, not the goal.

The design doc and the manifest spec must both state this distinction up front,
so the channel is never mistaken for a reproduce-everywhere channel.

## 2. The honest limitation — label-and-trust

`ainfra` is a **local, no-daemon, registry-less** tool (design §2). It can label
a machine `hub` and *trust* the team to label exactly one machine `hub`. It
**cannot** detect a second accidental `hub`, nor a zero-`hub` gap — it has no
view of any machine but the one it runs on.

For a per-developer channel that is harmless ("more machines have it" is fine).
For a singleton scheduled job running an autonomous command, running on two
machines is a real incident (double cost, racing state). **`ainfra` structurally
cannot prevent that.** This is stated plainly, not hidden behind a tidy field.

What `ainfra` *does* guarantee is **local**: `check`, run on a machine, confirms
"this machine's targets are X; the jobs whose `runsOn` intersects X are
installed, on the recorded schedule, with content matching the lock." A targeted
job therefore trades `ainfra`'s uniform-reproduction guarantee for
**label-and-trust** plus per-machine drift detection.

No `singleton: true` field is introduced. `ainfra` cannot enforce uniqueness, so
such a field would be enforcement theatre; the concept is documented instead.

## 3. Schema

### 3.1 The target vocabulary (governed)

The team layer declares the set of valid target labels once. The vocabulary is
open — a team extends it freely — but *governed*: declared centrally, not
invented ad-hoc per machine.

```yaml
targets: [hub, laptop, ci]      # top-level; the governed label set
```

Layers are merged by union: any layer may contribute labels to the vocabulary.

### 3.2 A scheduled job

```yaml
scheduledJobs:
  flare-triage:
    schedule: "0 */4 * * *"               # cron expression — required
    command: claude -p "$(cat prompts/flare-triage.md)"   # required
    source: ./scripts/flare-triage.sh     # optional — a script the tool installs
    runsOn: [hub]                         # required — every label must be in `targets`
    description: Triage new Flare errors. # optional
    requires:
      - cliTool: claude                   # dependency edges (§9 graph)
    enabled: true                         # common field
    overridable: false                    # common field
```

`runsOn` is **required** and **non-empty** — a scheduled job must say where it
runs. There is deliberately no "omit `runsOn` = runs everywhere" default; that
default is the dangerous one for infrastructure.

### 3.3 Machine self-identification

A machine declares what it is, in its **personal layer** (`ainfra.personal.yaml`
— the per-developer, per-machine file):

```yaml
host:
  targets: [hub]                # every label must be in the vocabulary
```

For ephemeral machines with no personal file checked out (cloud runners, CI),
the `AINFRA_TARGETS` environment variable overrides `host.targets`
(e.g. `AINFRA_TARGETS=ci,cloud`). This is consumed at `apply` time (§6).

## 4. The mechanism

`ainfra` generates a **tagged local `crontab` entry** for each scheduled job
whose `runsOn` intersects the machine's targets — the same `# managed:` tag
pattern a hand-rolled `manage-crons.sh` already uses, so install/update/remove
is idempotent. The OS `cron` daemon runs the job.

`ainfra` **generates and checks; it never supervises** — identical to the
boundary it already holds for the launchd SSH tunnels (design §7). A generated
crontab line is not a supervised process, so this does not violate the §9
"no process supervision" non-goal.

## 5. Lockfile representation

A scheduled job's lock entry records its `layer`, its `runsOn` targets, and a
`contentHash` over its declared config (`schedule`, `command`, `source`,
`runsOn`, `description`). The lockfile is **machine-agnostic**: `lock` resolves
*every* scheduled job regardless of the machine it runs on, and records each
one's `runsOn`. The committed lockfile is therefore byte-identical for everyone.

The **per-machine filtering** — "which of these jobs does *this* machine
install?" — happens at `apply` time, never at `lock` time.

Personal-layer scheduled jobs honour the layered-lockfile split (lockfile spec
§7): they go to `ainfra.personal.lock`, not the committed `ainfra.lock`.

## 6. Validation rules

Static validation (`ainfra` `Validate`), in sorted-key order for deterministic
errors:

- Each `scheduledJobs` entry: `schedule` non-empty, `command` non-empty,
  `runsOn` non-empty.
- Every label in a job's `runsOn` must appear in the merged `targets`
  vocabulary — otherwise a validation error naming the job and the bad label.
- Every label in `host.targets` (when present) must appear in the vocabulary.

Cron-expression *syntax* validation (field count, ranges) is **not** in scope
for this iteration — `schedule` is only checked non-empty. Syntax validation is
a small follow-up or an `apply`-time check.

## 7. Dependency graph

A scheduled job may declare `requires` edges (e.g. `cliTool: claude`). These are
wired into the dependency graph through the existing unified `addRequireEdges`
helper, with the node prefix `job:`. The topo-sort and cycle check therefore
span scheduled jobs like every other channel.

## 8. Scope of this iteration

This iteration builds the channel **through `ainfra lock`**, exactly parallel to
how Iteration 3 delivered hooks and commands:

**In scope:**
- The top-level `targets` vocabulary and the `host` block (manifest types).
- The `scheduledJobs` channel (manifest type, validation, lockfile entry,
  pipeline resolution, graph wiring).
- The multi-database example gains one scheduled job exercising the channel
  end-to-end through `ainfra lock`.
- Spec updates: manifest schema (a new section), lockfile schema, `design.md`
  (eight channels → nine, with the targeted-infrastructure framing), and the
  assessment doc (the cron "open gap" is now closed).

**Deferred — `apply`-time, with every other channel's `apply`:**
- Generating, updating, and removing the tagged `crontab` entries.
- The `runsOn` ↔ machine-targets filtering, and the `AINFRA_TARGETS` override.
- `check` verifying installed crontab entries against the lock.

`apply` does not yet exist for *any* channel — it is the deferred providers
plan. This iteration makes `scheduledJobs` a fully-specced, validated,
locked, drift-detectable channel; the crontab mechanism lands when `apply` does,
for all nine channels together.

## 9. Non-goals

- No remote execution / push: each machine runs `ainfra` itself and writes its
  own local `crontab`. `ainfra` never reaches out to other machines.
- No uniqueness enforcement for singleton jobs (§2 — `ainfra` cannot, so it
  does not pretend to).
- No cron-expression syntax validation this iteration (§6).
- No process supervision — the OS `cron` daemon owns the running job (§4).
