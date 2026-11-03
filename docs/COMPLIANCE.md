# Compliance

Status: **Draft**. A checklist of federal, state, and local requirements that are likely to apply, organized by the phase in which each one first matters. It's a starting point for legal review, not legal advice: confirm each item with a lawyer in the launch jurisdiction(s) before relying on it. Deadlines and court challenges change often, so check the current status of each law when you start a phase. See [PLAN.md](PLAN.md) and [THREAT_MODEL.md](THREAT_MODEL.md).

The project is US-based for now. Each instance operator is legally responsible for its own instance; the project provides templates and this checklist, and each operator still gets its own legal review (see [Phase 5](#phase-5-federation)).

## Phase 1: Foundation

Items that shape what's being built now, even before there are public users.

- [ ] **Abuse imagery reporting (18 U.S.C. § 2258A).** Providers that learn of child sexual abuse material must report it to NCMEC's CyberTipline and preserve it. The REPORT Act (2024) extended preservation to one year. Register with NCMEC as an electronic service provider before connecting the scanning service; PhotoDNA access usually depends on it. The "CSAM matches" row in the retention table must keep evidence for at least one year.
- [ ] **Data breach notification.** All 50 states have breach notification laws, with different deadlines and triggers. Write an incident response plan that covers who is notified, by whom, and how fast. Some states also require a written security program (e.g. Massachusetts 201 CMR 17, New York's SHIELD Act).
- [ ] **Accessibility.** Target WCAG 2.1 AA (2.2 AA where practical) and check it in CI. There's no final federal rule for private websites, but ADA Title III lawsuits are common, and county and municipal shelters are covered by DOJ's 2024 Title II rule, which requires WCAG 2.1 AA and may be passed on to the tools they use.
- [ ] **Biometrics.** Passkeys are fine: the biometric never leaves the device. Selfie-based identity checks would bring in Illinois BIPA and similar laws in Texas and Washington. Factor this in when choosing an identity verification provider.
- [ ] **Encryption export.** App Store Connect asks about export compliance. Standard cryptography in open-source software is generally exempt under the EAR, but the question still has to be answered.
- [ ] **Email (CAN-SPAM).** Sign-in, verification, and account notices are transactional. Any newsletter or fundraising email, and possibly saved-search alerts, needs an unsubscribe link and a postal address. If SMS is ever added, the TCPA applies.

## Phase 2: Adoption MVP

The first public users, user content, and messaging.

- [ ] **Terms of service, privacy policy, and community guidelines.** The community guidelines can reuse the list of unacceptable behavior in the project's [Code of Conduct](../CODE_OF_CONDUCT.md#examples), adapted for platform users. The privacy policy must match what the system actually does (FTC Act § 5): approximate locations, metadata stripping, what push notifications carry, and that deletion can't reach backups until they expire.
- [ ] **Minimum age (COPPA).** The amended COPPA rule took effect in 2025, with compliance due in April 2026. Simplest path: an age gate at sign-up with a minimum of 13, and 18+ for adoption applications, fostering, and sitting, since those involve agreements. See the state minors' laws in [Phase 3](#phase-3-communities), which may make 18+ for everyone the easier choice.
- [ ] **Copyright (DMCA § 512).** Register a designated agent with the Copyright Office ($6, renewed every 3 years), and publish a takedown and counter-notice process and a repeat-infringer policy. Scammers often reuse other people's pet photos, so expect to use it.
- [ ] **Non-consensual intimate imagery (TAKE IT DOWN Act, 2025).** Platforms built on user content must offer a removal request process and remove reported images within 48 hours. Platforms had to comply by May 2026, so this is needed at launch.
- [ ] **Section 230.** Protects the platform for content users post and for moderating in good faith. It doesn't cover federal criminal law, intellectual property, or the FOSTA/SESTA exceptions.
- [ ] **Law enforcement requests (Stored Communications Act).** Messages between users can only be disclosed under the conditions the SCA sets. Write a process for subpoenas, warrants, preservation requests, and emergency disclosures, and record each request in the audit log.
- [ ] **State privacy laws.** CCPA exempts nonprofits, but several state laws cover them, including Colorado, Oregon, Delaware, New Jersey, Maryland, and Minnesota. Most only apply above roughly 35,000–100,000 residents of the state, but it's cheaper to design for them now:
  - [ ] Requests to access, delete, correct, and export personal data
  - [ ] Honor Global Privacy Control signals (simple while the platform sells no data)
  - [ ] Opt-in consent for **precise location**, which these laws treat as sensitive data
  - [ ] Data minimization (Maryland's is the strictest)
- [ ] **Retention vs. public records.** Government shelters fall under public records laws, so their messages and application decisions on the platform may be records they have to keep. That conflicts with deleting applications 12 months after they close (see [Data Retention](PLAN.md#data-retention-to-be-decided)). One answer: an export shelters can keep in their own systems, instead of keeping the records here.
- [ ] **Listing rules.** Limit listings to household pets; ban wildlife and exotic animals (Lacey Act, Endangered Species Act, and state exotic pet laws). Show a notice that shelters and rescues are responsible for state adoption requirements such as spay/neuter, rabies vaccination, and bite-history disclosure.
- [ ] **Organization verification.** Some states license or register rescues (e.g. Colorado PACFA, Pennsylvania, New Jersey). Add a license check to verification where the state requires one. Rescues that import dogs must follow the CDC's dog import rules (August 2024).
- [ ] **App stores.** In-app account deletion, report and block on all user content (Apple guideline 1.2), Apple's privacy labels, and Google Play's data safety form. Already listed in [Phase 2](PLAN.md#phase-2-adoption-mvp).
- [ ] **Donations, if turned on in this phase.** See [Funding](#any-phase-funding-and-the-legal-entity).

## Phase 3: Communities

- [ ] **Minors and social media laws.** Groups, posts, and lost-pet alerts may make the platform a "social media" service under state laws on minors (e.g. Utah, Texas, Florida). Some states also require app stores and apps to handle age signals and parental consent. Several of these laws are being challenged in court. A minimum age of 18 avoids most of them.
- [ ] **Moderation records.** Keep moderation decisions and appeals in the audit log; they support Section 230 good-faith moderation and answer legal requests.
- [ ] **Legal entity.** Form it before the public app release, as [PLAN.md](PLAN.md#open-questions) says. See [Funding](#any-phase-funding-and-the-legal-entity).

## Phase 4: Fostering

- [ ] **Local pet limits and permits.** Foster homes can go over a city's per-household pet limit or need a kennel or foster permit. Show a notice when someone applies to foster, and let shelters record that a permit was checked.
- [ ] **Pet licensing.** Some cities require dogs and cats to be licensed. Leave it to the shelter, with a notice.

## Phase 5: Federation

- [ ] **Operator responsibility.** Each instance operator is the legal service provider for its own users: its own DMCA agent, NCMEC registration, terms, privacy policy, and law enforcement process. Ship templates for each, and say clearly in the operator guide what the operator is responsible for.
- [ ] **Deletion across instances.** Deletes are sent to other instances, but the platform can't guarantee that remote servers remove the data. Disclose this in the privacy policy.
- [ ] **Outside the US.** An instance in the EU or UK, or one that serves EU or UK users, brings in GDPR, the EU Digital Services Act, and the UK Online Safety Act. Decide whether the project supports non-US operators before federation opens.

## Phase 6: Verified Breeders

Prerequisite in [PLAN.md](PLAN.md#phase-6-verified-breeders): legal review of animal sale and breeder rules in the launch jurisdiction(s).

- [ ] **Animal Welfare Act (USDA APHIS).** Breeders with more than four breeding females who sell animals sight unseen, including online, generally need a USDA license. Check it during breeder verification.
- [ ] **State sale laws.** Pet lemon laws (about 20 states), minimum sale ages (usually 8 weeks), and health certificates for interstate sales.
- [ ] **Retail sale bans.** Several states (e.g. California, Illinois, Maryland, New York, Washington) and hundreds of cities ban or limit retail sales of commercially bred animals. Some local bans cover any sale, not just pet stores. Check whether breeder listings are allowed in each instance's area.
- [ ] **Reviews.** The FTC's 2024 rule on fake reviews and the Consumer Review Fairness Act apply to buyer reviews. Don't suppress negative reviews, and label any review that's been incentivized.
- [ ] **Sales tax.** If the platform ever processes payments for animal sales, marketplace-facilitator laws could make it responsible for collecting sales tax. Keep payments off the platform, as planned.

## Phase 7: Pet Sitting and Monetization

Prerequisites in [PLAN.md](PLAN.md#phase-7-pet-sitting-and-monetization): legal review of pet-sitting liability, and platform insurance.

- [ ] **Worker classification.** Keep the directory model (sitters set their own rates, schedules, and terms; no booking fee). Tests like California's AB5 look at control over the work.
- [ ] **Background checks (FCRA).** If sitter verification uses a background check company: a permissible purpose, a separate written disclosure and consent, and pre-adverse and adverse action notices. Some states and cities also have fair chance rules.
- [ ] **Sitter licensing and zoning.** Some states and cities license pet care businesses or in-home boarding, and zoning may limit home businesses. Require sitters to confirm they meet local rules.
- [ ] **Payments.** The platform never holds funds, which keeps it out of money transmitter licensing. Stripe Connect handles identity checks and 1099-K forms.

## Any phase: funding and the legal entity

- [ ] **Nonprofit status.** Incorporate, appoint a registered agent, and register in other states where the entity operates. Apply for 501(c)(3) status (Form 1023 or 1023-EZ), and file Form 990 or 990-N every year.
- [ ] **Donations.** Send written acknowledgments for gifts of $250 or more. Publish funding openly, as planned.
- [ ] **Charitable solicitation registration.** About 40 states require registration before asking their residents for donations. A donate button on a website can count, though some states only count a website alone in certain cases. Check this before turning on donations.
- [ ] **Insurance.** General liability, directors and officers, and cyber, as listed in [Phase 7](PLAN.md#phase-7-pet-sitting-and-monetization). Cyber insurance is worth having earlier, once there are real users.
