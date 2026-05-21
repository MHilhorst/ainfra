#!/usr/bin/env node
// PreToolUse/Bash hook: escalate destructive SQL to user approval.
//
// Declared in ainfra.yaml under `hooks.guard-destructive-sql`. ainfra installs
// this script and wires the hook into Claude Code settings; it does not own
// the script's behaviour. Reads the Claude Code hook payload from stdin.

const DESTRUCTIVE = /\b(DROP\s+(DATABASE|TABLE|SCHEMA)|TRUNCATE\s+TABLE|DELETE\s+FROM\s+\w+\s*;?\s*$)/i;

let raw = "";
process.stdin.on("data", (c) => (raw += c));
process.stdin.on("end", () => {
  let cmd = "";
  try {
    cmd = (JSON.parse(raw).tool_input || {}).command || "";
  } catch {
    process.exit(0); // malformed payload: do not block
  }
  if (DESTRUCTIVE.test(cmd)) {
    process.stdout.write(
      JSON.stringify({
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: "ask",
          permissionDecisionReason:
            "Destructive SQL against a tunnelled production database — confirm before running.",
        },
      })
    );
  }
  process.exit(0);
});
