# Security policy

PetCommunityProject handles personal information, private messages, and precise locations, and connects people with animals in their care, so security problems matter. Thank you for helping keep its users safe.

## Reporting a vulnerability

**Please don't open a public issue, pull request, or discussion for a security problem.**

Report it privately through GitHub: on the repository's **Security** tab, choose **Report a vulnerability** ([direct link](https://github.com/chmbrb/PetCommunityProject/security/advisories/new)). Only the maintainers can see the report, and we can discuss it and prepare a fix there.

Helpful details:

- what's affected (component, endpoint, file, or version/commit);
- steps to reproduce, or a proof of concept;
- the impact as you understand it;
- whether you'd like to be credited, and how.

## What to expect

The project is maintained by one person, so these are good-faith targets, not guarantees:

- an acknowledgement within **7 days**;
- an initial assessment within **14 days**;
- updates as the fix progresses, and a coordinated disclosure date agreed with you, normally within **90 days** of the report.

Fixed issues are published as GitHub security advisories, crediting the reporter unless they'd rather not be named.

## Supported versions

The project is in early development (Phase 1) and has no releases yet. Only the latest commit on `main` is supported. Once releases begin, this section will list which ones receive security fixes.

## Scope

In scope:

- the code in this repository: the Go backend, the web and mobile apps, database migrations;
- the deployment configuration it ships: `Dockerfile`, `compose*.yaml`, `deploy/`, and the CI workflows;
- weaknesses in the design documented in `docs/`, such as the threat model or federation behaviour.

Out of scope:

- instances run by other people. Report problems with a specific instance's operation to its operator; flaws in this project's code are in scope wherever they're found;
- volumetric denial of service, spam, and social engineering of maintainers or users;
- vulnerabilities in third-party dependencies that are already publicly known (Dependabot and the CI scans track these), unless this project uses the dependency in an especially exposed way;
- findings from automated scanners without a demonstrated impact.

## Testing safely

Test against your own local copy: `make local-up` runs the full production stack on your machine (see [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)). Don't test against instances you don't operate, don't access or change other people's data, and stop as soon as you've shown the problem. We won't pursue or support legal action against anyone who reports in good faith and follows this policy.
