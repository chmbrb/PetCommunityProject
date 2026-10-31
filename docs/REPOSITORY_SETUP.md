# Repository setup

How the GitHub repository and the maintainers' accounts are configured to protect the code and the release pipeline (THREAT_MODEL.md I2, O1, O2). Most of this lives in GitHub's settings, not in files, so this page is the record of what should be set. Use it to check the settings, or to set up a fork or a new maintainer.

What CI checks, and when, is described in [DEVELOPMENT.md](DEVELOPMENT.md#what-ci-checks-and-when).

## Maintainer accounts

A hijacked maintainer account defeats every repository setting, so this comes first.

- [ ] Two-factor sign-in with a **passkey or security key**; remove SMS as a fallback. Keep the recovery codes offline.
- [ ] Review **Settings → Applications**, **Developer settings** and **SSH and GPG keys**, and remove anything unused. Prefer fine-grained personal access tokens limited to this repository, with an expiry date.
- [ ] Sign commits and tags (next section), and turn on **Flag unsigned commits as unverified** (vigilant mode) under **SSH and GPG keys**.

## Signing commits and tags

GitHub verifies signatures against the signing keys uploaded to the author's account. Nothing needs to be in the repository.

1. Create a dedicated signing key (choose a passphrase):

   ```sh
   ssh-keygen -t ed25519 -C "commit signing" -f ~/.ssh/github_signing
   ssh-add ~/.ssh/github_signing   # the agent then asks for the passphrase once per session
   ```

2. On GitHub, go to **Settings → SSH and GPG keys → New SSH key**, set **Key type** to **Signing Key** (it defaults to Authentication, which doesn't verify commits), and paste `~/.ssh/github_signing.pub`.

3. Configure Git. If you use several Git identities, apply it only to this account's repositories. In `~/.gitconfig`:

   ```ini
   [includeIf "gitdir:~/proj/github.com/<account>/"]
   	path = ~/.config/git/<account>.gitconfig
   ```

   In `~/.config/git/<account>.gitconfig`:

   ```ini
   [gpg]
   	format = ssh
   [user]
   	signingkey = ~/.ssh/github_signing.pub
   [commit]
   	gpgsign = true
   [tag]
   	gpgsign = true
   [gpg "ssh"]
   	allowedSignersFile = ~/.config/git/allowed_signers
   ```

4. List the keys you trust in `~/.config/git/allowed_signers`, one per line, as your commit email followed by the public key:

   ```
   <commit email> namespaces="git" ssh-ed25519 AAAA...
   ```

   The commit email must be one GitHub has verified for your account; the `@users.noreply.github.com` address counts.

5. Check it: after your next commit, `git log --show-signature -1` should report a good signature, and the pushed commit shows **Verified** on GitHub. `git tag -a v1.2.3 -m "..."` now creates a signed tag.

**Why the allowed signers file is personal, not in the repository.** Git doesn't read any such file by default; `gpg.ssh.allowedSignersFile` in each person's own configuration names it, and GitHub ignores it entirely. It decides which keys Git reports as trusted, so a copy in the repository would change with whatever is checked out: a pull request branch could add its own key and its commits would then show as validly signed. A shared file becomes worthwhile with several maintainers, or for a release workflow that only builds tags signed by a maintainer. If it's added then, keep it at `.github/allowed_signers`, require review of changes to it (CODEOWNERS), and use `valid-after`/`valid-before` on each entry instead of deleting retired keys, so their old signatures still verify.

## Settings → General

- [ ] **Features:** turn off what isn't used (Wiki, Projects, Discussions); each is something else to watch for spam.
- [ ] **Pull Requests:** allow **squash merging** only (untick merge commits and rebase merging), with the default message **Pull request title and description**.
- [ ] **Automatically delete head branches:** on.

Squash merging is required by the `main` ruleset (linear history). It's also what lets contributors skip signing: GitHub writes and signs the squashed commit on `main`. Rebase merging would break that, because GitHub can't sign rebased commits.

## Settings → Advanced Security

All of these are free for public repositories.

- [ ] **Dependency graph:** required by the CI dependency review job, which fails without it.
- [ ] **Dependabot alerts** and **Dependabot security updates.**
- [ ] **Secret scanning** with **push protection:** rejects a push containing a known token format before it reaches GitHub; gitleaks in CI covers the rest.
- [ ] **Private vulnerability reporting:** the reporting channel named in [SECURITY.md](../SECURITY.md).
- [ ] **Code scanning:** leave it on the CodeQL workflow (`.github/workflows/codeql.yml`). Don't also turn on "default setup"; the two conflict.

## Settings → Actions → General

- [ ] **Actions permissions:** allow GitHub's own actions and selected others (currently `pnpm/action-setup` and `golangci/golangci-lint-action`), and tick **Require actions to be pinned to a full-length commit SHA**. The workflows already pin every action.
- [ ] **Fork pull request workflows:** **Require approval for all external contributors**, so a stranger's pull request can't run code on the runners until a maintainer has read it.
- [ ] **Workflow permissions:** read repository contents only, and untick **Allow GitHub Actions to create and approve pull requests**. The workflows request any further permissions per job.
- [ ] **Artifact and log retention:** about 30 days.

## Settings → Rules → Rulesets

Use rulesets, not the classic branch protection rules; if a classic rule exists for `main`, delete it once the ruleset is active.

### `main` (branch ruleset)

| Setting | Value |
|---|---|
| Enforcement status | Active |
| Bypass list | Empty. In an emergency, disable the ruleset temporarily; that's visible and deliberate. |
| Target branches | Include default branch |
| Restrict creations, Restrict updates | Off |
| Restrict deletions | **On** |
| Require linear history | **On** (squash merges) |
| Require merge queue, Require deployments to succeed | Off |
| Require signed commits | **On** |
| Require a pull request before merging | **On**: required approvals 0 while there's one maintainer (nobody can approve their own pull request); dismiss stale approvals on; review from Code Owners off; approval of the most recent push off; **require conversation resolution on**; allowed merge methods **Squash** |
| Require status checks to pass | **On**: **require branches to be up to date**; required checks (source GitHub Actions) `CI result`, `Analyze (go)`, `Analyze (javascript-typescript)`, `Analyze (actions)` |
| Block force pushes | **On** |
| Require code scanning results | **On**: tool CodeQL, alerts **Errors**, security alerts **High or higher** |

About the required checks:

- Require only `CI result` from `ci.yml`, never the individual jobs. CI skips jobs whose area didn't change, and a required check that never runs blocks the merge forever. `CI result` always runs and fails if any job failed.
- CodeQL runs on every change, including documentation-only ones, for the same reason; keep `paths-ignore` out of `codeql.yml`.
- The check picker only suggests names it has seen recently, so add them after a pull request has run both workflows.
- When there's a second maintainer: raise required approvals to 1, and turn on review from Code Owners and approval of the most recent push.

### `release tags` (tag ruleset)

| Setting | Value |
|---|---|
| Enforcement status | Active |
| Bypass list | Empty |
| Target tags | Include by pattern `v*` |
| Rules | Restrict updates, Restrict deletions, Block force pushes, Require signed commits |

A published release tag then always points at the code it was created for, and was signed by a maintainer.

## Dependabot

Configured in `.github/dependabot.yml`. Notes:

- **Majors that are upgraded deliberately** are ignored: Node.js (LTS releases only), `@types/node`, Go, and Postgres/PostGIS (see DEVELOPMENT.md, "Upgrading runtimes").
- **Chainguard images** (MinIO and its client, used only by the development and local stacks) are grouped into one weekly pull request, because Chainguard rebuilds them almost daily. They're pinned by digest alone, which has no publication date, so Dependabot warns that it couldn't apply a cooldown. That's expected.

## Effect on contributors

None of this requires anything of contributors beyond a normal pull request:

- They don't need to sign commits: the signed-commits rule applies to `main`, where GitHub writes the signed squash commit.
- The allowed signers file and vigilant mode only affect the maintainer's own machine and commits.
- Their first workflow runs wait for a maintainer's **Approve and run**.
- If `main` moves on, the pull request needs **Update branch**; maintainers can do this when **Allow edits by maintainers** is ticked (the default).
- Review conversations must be resolved before merging.

[CONTRIBUTING.md](../CONTRIBUTING.md) tells contributors this.

## Later

- **Production environment** once there are deployment workflows: deployable only from `main`, with a required reviewer, and holding the deployment secrets so workflows on other branches can't read them.
- **OIDC** for container registries and hosting instead of long-lived tokens stored as secrets.
- **Signed images with build provenance** (cosign, `actions/attest-build-provenance`) when images are published, so servers can verify an image came from this repository's CI.
- **CODEOWNERS** for `.github/`, `Dockerfile`, `compose*.yaml` and `deploy/` once there are collaborators.
- **OpenSSF Scorecard** workflow, to audit these settings weekly and catch drift.
- **A shared allowed signers file**, with a second maintainer or signed-release verification (see above).
