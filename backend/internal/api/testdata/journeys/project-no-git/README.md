# project-no-git

Plain directory tree without any `.git` directory, for the Codeflow 3.0
journey E2E tests (plan §21.2). It exists to verify the copy-based shadow
path of `runworkspace`: when a workspace root is not a git repository, the
shadow must be built from a controlled directory copy plus a file manifest
(content hashes), not from a git worktree or base commit.

Guarantees:

- no `.git` directory anywhere in this tree, and none may ever be added;
- only small plain-text files; no symlinks/junctions, no binaries, nothing
  larger than 100KB;
- copy this fixture into a temp dir before mutating it; never run
  destructive cases against this source tree.
