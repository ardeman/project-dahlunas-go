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

- 2026-10-26: The sandbox gets its own Hosting site, `dah-lunas-sandbox.web.app`, rewriting to `dahlunas-development` in the same Firebase project; a memorable address, and sandbox tests now pass through Hosting like production. The checkout CSRF cookie is named `__session`, the only cookie Hosting forwards. Separate Firebase projects per environment can wait until others get dev access.

- 2026-10-26: Pages show one language at a time, chosen by `?lang=` carried on internal links, else `Accept-Language`, else Indonesian; Firebase Hosting forwards no cookie but `__session` to Cloud Run, so a language cookie would never arrive. Emails stay bilingual.

- 2026-10-26: GitHub Actions deploys both Cloud Run services after the checks pass on `main`, development automatically and production after the owner's approval in a GitHub environment, signing in through Workload Identity Federation as a narrow deployer account; a manual deploy was forgotten once, and no long-lived Google key may sit in GitHub. Pushes that change only docs, Hosting or CI files skip both deploys. The approval is its own job, which a newer push cancels while it waits, so only the latest build is offered; the deploy job after it never cancels. Firebase Hosting stays a manual deploy, since its rewrite rarely changes.

- 2026-10-26: Pages look like an Indonesian receipt: white paper, true black, one stamp red, self-hosted Archivo (OFL, no third-party font requests that would expose buyer IPs), dashed rules only between line items, and a single "LUNAS" stamp as the memorable element; avoids generic generated-site tells (gradient washes, shadowed rounded cards, uppercase eyebrows).

- 2026-10-26: Email delivery is retried by a Cloud Scheduler call to a bearer-token endpoint that queries a top-level `deliveryPending` Firestore field, with fixed log lines for log-based alerts; Cloud Run has no background worker, and gateway notifications alone may stop before a stuck email is sent.

- 2026-10-26: The delivery sweep also reconciles unpaid orders through an optional gateway `Checker` (Midtrans status API), queried by a top-level `awaitingPayment` field after 10 minutes and abandoned after 48 hours; a lost notification must not leave a paid order unfulfilled, and Lemon Squeezy has no lookup by our order id.

- 2026-10-26: Checkout uses a bounded per-instance token bucket (burst 10, refill 30/minute) plus server-verified Turnstile required in production; avoids retaining buyer IPs or trusting proxy headers, while leaving payment webhooks unaffected. Fleet-wide edge quotas remain separate.

- 2026-10-26: Lemon Squeezy uses optional USD catalog entries, signed webhooks plus authoritative order/item reads, and a per-order random payment proof; one shared production flag separates both gateways' test/live orders, while legacy records remain Midtrans/IDR. Prices are integer minor units and tax-exclusive; the default catalog stays Midtrans-only.

- 2026-10-26: Owned projects use a validated JSON catalog with per-project signing key IDs and bilingual delivery copy; each order snapshots its product and wording so configuration changes cannot alter pending fulfillment. Secrets remain in environment variables.

- 2026-10-26: Checkout uses Firestore Native mode with conditional REST writes and separate sandbox/production collections, so Cloud Run instances share durable orders without a database driver dependency. Resend delivers codes behind a mail interface, using a durable lease and provider idempotency keys; automatic retries stop after 23 hours, before Resend's 24-hour deduplication window expires, for manual review of ambiguous delivery.

- 2026-10-24: Unlock code v1 uses a fixed binary payload and a full Ed25519 signature in unpadded Base32 (148 characters including prefix), with per-project key IDs for rotation; copy/paste and QR scanning preserve offline public-key verification without truncating signatures for a short manual code. Contract: `docs/code-format.md`.
- 2026-10-24: Go with htmx on Cloud Run behind Firebase Hosting (the user's choice of a light, fast server on Firebase): Go starts fast in a small container, has Ed25519 in the standard library, and Xendit has an official Go SDK (Midtrans, chosen instead on 2026-10-26, has one too). Region `asia-southeast2` (Jakarta), close to the first buyers. Chosen over TypeScript on Firebase Functions (the user's choice): leaner and faster, few dependencies for money code, at the cost of a second deploy tool; TypeScript 7's Go compiler speeds up builds only, not the Node runtime.
- 2026-10-24: Xendit first (replaced on 2026-10-26, below).
- 2026-10-25: A signing key is provisioned as its 32-byte Ed25519 seed in hex, one environment variable per project and key id (`DAHLUNAS_KEY_<project>_<key id>`), so keys rotate per project without code changes. `cmd/mint new-key` writes a new seed to a 0600 file (refusing to overwrite) and prints only the public key, so the secret never reaches the terminal's scrollback.
- 2026-10-26: Midtrans first, instead of Xendit: Xendit's signup offers only legal entities (PT, CV, Yayasan and others), and the user has no company; Midtrans takes an individual ("Registrasi perorangan", category Software) with QRIS, GoPay, virtual accounts and cards. Its website rules (products with rupiah prices, terms, refunds, contact, checkout not sent to another site) put the shop on Dahlunas's own domain, which each project's website links to. Lemon Squeezy is planned second, for buyers abroad.
