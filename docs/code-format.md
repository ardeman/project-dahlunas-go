# Unlock code format, version 1

Status: implemented in `internal/unlock` and `cmd/mint`; codes are minted by
hand until the payment webhook exists.

An unlock code carries an entitlement signed with a project's Ed25519 key. The
app verifies it locally using a bundled public key. Verification never contacts
Dahlunas, including in children's apps. Email, payment references, prices and
device identifiers do not belong in the code.

## Text representation

The canonical representation is `DL1.` followed by 144 uppercase Base32
characters, for 148 ASCII characters total. Encode the entire 90-byte binary
envelope as one value using the alphabet `ABCDEFGHIJKLMNOPQRSTUVWXYZ234567`
from [RFC 4648, section 6](https://www.rfc-editor.org/rfc/rfc4648#section-6),
without `=` padding. Do not encode the payload and signature separately.

Copy/paste is the primary entry method. A QR code may contain the same canonical
text, with no URL or buyer data. For manual entry, a display may group the body
into 18 groups of eight characters separated by hyphens (165 characters including
the prefix). A full Ed25519 signature is 64 bytes; preserving that signature
means this cannot be a short code to type by hand.

To normalize input:

1. Reject raw input longer than 512 bytes.
2. Remove ASCII space, tab, carriage return and line feed throughout the input,
   then convert ASCII `a`–`z` to uppercase. Do not apply Unicode normalization.
3. Require the exact prefix `DL1.` and remove it.
4. Remove ASCII hyphens from the body, then require exactly 144 characters from
   the alphabet above. Reject everything else, including padding, Unicode
   lookalikes and other punctuation. Do not substitute `0` for `O` or `1` for `I`.
5. Decode the body, require exactly 90 bytes, and re-encode to confirm that the
   normalized body is canonical.

Normalization only changes presentation; the signature covers binary bytes.
Persist and share the canonical representation, regardless of input grouping.

## Binary envelope

The envelope is `payload || signature`. The payload is exactly 26 bytes and the
signature is exactly 64 bytes. All multi-byte integers are unsigned, big-endian.
Offsets are zero-based; there are no optional fields or trailing bytes.

| Offset | Bytes | Field | Meaning |
| --- | --- | --- | --- |
| 0 | 1 | Version | Exactly `1`. Must agree with the `DL1.` prefix. |
| 1 | 1 | Key ID | `1`–`255`, selecting a public key within the project. |
| 2 | 2 | Project ID | `1`–`65535`, permanently assigned to one of Ardeman's projects. |
| 4 | 2 | Product ID | `1`–`65535`, permanently assigned within that project. |
| 6 | 16 | Code ID | Cryptographically random 128-bit value, not all zero. |
| 22 | 4 | Issued day | Whole UTC days since `1970-01-01`, where that day is `0`. |
| 26 | 64 | Signature | Ed25519 signature over the message defined below. |

Project and product assignments must be recorded in project configuration before
minting. ID zero is reserved for version, key, project and product; issued day
zero is valid. Product IDs must never be reused for different entitlements.
The example below uses test assignments and does not register a real project.

The issued day is `floor(unix_seconds / 86400)` at issuance. Issuers must reject
dates outside the unsigned 32-bit day range. It is metadata, not an expiry or a
clock check: version 1 entitlements are perpetual. An app must not reject an
otherwise valid code because its device clock is wrong. Subscriptions, expiry,
or time-limited entitlements require a separately specified format.

## Signing

Use plain Ed25519 as specified in
[RFC 8032, section 5.1](https://www.rfc-editor.org/rfc/rfc8032#section-5.1).
Sign this exact byte sequence:

```text
ASCII("Dahlunas/unlock/v1") || 0x00 || payload[0:26]
```

The prefix is case-sensitive and contains one terminating zero byte. Use no
prehash, Ed25519 context option, JSON, text encoding or additional newline.
In Go, use `ed25519.Sign(privateKey, message)` and
`ed25519.Verify(publicKey, message, signature)`. The domain prefix keeps unlock
signatures distinct from signatures for other purposes. Never truncate the
signature or implement the cryptographic algorithm by hand.

Each project has its own signing keys. Private keys come from environment
variables (Secret Manager in production) and must never be committed, printed or
logged. A private key is provisioned as its 32-byte Ed25519 seed in hex, in
`DAHLUNAS_KEY_<project>_<key id>` (made by `cmd/mint new-key`); this is not part
of the code format. Apps bundle only the 32-byte public keys,
indexed by key ID for their own project.

## Verification and activation

1. Normalize and decode the input as above.
2. Require binary version `1` and valid field ranges, including a nonzero code ID.
3. Require the app's expected project ID and a product it explicitly supports.
   A signature alone does not authorize an unknown product.
4. Look up the key ID in that project's bundled keys. Reject unknown keys;
   never load a key or URL supplied by the code.
5. Verify the full signature over the domain prefix and exact payload bytes.
6. Only after all checks pass, grant the mapped product entitlement and persist
   the canonical code locally. Re-entering the same code is a harmless success.

Treat decoded fields as untrusted until verification succeeds. A rejected code
must never change entitlement state. Apps may distinguish unsupported versions
or keys to suggest an app update, but must not fall back to weaker verification.
Any user-facing messages must be available in Indonesian and English.

The code is a bearer entitlement: anyone holding it can use it. Offline
verification cannot prevent sharing, enforce a device limit, revoke a refund,
or learn that a key was compromised. Do not claim one-time redemption. Adding
online redemption later would require an explicit product decision, especially
for children's apps.

## Issuance, retries and key rotation

Generate the code ID using a cryptographic random source (`crypto/rand` in Go).
Never derive it from buyer data or a sequential order number. For paid orders,
store the canonical code against the order atomically and enforce one issuance
per payment in durable storage. Webhook retries must retrieve that code instead
of generating a new ID or choosing a new date/key. Delivery retries reuse the
stored code; the format itself does not provide webhook or email idempotency.

To rotate a key, assign a new key ID, ship its public key in the app, and only
then begin issuing with it. Keep older public keys so perpetual purchases still
work. Never reuse a key ID for another key within the project. Version 1 allows
255 key IDs per project; exhausting them requires a new format. Old app builds
cannot accept codes signed by keys they do not have. Removing a compromised
public key in an app update also invalidates legitimate codes signed by it;
already installed offline apps will not learn about the removal.

## Verification fixture

This fixture is test data only. It grants no real product. Its throwaway private
key was discarded; the public key below must never be trusted in a production
app. Hex strings describe raw bytes, not text to sign.

```text
public key (32 bytes):
008c7deff2cbf3a7a1a584bcd07688840e98a21e79758af3d14dae5f60c44c1f

payload (26 bytes):
0101000100010102030405060708090a0b0c0d0e0f100000510e

signature (64 bytes):
1019410784ca3c9cc4665b73619cd4ef8391214291318c19c46506a8586b3ba4ae6ca4eaccccc6c247763aa89325cca4322dbe4d7b51c84a9b34d77a4a617408

canonical code (148 characters):
DL1.AEAQAAIAAEAQEAYEAUDAOCAJBIFQYDIOB4IAAACRBYIBSQIHQTFDZHGEMZNXGYM42TXYHEJBIKITDDAZYRSQNKCYNM52JLTMUTVMZTGGYJDXMOVISMS4ZJBSFW7E262RZBFJWNGXPJFGC5AI
```

Expected fields: version `1`, key `1`, project `1`, product `1`, code ID hex
`0102030405060708090a0b0c0d0e0f10`, issued day `20750` (`2026-10-24` UTC).

When implementing, add tests that accept this fixture and its permitted case,
whitespace and grouping variants. Reject changed payloads and signatures,
wrong keys/projects, unsupported products/versions/keys, zero reserved fields,
zero code IDs, malformed lengths, padding, extra bytes and invalid characters.
Test that an incorrect local clock does not invalidate the fixture and that
repeated activation does not change the granted entitlement.
