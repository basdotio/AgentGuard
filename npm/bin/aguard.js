#!/usr/bin/env node
// SPDX-License-Identifier: MIT
//
// Launcher for the npm distribution. The real program is a static Go binary that ships inside
// one per-platform package; this file finds the one matching the host and runs it, passing
// stdin/stdout/stderr and the exit code straight through.
//
// Two properties are load-bearing and neither is cosmetic:
//
//   stdio: "inherit" — `aguard hook` reads a Claude Code hook event as JSON on STDIN. A pipe
//   that does not reach the child turns the load-time gate into a gate that never sees the
//   event it was asked about.
//
//   exit codes — they are this tool's contract: 0 below threshold, 1 a finding at or above
//   --fail-on, 2 runtime error, 3 (clean only) acted partially. So this launcher must never
//   invent a 0: every path where the scanner did not actually reach a verdict exits 2. That is
//   the same rule the tool states about itself — "exit code 2 is not a pass, the scan did not
//   happen" — and it is why this file deliberately does NOT copy the common
//   `process.exitCode = result.status` ending: `status` is null when a child dies from a
//   signal, and null makes Node exit 0, i.e. renders a killed scan as a clean one.

"use strict";

const { spawnSync } = require("child_process");

// 2 = runtime error. Used for every "we did not get an answer" path below.
const EXIT_RUNTIME_ERROR = 2;

function die(message) {
  process.stderr.write(`aguard: ${message}\n`);
  process.exit(EXIT_RUNTIME_ERROR);
}

// Everything about the platform set is READ from package.json rather than restated here: each
// platform package is this package's name plus a `<os>-<cpu>` suffix, and they are already
// listed as optionalDependencies. Keeping a second copy in this file would let the two drift —
// a platform that is built and pinned but missing from the copy would be refused here even
// though npm had installed its binary.
const { name, version, optionalDependencies } = require("../package.json");
const { platform, arch } = process;

const suffixes = Object.keys(optionalDependencies || {})
  .filter((dep) => dep.startsWith(`${name}-`))
  .map((dep) => dep.slice(name.length + 1));

const platformPackage = `${name}-${platform}-${arch}`;

if (!suffixes.includes(`${platform}-${arch}`)) {
  die(
    `no prebuilt binary for ${platform}-${arch}. Supported: ` +
      (suffixes.join(", ") || "none declared") +
      ". Download a release binary or build from source: " +
      "https://github.com/basdotio/guard/releases/latest",
  );
}

let binary;
try {
  binary = require.resolve(`${platformPackage}/aguard`);
} catch {
  // The optional dependency is missing: installed with --no-optional, restored from a lockfile
  // made on another platform, or a partial install. Say which package, because the fix is to
  // install it and nothing here can guess why it is absent.
  die(
    `the platform package ${platformPackage} is not installed, so there is no binary to run. ` +
      `Reinstall ${name} (or run: npm i ${platformPackage}@${version}).`,
  );
}

const result = spawnSync(binary, process.argv.slice(2), {
  shell: false,
  stdio: "inherit",
});

if (result.error) {
  die(`could not run ${binary}: ${result.error.message}`);
}

if (typeof result.status === "number") {
  process.exit(result.status);
}

// Killed by a signal: status is null, and letting null reach process.exitCode would make Node
// exit 0 — i.e. render a killed scan as a clean one. Reproduce what the shell would have
// reported for the binary itself (128 + signal) instead of inventing a code of our own, so
// running through the launcher behaves like running the binary.
//
// Whether to SAY anything is a separate question from the exit code, and the dividing line is
// not one signal but a category: these five are how a person or the OS asks a program to stop.
// Ctrl-C, closing the terminal, `kill`, and `aguard scan | head` closing the pipe are all
// ordinary, and the binary alone prints nothing for any of them — so a launcher that announced
// them would add a scary line, naming a file inside node_modules, to the most common way of
// interrupting a long scan. What is left (SIGSEGV, SIGBUS, SIGABRT, SIGKILL from an OOM kill)
// is the binary failing rather than being asked to stop, and that is worth a line.
const ROUTINE_SIGNALS = ["SIGINT", "SIGTERM", "SIGHUP", "SIGQUIT", "SIGPIPE"];

if (result.signal) {
  const signo = require("os").constants.signals[result.signal];
  if (!ROUTINE_SIGNALS.includes(result.signal)) {
    process.stderr.write(
      `aguard: ${binary} was terminated by ${result.signal} before it produced a verdict\n`,
    );
  }
  process.exit(typeof signo === "number" ? 128 + signo : EXIT_RUNTIME_ERROR);
}

die(`${binary} exited without a status`);
