// SPDX-License-Identifier: MIT
package collect

// Two questions used to be answered by one predicate, and they have different answers:
//
//	"is this directory part of the artifact's IDENTITY?"   → hashing
//	"should this directory's content be READ?"             → scanning
//
// Conflating them meant `dist/setup.sh` could hold `curl … | bash` and no rule would ever run on
// it, because the same list that keeps a hash stable across a rebuild also kept the reader out.
// The two lists are separate, and the asymmetry is the point:
//
//   - ExcludeFromHash must not change. The canonical hash is the reputation database's key, so it
//     has to be identical on two machines and after a rebuild. `.git/index`, `FETCH_HEAD` and
//     reflogs carry per-checkout state; `node_modules/` differs by lockfile resolution and
//     platform. Adding a directory here is a breaking change to every stored hash.
//   - ExcludeFromScan is SMALLER, because reading costs nothing but noise. Off it: the artifact's
//     OWN build output — `dist/`, `build/`, `out/`, `.next` — which is generated from the code in
//     the same tree and is exactly where a payload hides from a name-based skip. On it:
//     third-party trees, because findings in `node_modules/` or `vendor/` describe somebody else's
//     dependency, not this artifact, and a scanner that reports them teaches its user to skim;
//     and `.git/`, whose `hooks/*.sample` files ship with `curl`-shaped examples that were
//     measured producing a high-severity finding about the sample.
//
// `test/` is on neither list, deliberately: a payload can hide in tests, so test files are hashed
// and scanned like any other authored file.

// ExcludeFromHash names directories left out of the canonical tree hash. Changing this set changes
// every artifact's identity, so it is effectively frozen.
var ExcludeFromHash = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	"vendor": true, ".next": true, "out": true, "coverage": true,
	// Claude Code's own session markers. An installed plugin under plugins/cache grows a
	// `.in_use/<pid>` file per live session — nothing the author shipped, and a different name
	// every time. Measured: the figma plugin's cache hashed differently from the marketplace
	// commit it was installed from, and the ONLY difference was two of these files. Without this
	// entry no reputation hash can ever match an installed plugin while Claude Code is running,
	// which is the only time anyone scans. This is the one addition the "frozen" rule above
	// permits: trees that contain `.in_use/` never had a stable hash to break.
	".in_use": true,
}

// ExcludeFromScan names directories whose CONTENT is not read. Deliberately narrower than
// ExcludeFromHash: an artifact's own build output is read, because that is where a payload hides
// behind a name.
var ExcludeFromScan = map[string]bool{
	// Third-party trees: their findings are about somebody else's dependency.
	"node_modules": true, "vendor": true,
	// Python third-party trees and caches. A venv holds downloaded packages and a copy of the
	// interpreter, not authored code; the site-packages under it is where numpy/pygments/setuptools
	// live, and their docstrings and filename tables trip the keyword rules — a numpy docstring
	// demoing a REFUSED open("~/.ssh/id_dsa") reads as FS-001. Measured on a real vedic-calculator
	// skill: ~1100 dependency files scanned, 26 high-severity false positives, the artifact forced
	// to 0/100. On the SCAN list only, never the HASH list — skipping them changes what is read,
	// not an artifact's identity.
	"venv": true, ".venv": true, "site-packages": true, "__pycache__": true,
	// Version-control metadata. Not authored content, and git's bundled hooks/*.sample files contain
	// curl-shaped examples that were measured producing a high-severity finding about a sample.
	".git": true,
	// Test-coverage output: machine-generated reports, no authored content.
	"coverage": true,
	// Claude Code session markers (see ExcludeFromHash). Extensionless 50-byte files: unread
	// they would surface as a COV-000 "unknown extension" disclosure about the editor's own
	// bookkeeping, on every scan, for every installed plugin.
	".in_use": true,
}

// ExcludedFromHash reports whether a directory name is left out of the canonical hash.
func ExcludedFromHash(name string) bool { return ExcludeFromHash[name] }

// ExcludedFromScan reports whether a directory name's content is left unread.
func ExcludedFromScan(name string) bool { return ExcludeFromScan[name] }
