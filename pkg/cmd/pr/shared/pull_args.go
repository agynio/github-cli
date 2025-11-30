package shared

import (
	"strconv"
	"strings"

	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmdutil"
)

// ResolvePullRequest resolves repository and pull request number from a combination of
// positional selector and --pr flag. The selector may be a pull request URL, an
// "owner/repo#number" reference, or a plain number with an optional leading '#'.
// When the selector does not include repository information, the provided baseRepo
// function is used to determine it (which honors `-R/--repo` overrides).
func ResolvePullRequest(baseRepo func() (ghrepo.Interface, error), selector string, prFlag int) (ghrepo.Interface, int, error) {
	if selector != "" {
		if repo, number, _, err := ParseURL(selector); err == nil {
			if prFlag > 0 && prFlag != number {
				return nil, 0, cmdutil.FlagErrorf("pull request argument %q does not match --pr=%d", selector, prFlag)
			}
			return repo, number, nil
		}

		if repo, number, err := ParseFullReference(selector); err == nil {
			if prFlag > 0 && prFlag != number {
				return nil, 0, cmdutil.FlagErrorf("pull request argument %q does not match --pr=%d", selector, prFlag)
			}

			if baseRepo != nil {
				if base, err := baseRepo(); err == nil {
					repo = ghrepo.NewWithHost(repo.RepoOwner(), repo.RepoName(), base.RepoHost())
				}
			}
			return repo, number, nil
		}

		trimmed := strings.TrimPrefix(selector, "#")
		if trimmed != "" {
			if n, err := strconv.Atoi(trimmed); err == nil && n > 0 {
				if prFlag > 0 && prFlag != n {
					return nil, 0, cmdutil.FlagErrorf("pull request argument %q does not match --pr=%d", selector, prFlag)
				}

				repo, err := baseRepo()
				if err != nil {
					return nil, 0, err
				}
				return repo, n, nil
			}
		}

		return nil, 0, cmdutil.FlagErrorf("invalid pull request argument: %q", selector)
	}

	if prFlag <= 0 {
		return nil, 0, cmdutil.FlagErrorf("must specify a pull request via --pr or as an argument")
	}

	repo, err := baseRepo()
	if err != nil {
		return nil, 0, err
	}

	return repo, prFlag, nil
}
