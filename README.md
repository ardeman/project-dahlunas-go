# Dahlunas

One checkout for all of Ardeman's own projects. A buyer pays through a payment gateway (Xendit first), the gateway's webhook tells Dahlunas, and Dahlunas sends back what was bought: for offline apps such as Coba Lagi, a signed unlock code the app checks without going online.

Go (standard library first) with htmx pages, running on Cloud Run behind Firebase Hosting.

## Setup

- Go 1.24 or newer.
- `gcloud` (Google Cloud SDK) to deploy; Docker isn't needed, since Cloud Run builds from source.
- Firebase CLI (`npm i -g firebase-tools`) for the Hosting rewrite. Pick the Firebase project once with `firebase use --add`.
- For local configuration, copy `.env.example` to `.env`, then load it with `set -a; source .env; set +a` before running the server. The server reads environment variables; it does not load `.env` automatically. Defaults work without this step.

## Commands

| What | Command |
| --- | --- |
| Run locally | `go run ./cmd/server` (http://localhost:8080) |
| Test | `go test ./...` |
| Checks | `test -z "$(gofmt -l .)" && go vet ./... && go test ./...` |
| Deploy the service | `gcloud run deploy dahlunas --source . --region asia-southeast2` |
| Deploy Hosting | `firebase deploy --only hosting` |

## Layout

- `cmd/server/` starts the HTTP server (`PORT`, set by Cloud Run).
- `internal/web/` holds routes, page templates (`templates/`) and static files (`static/`, htmx vendored at 2.0.4).
- `public/` is Firebase Hosting's folder; every request is rewritten to the Cloud Run service `dahlunas` in `asia-southeast2` (Jakarta).
- `.github/workflows/checks.yml` runs the Checks command on pushes and pull requests.
- `docs/code-format.md` specifies the shared offline unlock-code format for Dahlunas and the apps.

The starter serves a bilingual homepage at `/`, a health check at `/healthz`, and embedded htmx at `/static/htmx.min.js`. Payments and unlock codes are planned below.

## Roadmap

1. **Code format specified:** [version 1](docs/code-format.md) carries project, product, id and date, signed with Ed25519. Codes are 148 characters for copy/paste or QR scanning, with optional grouping for manual entry. Each project has its own keys: private keys stay in Secret Manager, and apps bundle public keys. Next, implement signing and offline verification with compatibility tests against the documented fixture.
2. **Minting:** a command (`cmd/mint`) that makes a code by hand, for orders before the webhook exists.
3. **Xendit:** create payment links per project and product; receive the payment webhook, check its token, make the code and email it to the buyer. A payment must never send two codes, even when the webhook comes twice.
4. **Projects:** per-project config (products, prices, key, email wording in Indonesian and English).
5. **Worldwide:** international cards through Xendit if the individual account allows them, else a second gateway behind the same interface.
6. **Ready for a rush** (traffic stays small, since apps check codes offline; these are what break first):
   - Email codes through a real sender (Resend, Postmark or SES) on our domain, and show the code on the "paid" page too.
   - Rate-limit checkout and guard it against bots (Turnstile or App Check).
   - One code per payment, however often Xendit retries the webhook.
   - Card fraud and chargebacks: keep amounts small; codes checked offline can't be taken back.
   - Xendit may ask for more documents as volume grows; taxes (NPWP, VAT abroad).

## License

Dahlunas is licensed under the [MIT License](LICENSE). The vendored [htmx 2.0.4](https://github.com/bigskysoftware/htmx/tree/v2.0.4) script uses the [Zero-Clause BSD license](internal/web/static/htmx.LICENSE).
