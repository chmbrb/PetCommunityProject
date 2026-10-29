# PetCommunityProject

A federated, community-run platform that connects people and organizations around household pets: adopting, fostering, and pet sitting, with local communities built around shelters and rescues.

> **Status:** planning (Phase 0). There's no code yet. See the [plan and roadmap](docs/PLAN.md).

## Vision

Finding a pet, a foster home, or a trustworthy sitter is scattered across shelter websites, social media groups, and classifieds, where scams and stale listings are common. This project aims to bring those needs together in one place that people can trust.

- **Connect people based on needs.** Search for pets by breed, location, and fee. Help shelters house and rehouse animals. Later, connect pet owners with trusted sitters.
- **Local communities, run by their members.** Shelters and other groups can set up local communities. Members can form and join groups, share posts about their pets, and get help finding a lost pet. Each group has an owner and trusted moderators, with fair rules for handing over or replacing inactive leaders.
- **One account, many roles.** A person might be an adopter, a foster volunteer, and a sitter. An organization might be a shelter or a rescue. Profiles carry a post history and a visible verification level.
- **Federated.** Built on ActivityPub, so independent instances (a county, a shelter network, a region) can run their own communities and connect with each other, without one company owning everything.
- **Trust and safety first.** Verification tiers, moderation tools, protection against scams and malware, and privacy by default (approximate locations, no photo metadata, personal details shared only when needed). Gaining and keeping trust is the whole point.
- **Lean and sustainable.** Cheap to host and simple to run. Funding only covers what instances need to keep running, published openly; it's not a profit scheme.
- **Easy to reach.** Native Android and iOS apps alongside the website, with notifications for applications, messages, and lost pets nearby.

## Roadmap at a glance

| Phase | Focus |
|-------|-------|
| 0 | Discovery: personas, domain model, threat model, architecture decisions |
| 1 | Foundation: deployment, accounts and sign-in, photo safety pipeline, backups, mobile app shell |
| 2 | Adoption MVP: verified shelters, listings (including Shelterluv/PetPoint imports), search, applications, messaging, mobile beta |
| 3 | Communities: groups, governance, lost-and-found posts, public app release |
| 4 | Fostering |
| 5 | Federation between instances |
| 6 | Verified breeders |
| 7 | Pet sitting and funding |

Details, decisions, and exit criteria for each phase are in [docs/PLAN.md](docs/PLAN.md).

## Planned stack

Go backend · PostgreSQL (with PostGIS) · Vue 3 + Vuetify web app, packaged for Android and iOS with Capacitor · Caddy · S3-compatible object storage · Docker Compose. The reasons for each choice are recorded in the [architecture decision records](docs/adr/README.md).

## Documentation

- [Plan and roadmap](docs/PLAN.md): decisions, trust tiers, governance, data retention, open questions
- [Personas](docs/PERSONAS.md): who the platform serves, and the bad actors it must withstand
- [Domain model](docs/DOMAIN_MODEL.md): core entities and how they map to ActivityPub
- [Threat model](docs/THREAT_MODEL.md): risks and mitigations by phase
- [Architecture decision records](docs/adr/README.md)

## Get involved

The project is currently led by one person, with the aim of bringing in others and growing through federation. If you work at a shelter or rescue, run community infrastructure, or want to contribute, please open an issue to start a conversation.

A contributor agreement is still to be decided (it's needed to publish the mobile apps in the app stores). Until then, please talk to us before submitting code.

## License

[GNU Affero General Public License v3.0](LICENSE). If you run a modified version of this software as a service, you must make its source available to your users.
