# AGENTS.md

Instructions for AI coding agents working in this repo. Read `README.md` for the overview, setup, commands and layout. This file does not repeat them.

## Definition of done

A change is done only when the **Checks** command in `README.md` passes. If you can't run it, say so; don't claim it passed. Add or update tests for any behavior you change.

## Conventions

- Prefer hyphens (`-`) in folder names; use underscores (`_`) when hyphens aren't allowed. Go package folders use short lowercase names without separators.
- Use Conventional Commit subjects (`feat:`, `fix:`, `docs:`, `chore:` …).
- Follow Effective Go; `gofmt` and `go vet` must be clean. Prefer the standard library; a new module needs a reason recorded under "Decisions".
- Packages live under `internal/`; `cmd/` holds only small `main` packages.
- Pages are server-rendered `html/template` with htmx for interaction; no JavaScript build step. User-facing text comes in Indonesian and English.
- Each gateway sits behind one Go interface, so projects never depend on a gateway directly.

## Guardrails

- **Own projects only.** Dahlunas takes payments for Ardeman's own projects. Collecting money for other businesses needs a Bank Indonesia payment licence (PJP); don't add that without the user deciding it.
- **Secrets:** gateway keys, webhook tokens and signing private keys come from environment variables (Secret Manager on Cloud Run). Never commit them, print them or write them into logs.
- **Webhooks:** check every webhook's token or signature before acting, and make handling idempotent: the same payment never sends two codes.
- **Buyer data:** keep only what delivery needs (email, order id, product). Each project's privacy policy must say Dahlunas handles it.
- **Children's apps:** apps such as Coba Lagi never call Dahlunas. They only check codes offline; the checkout is reached from the project's website.

## Keeping docs current

- When you change a command, the layout or the setup, update `README.md`. When you change a convention or rule, update this file.
- `CLAUDE.md` and `GEMINI.md` must contain only the import of this file.

## Decisions

Record architectural choices here as one line each: date, decision, reason.

- 2026-10-26: Checkout uses Firestore Native mode with conditional REST writes and separate sandbox/production collections, so Cloud Run instances share durable orders without a database driver dependency. Resend delivers codes behind a mail interface, using a durable lease and provider idempotency keys; automatic retries stop after 23 hours, before Resend's 24-hour deduplication window expires, for manual review of ambiguous delivery.

- 2026-10-24: Unlock code v1 uses a fixed binary payload and a full Ed25519 signature in unpadded Base32 (148 characters including prefix), with per-project key IDs for rotation; copy/paste and QR scanning preserve offline public-key verification without truncating signatures for a short manual code. Contract: `docs/code-format.md`.
- 2026-10-24: Go with htmx on Cloud Run behind Firebase Hosting (the user's choice of a light, fast server on Firebase): Go starts fast in a small container, has Ed25519 in the standard library, and Xendit has an official Go SDK (Midtrans, chosen instead on 2026-10-26, has one too). Region `asia-southeast2` (Jakarta), close to the first buyers. Chosen over TypeScript on Firebase Functions (the user's choice): leaner and faster, few dependencies for money code, at the cost of a second deploy tool; TypeScript 7's Go compiler speeds up builds only, not the Node runtime.
- 2026-10-24: Xendit first (replaced on 2026-10-26, below).
- 2026-10-25: A signing key is provisioned as its 32-byte Ed25519 seed in hex, one environment variable per project and key id (`DAHLUNAS_KEY_<project>_<key id>`), so keys rotate per project without code changes. `cmd/mint new-key` writes a new seed to a 0600 file (refusing to overwrite) and prints only the public key, so the secret never reaches the terminal's scrollback.
- 2026-10-26: Midtrans first, instead of Xendit: Xendit's signup offers only legal entities (PT, CV, Yayasan and others), and the user has no company; Midtrans takes an individual ("Registrasi perorangan", category Software) with QRIS, GoPay, virtual accounts and cards. Its website rules (products with rupiah prices, terms, refunds, contact, checkout not sent to another site) put the shop on Dahlunas's own domain, which each project's website links to. Lemon Squeezy is planned second, for buyers abroad.
