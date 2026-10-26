# Device activation

Some projects bind each code to one device at a time. The project sets
`"activation": true` in the catalog ([projects](projects.md)); today only Bedug
does. Coba Lagi and other children's apps never use activation: they check codes
offline and never call Dahlunas.

A code stays an ordinary [version 1 code](code-format.md). Activation adds a
record per code ID in Firestore (`activations-sandbox` or
`activations-production`) and a signed activation the app keeps and checks
offline.

## Rules

- The first device to activate a code holds it.
- The same device can activate or check again at any time; nothing changes.
- When the buyer enters the code on another device, the code moves there, up to
  `3` times per code. After that Dahlunas refuses moves; support can reset the
  record's `Moves` field in Firestore by hand.
- The app's periodic check never moves a code. If the code has moved, the check
  answers `moved` and the app drops its activation.
- Moving takes effect on the old device at its next online check. An offline
  device keeps its activation until then: prayer times must never depend on
  Dahlunas being reachable.

## Install ID

On first launch the app makes a random 16-byte install ID and stores it in its
own settings, written as 32 lowercase hex characters. It is never derived from
hardware, an account or an advertising ID. Reinstalling makes a new one, which
counts as a new device. Dahlunas stores only `SHA-256(install ID as ASCII)`.

## Request

```http
POST /activations
Content-Type: application/json

{"code": "DL1.…", "install_id": "000102030405060708090a0b0c0d0e0f", "move": true}
```

The code is the canonical code text (spaces, hyphens and lowercase allowed, as
in the code format). Send `"move": true` only when the buyer enters a code;
the periodic check, about once a week while online, sends `false`. Requests are
limited per server instance; on `busy`, retry later with backoff.

## Answers

| Status | Body | App action |
| --- | --- | --- |
| 200 | `{"activation": "DA1.…", "moves_left": 2}` | Verify the activation, store it with the code, unlock. |
| 409 | `{"error": "moved", "moves_left": 2}` | (Check only.) Drop the activation; offer to enter the code again, which moves it back. |
| 409 | `{"error": "move_limit", "moves_left": 0}` | Say the code has moved too often; show the support email. |
| 400 | `{"error": "invalid"}` | Not a valid code for this app. |
| 404 | `{"error": "not_supported"}` | This project does not activate codes. |
| 429, 503 | `{"error": "busy"}` | Keep the current state; try again later. |

A network failure is not an answer: keep the current state.

## Activation format

`DA1.` followed by 192 unpadded Base32 characters (RFC 4648 alphabet) of a
56-byte payload and its 64-byte Ed25519 signature. The signed message is the
ASCII domain `Dahlunas/activation/v1` and one zero byte, then the payload.

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 1 | Version, `1` |
| 1 | 1 | Key ID, the project's signing key (its current one, maybe newer than the code's) |
| 2 | 2 | Project ID, big-endian |
| 4 | 16 | Code ID, from the code |
| 20 | 32 | Device: `SHA-256(install ID)` |
| 52 | 4 | Day activated, whole UTC days since 1970-01-01, big-endian; metadata only |

The app accepts an activation when its signature verifies under a public key it
bundles for that key ID, its project matches, its code ID matches the stored
code's, and its device equals the hash of the app's own install ID. It is
checked offline on every launch; a device clock never invalidates it.

## Fixture

Test data only; the seed is public.

```text
seed (32 bytes): 000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
public key:      03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8
install ID:      000102030405060708090a0b0c0d0e0f
device hash:     d65df89f702eec58ca3c7bf2001c9ffc7cd80553ae01d42a799ba26756142d96
payload:         010100020102030405060708090a0b0c0d0e0f10d65df89f702eec58ca3c7bf2001c9ffc7cd80553ae01d42a799ba26756142d9600005110

activation:
DA1.AEAQAAQBAIBQIBIGA4EASCQLBQGQ4DYQ2ZO7RH3QF3WFRSR4PPZAAHE77R6NQBKTVYA5IKTZTORGOVQUFWLAAACRCBLKDC75ZPO3ZHAXJRM4UJXCRSML45BMOTSEOX5KTE27G7AOG4UA5A2S4TG6WABQDI4YAZ5VOIDBGAQPHGBN2RBWVRMQIF6NUCR3O6IK
```

Fields: key `1`, project `2`, code ID `0102030405060708090a0b0c0d0e0f10`, day
`20752` (`2026-10-26`). Apps should accept it with this install ID and reject
it with any other, and reject a changed character, a `DL1.` prefix and wrong
lengths.
