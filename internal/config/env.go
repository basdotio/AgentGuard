// SPDX-License-Identifier: MIT
package config

import "os"

// renamedEnv maps each environment variable the tool read before its rename to BAS Guard to the
// name it reads first now (P-082). The old name keeps working; the new one wins when both are set.
// hack/pre-commit applies the same rule to its own variable, AGUARD_FAIL_ON.
var renamedEnv = map[string]string{
	"AGUARD_LLM_KEY":   "BAS_GUARD_LLM_KEY",
	"AGUARD_USAGE_LOG": "BAS_GUARD_USAGE_LOG",
}

// LookupEnv returns the non-empty value of name, trying its BAS_GUARD_ form first, and the
// variable that supplied it; ("", "") when neither is set. A name with no BAS_GUARD_ form (an
// api_key_env the operator chose) is read as given.
func LookupEnv(name string) (value, from string) {
	if alt, ok := renamedEnv[name]; ok {
		if v := os.Getenv(alt); v != "" {
			return v, alt
		}
	}
	if v := os.Getenv(name); v != "" {
		return v, name
	}
	return "", ""
}
