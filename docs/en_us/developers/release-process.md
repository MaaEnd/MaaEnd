# Release Process

This document describes MaaEnd's release branch model, PR target branch selection, and the release operation process for maintainers.

---

## General Developers

The following content is essential for all contributors to understand. It primarily concerns **which branch to submit PRs to**.

### Branch Model

| Branch         | Purpose                                        | Allowed merge types                                       |
| -------------- | ---------------------------------------------- | --------------------------------------------------------- |
| `v2`           | Main development branch                        | `feat` `refactor` `perf` `fix` `docs` `chore` … All types |
| `release/vX.Y` | Current release branch (e.g., `release/v2.16`) | **Only `fix`**                                            |

> Daily development (new features, refactoring, bug fixes) defaults to submitting PRs to the `v2` branch. The `release/vX.Y` branch is used only in specific scenarios.

### Which branch should I submit a PR to?

```text
What is your change?
  ├─ New feature / Refactoring / Daily fix  → Submit to the v2 branch
  └─ Fix a bug affecting the Stable version  → Submit to the release/vX.Y branch
```

**Simple check: Does this bug also exist in the latest Stable version?**

- No (only appears in the Beta version) → `v2`
- Yes → `release/vX.Y`

> If you submit a fix PR to the `release/vX.Y` branch, after merging, CI will **automatically** cherry-pick the fix back to `v2` and create a PR. **You do not need to submit two PRs.**

### Branch Naming

- Feature branches: `feat/short-description` (e.g., `feat/auto-sell-items`)
- Fix branches: `fix/short-description`

---

## Maintainers

The following content is intended for maintainers (those with release permissions) only.

### Release Cycle

```text
Daily     Beta (vX.Y.Z-beta.N) → Based on v2 (No Beta on Thursdays; RC is released instead)
    Thursday  RC (vX.Y.Z-rc.N) → Based on v2
           ├─ CI automatically creates the release/vX.Y branch
           └─ Thereafter, the release branch only accepts fix PRs
    Friday    Stable (vX.Y.0) → Based on release/vX.Y
           ├─ CI automatically cleans up old release/v* branches
           └─ Hotfixes (vX.Y.1, vX.Y.2 …) can be released as needed
```

### Release Operations

1. **Beta / RC**: Create a tag on `v2` (e.g., `v2.17.0-beta.1`, `v2.17.0-rc.1`). Pushing the tag triggers CI build and release.
2. **Stable**: Create a tag on the `release/vX.Y` branch (e.g., `v2.17.0`). Pushing the tag triggers the official build and release.
3. **Hotfix**: After merging the fix into `release/vX.Y`, create a new tag on the release branch (e.g., `v2.17.1`).

### Automation

| Trigger Event                     | Automatic Behavior                                                | Workflow                    |
| --------------------------------- | ----------------------------------------------------------------- | --------------------------- |
| RC version release                | Creates the `release/vX.Y` branch                                 | `create-release-branch.yml` |
| Stable `.0` release               | Deletes old `release/v*` branches (excluding those with open PRs) | `create-release-branch.yml` |
| Fix PR merged into release branch | Cherry-picks to `fix/pr-N` and creates a PR to `v2`               | `cherry-pick-to-v2.yml`     |

### Hotfix Process

1. Create a fix branch based on `release/vX.Y`, commit the fix.
2. Submit a PR to `release/vX.Y` and merge it.
3. CI automatically cherry-picks to `v2`, resulting in a PR on v2 with the message `fix: cherry-pick #N from release/vX.Y to v2` — **just merge it directly**.
4. Create a new tag on the release branch (e.g., `v2.16.1`) to trigger the release.

### Handling Conflicts in Cherry-pick PRs

Manually resolve the conflicts, push the fix to the `fix/pr-N` branch, and then merge.

### When are Old Release Branches Deleted?

Automatically deleted when a new Stable `.0` is released. Before deletion, it checks for any unmerged open PRs; if any exist, the deletion is skipped (though these should be handled promptly).

### Notes

- The tag for Stable `.0` **must be created on the release branch**; otherwise, the changelog will not include hotfix fixes.
- Hotfix tags (`vX.Y.Z`, where Z>0) do not trigger cleanup; the release branch continues to exist.
- Automatically cherry-picked commits include `[skip changelog]`, so they will not appear repeatedly in the changelogs of subsequent versions.
