# Incident response

Status: **Draft**. What to do, in order, when something goes wrong on an instance. Like [COMPLIANCE.md](COMPLIANCE.md), it's a starting point for legal review, not legal advice: the legal steps below need a lawyer's confirmation before they're relied on. Items marked **None yet** are filled in once more people take part in the project (see [Roles and contacts](#roles-and-contacts)).

It's written for the operator of one instance, which today is the project lead running the main instance. Each operator is responsible for its own instance and its own users (COMPLIANCE.md, Phase 5). Security problems in the project's code follow [SECURITY.md](../SECURITY.md) instead; the operator side of a vulnerability is in [Vulnerability in the code](#vulnerability-in-the-code).

## Roles and contacts

| Role | Who | Notes |
|---|---|---|
| Incident lead | The instance operator: the project lead | Runs every step below, and files the reports the law requires |
| Backup | **None yet** | Someone who can act when the operator can't. Until there is one, a serious incident waits for one person |
| Legal counsel | **None yet** | Needed for any data breach, and before a first CyberTipline report; found when one of those comes up |
| NCMEC CyberTipline | The instance's electronic service provider registration | Register before connecting a scanning service (COMPLIANCE.md, Phase 1) |
| Hosting, email, and storage providers | Their abuse and support contacts | Keep the account details offline, with the keys |

**For now, it's one person.** The project lead runs the project and the main instance alone, so every role above falls to them. Update this table once more people take part in the project: a backup who can act on an incident, legal counsel, and their contact details. Do it before inviting outside contributors or operators.

Keep this table, filled in, with the offline copy of the instance's keys, not only in the repository.

## Any incident: first steps

1. **Start an incident log.** A dated, private file kept outside the instance (on an encrypted disk, never in this repository or an issue): what happened, when you learned of it, and each step you take, with times. Breach laws and reports to NCMEC ask when you knew.
2. **Judge how serious it is:**
   - **Critical:** abuse imagery; someone else has access to the server, the database, a backup, or a secret; personal data has left the instance.
   - **High:** a verified organization's account is taken over; a scam or impersonation wave; a vulnerability that's being exploited.
   - **Normal:** one ordinary account taken over; a vulnerability report with no sign of use.
3. **Keep evidence before changing anything.** Save the logs first, because restarting or removing containers loses them:
   ```sh
   docker compose logs --timestamps --no-color > incident-$(date -u +%Y%m%dT%H%M%SZ).log
   ```
   Put the file with the incident log. For a server compromise, take a snapshot of the server's disk through the hosting provider before rebuilding.
4. **Contain it,** using the playbook below.
5. **Decide who must be told,** and by when (each playbook says). If in doubt, ask legal counsel early: some deadlines start when you learned of the incident.
6. **Review it afterwards** (see [After an incident](#after-an-incident)).

What you'll have to go on today: the audit log (every case action, suspension, and session revocation), the database, and the containers' own logs. Request logs with IP addresses aren't kept: Caddy doesn't write an access log, uploads don't record an IP address (ADR 0022), and container logs are capped at 50 MB per container (Docker keeps up to five such files; Podman only the latest), so on a busy instance they reach back days, not weeks. Save them early.

## Operator commands

There are no moderation tools in the app yet (they come in Phase 2). Until then, these commands run on the server, like `petcommunity cases`. Every change needs `--operator` (your name) and `--reason`, and goes in the audit log:

```sh
docker compose run --rm -T worker accounts find @handle          # or an email address; prints the account's ID
docker compose run --rm -T worker accounts show ACCOUNT_ID       # status, sign-in methods, passkeys, signed-in devices
docker compose run --rm -T worker accounts suspend --operator "Your Name" --reason "suspected takeover" ACCOUNT_ID
docker compose run --rm -T worker accounts reinstate --operator "Your Name" --reason "owner confirmed" ACCOUNT_ID
docker compose run --rm -T worker accounts remove-passkey --operator "Your Name" --reason "added by attacker" ACCOUNT_ID PASSKEY_ID
docker compose run --rm -T worker signups off --operator "Your Name" --reason "scam wave"
docker compose run --rm -T worker signups on --operator "Your Name" --reason "wave over"
docker compose run --rm -T worker password-sign-in off --operator "Your Name" --reason "takeover wave"
docker compose run --rm -T worker password-sign-in on --operator "Your Name" --reason "wave over"
```

- **`suspend`** stops the account signing in and signs it out everywhere at once.
- **`reinstate`** lifts a suspension. It refuses while an abuse imagery case is open or reported for the account: close the case first.
- **`remove-passkey`** sends no email (suspended accounts get none), so tell the owner yourself. It won't remove the only passkey of an account whose email isn't confirmed, since it couldn't sign in without it.
- **`signups off`** stops new accounts at once; existing accounts keep working. `signups` on its own says whether sign-ups are open.
- **`password-sign-in off`** pauses password sign-ins at once. Email codes and passkeys keep working, and the sign-in page hides the password field. `password-sign-in` on its own says whether it's on.

For anything else, such as an account's audit history, connect to the database from the server:

```sh
docker compose exec db psql -U petcommunity petcommunity
```

Abuse imagery cases have their own command, `petcommunity cases` ([DEVELOPMENT.md](DEVELOPMENT.md), "Abuse imagery cases"). Restoring the database is in DEVELOPMENT.md, "Restoring from a backup".

## Playbooks

### Abuse imagery

**How you'll know:** an email to `PC_ABUSE_ALERT_EMAIL` ("Action needed: abuse imagery case opened"), with the case ID and the next steps. By the time it arrives, the upload's account is already suspended and signed out (ADR 0022). It goes to `abuse@` the instance's domain unless set otherwise: at launch, a group address for everyone who handles reports. In development it goes to Mailpit. The worker also logs `upload matched known abuse imagery` at error level.

An alert can be delayed or lost (the email provider is down, or the group's settings changed), so check for open cases once a week too:

```sh
docker compose run --rm -T worker cases list
```

A person may also report an image that no scanner matched.

**Steps, for a scanner match:**

1. `cases show CASE_ID`, and note in the incident log when you learned of it.
2. **Don't view the image.** Nobody needs to: the match, the hash, and the case's details are what the report needs. The law asks that access to preserved material be limited to what's necessary (18 U.S.C. § 2258A(h)(3)).
3. **Report it to NCMEC's CyberTipline as soon as reasonably possible.** That's the legal standard (§ 2258A(a)(1)); the project's target is **within 24 hours** of learning of it. Attach the file from `cases export` (handled as DEVELOPMENT.md describes: never opened, deleted after upload), and include the details from `cases show`.
4. **Record the report:** `cases report --reference REPORT_ID`. Evidence is then kept for at least a year after the report, as § 2258A(h) requires, and the database refuses anything shorter.
5. **Don't tell the uploader** about the report, and don't delete their account. The suspension stays. Whether to ban them is a separate decision, recorded in the audit log.
6. If law enforcement contacts you about the case, see [Requests from law enforcement](#requests-from-law-enforcement).
7. When nothing more is needed, `cases close`. The worker purges the evidence 30 days after closing, and never before the preservation date.

**If it was a false match** (a hash list mistake, confirmed by the hash list's provider rather than by looking at the image): `cases close --reason "..."`, then lift the suspension with `accounts reinstate`.

**If a person reports an image that no scanner matched:** suspend the uploader's account (`accounts suspend`), but don't download, view, or forward the image, and don't delete it. There's no way yet to turn such an upload into a case with preserved evidence. Ask legal counsel, and NCMEC, how to report it with what you have, and record what you did in the incident log. Building that path is a follow-up below.

### Account takeover

**How you'll know:** the account's owner tells you, often after an email they didn't expect: a code for signing in with their password on a new device, or a notice that a passkey, their password, or two-step sign-in changed. Or someone reports strange posts or messages from the account.

1. **Check that it's really the owner asking,** through a channel the attacker can't control: not by replying to the account's email address if it may have changed, and not by trusting a message sent from the account. For a verified organization, use the contact details from its verification.
2. **Suspend the account and sign it out everywhere** (`accounts suspend`). This stops the attacker at once.
3. **See what changed:** `accounts show ACCOUNT_ID` lists the account's passkeys, with when each was added and last used, and how else it signs in. The audit log shows what was done to it (in `psql`):
   ```sql
   SELECT occurred_at, action, details FROM audit_log WHERE target_id = 'ACCOUNT_ID' ORDER BY occurred_at;
   ```
   Note any passkey the owner doesn't recognize; it has to go before the account is reinstated.
4. **Give the account back:** remove each passkey the owner doesn't recognize (`accounts remove-passkey`), then `accounts reinstate`. The owner signs in by email code, sets a new password if they use one (which signs out every other device), and checks their passkeys and two-step sign-in in Settings. Encourage two-step sign-in.
5. **Undo what the attacker did** with the account. Today that's limited to its profile and photo; once listings and messages exist (Phase 2), check those, and warn anyone the attacker contacted.
6. **If it's a verified organization** (Phase 2), treat it as High: check its listings and applications too, and post a notice if adopters may have been contacted.

Most takeovers start with a reused password. If several accounts are taken over at once, treat it as a [Scam or impersonation wave](#scam-or-impersonation-wave), and consider pausing password sign-ins (`password-sign-in off`) until it's over. Everyone can still sign in with an emailed code.

### Data breach or server compromise

**How you'll know:** a hosting provider's alert, unexpected changes on the server, a secret or backup found where it shouldn't be, or someone telling you they have your data.

1. **Keep evidence:** logs first (see above), then a snapshot of the server's disk through the hosting provider.
2. **Cut off access.** Take the instance offline if someone may still be inside (`docker compose down`, or block traffic at the hosting provider). Then change every credential that may have been seen, rebuilding the server from a clean install if it was compromised. Today that's done by hand: replace the file in `secrets/` with a new value, change it at its source (the hosting provider's storage keys, the email provider's SMTP password, or `ALTER ROLE petcommunity PASSWORD ...` in the database), and restart the services that use it.
3. **Rotate the encryption keys,** following DEVELOPMENT.md, "Rotating encryption keys" (ADR 0025). Don't just replace them: that would make what they encrypt unreadable. Rotating protects data from now on; it can't take back copies already made, so:
   - `PC_SECRET_KEY` (`secret_key`): encrypts actors' signing keys and abuse imagery evidence. If it leaked together with the database or the evidence bucket, treat the evidence and the keys as exposed, and delete old versions of evidence files if the bucket allows it.
   - `mfa_key`: encrypts authenticator secrets. If it leaked together with the database, rotate it and also ask every account with two-step sign-in to remove its authenticator and set it up again: the leaked secrets still generate valid codes.
   - `backup_key` can't be rotated in place: anyone holding it and a copy of the backups can read everything in them. New backups can use a new key, but each old one needs the key it was made with.
4. **Find out what was exposed:** which data (THREAT_MODEL.md, "Assets": adopter details and precise locations are the most sensitive), whose, and from when to when. If the database was changed, restore it to a point before the compromise (DEVELOPMENT.md, "Restoring from a backup") once the server is clean.
5. **Notify, with legal counsel.** If personal information was exposed, state breach laws apply according to **where the affected people live**, not where the instance runs. Deadlines differ by state: the shortest give 30 days to notify the people affected (Washington, Colorado, Florida, and Maine, among others), and several also require notice to the state's attorney general once 500 or more of its residents are affected. Some count from when the breach was discovered, others from when it was confirmed, which is why the incident log records both. Notices go from the operator. Also tell the hosting provider, and, if passwords may have leaked, every affected account.
6. **If the cause was a flaw in the project's code,** report it as SECURITY.md describes, so other instances are fixed too.

### Scam or impersonation wave

**How you'll know:** several reports of the same kind at once, such as accounts posing as a real shelter, or messages asking for deposits outside the platform (THREAT_MODEL.md A2, A3; most of this arrives with listings and messages in Phase 2).

1. **Find the pattern:** the accounts involved, when they signed up, and anything they share (handles, display names, email domains, profile photos). The media pipeline flags a photo that's a near copy of someone else's, which catches stolen pet and shelter photos.
2. **Suspend the accounts** (`accounts suspend`), with the same reason in each audit event, so the wave can be found again.
3. **If new accounts keep arriving, stop sign-ups for a while** (`signups off`), and turn them back on (`signups on`) once it's over.
4. **Tell the people affected:** anyone who was contacted, and, for an impersonated shelter, the shelter itself, so it can warn its own followers.
5. If money was taken, point the people affected to their bank and to the FTC (reportfraud.ftc.gov), and keep the details in case law enforcement asks.

### Vulnerability in the code

When a report arrives through SECURITY.md, the maintainer follows its timeline. As an operator: if it's being exploited, treat it as a [Data breach or server compromise](#data-breach-or-server-compromise); if not, update the instance as soon as the fix is released, and check the audit log and logs for signs that it was used before then.

### Requests from law enforcement

Ask legal counsel before handing anything over. Record every request, and what was provided, in the incident log. Preserve what a request names straight away, even before deciding whether to hand it over, so nothing is purged in the meantime: for an abuse imagery case, don't close it. A written process for these requests is part of COMPLIANCE.md, Phase 2.

## After an incident

Within two weeks, write a short review in the incident log: what happened, how it was noticed, how long each step took, and what would have caught it sooner or limited it. Turn each lesson into an issue or a change to this runbook or THREAT_MODEL.md. For a security incident, don't put details in a public issue until it's fixed.

## Gaps found while writing this

Known weak points, in rough order of importance. Each one is also tracked where it belongs. (Alerting the operator about a match, capping container logs, and pausing password sign-ins are done.)

- **There's no backup person**, so a serious incident depends on one person being reachable. It stays that way while the project lead runs everything alone (see [Roles and contacts](#roles-and-contacts)).
- **A reported image that no scanner matched can't be turned into a case** with preserved evidence.
- **No moderation tools yet:** the `accounts` and `signups` commands cover suspending, reinstating, removing a passkey, and stopping sign-ups, but there's no way yet to review reports or act on content.
- **Rotating the other secrets isn't scripted** (database password, storage and SMTP credentials), and `backup_key` can't be rotated in place. `PC_SECRET_KEY` and `mfa_key` can (ADR 0025).
- **Little to investigate with:** no request logs, no upload IP addresses (ADR 0022), and container logs capped at 50 MB per container.
- **No templates** for breach notices or for messages to affected users.
