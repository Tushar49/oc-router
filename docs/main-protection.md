# Main and tag protection

Configured 2026-10-28 with `gh` on `Tushar49/oc-router`.

## Branch ruleset

```text
name:       protect-main
id:         26033895
target:     branch
ref:        refs/heads/main
enforcement: active
rules:      deletion, non_fast_forward
bypass:     none (current_user_can_bypass=never)
```

This blocks deleting `main` and force-pushing or otherwise rewriting its
history. It does not require a pull request, linear history, or signed commits:
`scripts/promote.mjs` pushes a normal merge commit directly, and `guard-main.yml`
plus CI are the post-push gates. The promotion merge is a fast-forward from
`main`'s first parent, so `non_fast_forward` does not block it.

Do not add a bypass actor just to make automation work. If a recovery needs a
rule change, change the ruleset deliberately, record it here, and restore the
protection afterward.

## Release-tag ruleset

```text
name:       protect-release-tags
id:         26033934
target:     tag
ref:        refs/tags/v*
enforcement: active
rules:      deletion, update
bypass:     none (current_user_can_bypass=never)
```

Release tags can be created, but an existing `v*` tag cannot be moved or
deleted. `release.yml` also verifies that the tag commit is an ancestor of
`origin/main` before building.

## Remaining release gates

- Guard main and CI must be green on the promoted `main` commit before tagging.
- Never tag from `dev`.
- The release workflow has not been smoke-tested with `cosign` on this host;
  do not create a real release tag until that gate is addressed.
