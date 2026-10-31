# Contributing

Thanks for your interest in PetCommunityProject. It's early (Phase 1), led by one person, and help is welcome: code, documentation, design, testing, and above all experience from shelters, rescues, and fosters.

## Before you start

- **Open an issue first** for anything beyond a small fix, so we can agree on the approach before you spend time on it. The [plan and roadmap](docs/PLAN.md) and the [architecture decision records](docs/adr/README.md) explain what's planned and why.
- **A contributor agreement is still being decided.** It's needed to publish the mobile apps in the app stores. Until it is, please talk to us in an issue before submitting code. Documentation fixes are welcome any time.
- **Security problems** go through [SECURITY.md](SECURITY.md), never a public issue or pull request.

## Setting up

[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) covers prerequisites, running everything locally, the repository layout, and the project's conventions. In short:

```sh
make dev-up && make migrate
make run      # API
make worker   # background jobs
make web      # web app on http://localhost:5173
```

## Making a change

1. Fork the repository (or, if you're a collaborator, create a branch) and make your change on a branch.
2. Follow the [conventions](docs/DEVELOPMENT.md#conventions). Among them: a `SPDX-License-Identifier: AGPL-3.0-or-later` header on new source files, new database migrations instead of edits to merged ones, and SQL through sqlc only.
3. Add or update tests, and update the documentation when behaviour or setup changes.
4. Check locally:

   ```sh
   make lint
   make test
   make test-integration   # Go tests including database tests; needs make dev-up
   ```

5. Open a pull request against `main` that explains what changed and why, and links the issue.

## What happens to your pull request

- **CI runs when a maintainer approves it.** Workflows from outside contributors wait for **Approve and run**, so nobody's code runs on the project's runners unread. CI then runs only the checks your change affects; for example, documentation-only changes skip the build and tests. The secret scan and CodeQL always run.
- **Everything must pass:** the `CI result` check and the CodeQL checks are required.
- **Your branch must be up to date with `main`.** If `main` has moved on, the pull request shows **Update branch**. Leave **Allow edits by maintainers** ticked and we can do this for you.
- **Review conversations must be resolved** before merging.
- **Pull requests are squash-merged**: your commits become a single commit on `main`, with you as its author. The pull request title and description become its message, so make them clear.

## Commit signing is optional

`main` only accepts signed commits, but you don't need to sign yours: when a pull request is squash-merged, GitHub writes the commit on `main` and signs it. If you'd like to sign your commits anyway, [docs/REPOSITORY_SETUP.md](docs/REPOSITORY_SETUP.md#signing-commits-and-tags) shows how.

## License

The project is licensed under the [GNU Affero General Public License v3.0 or later](LICENSE). By contributing, you agree that your contributions are licensed under the same terms.
