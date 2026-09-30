<!-- SPDX-License-Identifier: MIT -->
# Notices

AgentGuard is MIT licensed (see LICENSE).

**Benchmark results** under `baselines/results/` were produced by running scanners over
[agent-artifact-corpus](https://github.com/basdotio/agent-artifact-corpus). The ledgers and
verdict files name that corpus's sample identifiers and, in `detail` fields, quote short
fragments of what a scanner printed about them. The samples themselves are not in this
repository; their provenance and licences are recorded in the corpus repository's NOTICE.

**Third-party scanners** measured for comparison, each run from its published release and
recorded with version and checksum in the corresponding `run.yaml`:
[cc-audit](https://github.com/ryo-ebata/cc-audit) (MIT) and
[Cisco skill-scanner](https://github.com/cisco-ai-defense/skill-scanner). Their figures are
marked `provisional: true`: agent-artifact-corpus is public and each vendor can measure itself.

**Rule taxonomy** references the OWASP Agentic Security Initiative and MITRE ATLAS naming; the
rules are re-derived, not ported from any other scanner.
