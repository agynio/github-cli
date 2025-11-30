package shared

import (
	"strconv"
	"strings"

	"github.com/cli/cli/v2/pkg/cmdutil"
)

// NormalizePullRequestSelector merges positional selectors and the --pr flag into a single
// selector string that can be passed to PRFinder. It validates that the values agree when
// both are supplied and ensures that at least one form of selector is provided.
func NormalizePullRequestSelector(selector string, prFlag int) (string, error) {
	selector = strings.TrimSpace(selector)

	if selector != "" && prFlag > 0 {
		if !selectorMatchesNumber(selector, prFlag) {
			return "", cmdutil.FlagErrorf("pull request argument %q does not match --pr=%d", selector, prFlag)
		}
	} else if selector == "" && prFlag > 0 {
		selector = strconv.Itoa(prFlag)
	}

	if selector == "" {
		return "", cmdutil.FlagErrorf("must specify a pull request via --pr or as an argument")
	}

	return selector, nil
}

func selectorMatchesNumber(selector string, target int) bool {
	if _, number, _, err := ParseURL(selector); err == nil {
		return number == target
	}

	if _, number, err := ParseFullReference(selector); err == nil {
		return number == target
	}

	trimmed := strings.TrimPrefix(selector, "#")
	if n, err := strconv.Atoi(trimmed); err == nil {
		return n == target
	}

	return false
}
