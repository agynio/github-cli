This folder is used for documentation related to developing `gh`. Docs for `gh` installation and usage are available at [https://cli.github.com/manual](https://cli.github.com/manual).

## Pending review inline comments

- `gh pr review add-comment` now resolves diff positions against the pending
  review's commit by calling the commit REST API. This ensures `--line/--side`
  inputs match the final diff on GitHub.
- When the append endpoint unexpectedly returns 404, the CLI creates a new
  pending review pinned to the same commit and replays the comment payloads.
- GitHub enforces a single pending review per user; if fallback creation fails
  with a 422, instruct users to abort or submit the other pending review before
  retrying.
