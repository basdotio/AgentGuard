#!/usr/bin/env node
// SPDX-License-Identifier: MIT
//
// The unscoped `bas-guard` package (P-082) exists so that `npx bas-guard` runs this tool and the
// name cannot be published by anyone else. It carries no binary and no platform list: it runs the
// launcher of __NAME__, which it depends on at exactly its own version, so the launcher's contract
// (stdio passed through, the binary's exit code reproduced, 2 whenever no verdict was reached) is
// the only one there is. `make npm-dist` fills in __NAME__.

"use strict";

require("__NAME__/bin/bas-guard.js");
