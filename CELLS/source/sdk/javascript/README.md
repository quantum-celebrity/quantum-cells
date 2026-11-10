# CELLS-sdk — JavaScript / Node.js SDK

Official JavaScript client for the CELLS HTTP API. Mirrors `sdk/go` feature-for-feature.

> Published on npm as **`CELLS-sdk`** (the bare name `CELLS` was rejected by npm's
> name-similarity heuristic against `qs` / `esm` / `tsdx` etc.; the on-chain
> brand, repo, and binaries are still CELLS — only the npm package id is suffixed).

## Install

```bash
npm install CELLS-sdk
```

(Or vendor `CELLS.js` + `CELLS.d.ts` directly — the SDK has no runtime dependencies.)

### Checking the download

From 0.5.0 on, each version is published by GitHub Actions with a signed record
of the workflow run that built it (npm provenance). To check the packages of a
project, run `npm audit signatures` there with a current npm: npm 12.0.2
verifies both the registry signature and the attestation of `CELLS-sdk` 0.5.0,
while older clients such as npm 10.2.5 can wrongly report a valid attestation
as invalid.

## Quick start

```js
const { CELLSClient } = require('CELLS-sdk');

const client = new CELLSClient('https://api.CELLS.tech');
const address = '5d28b82565421e3666ef7a493e0d39fc89d3450009a32cbda72aa89c7cb820e7';

const balance = await client.getBalance(address);         // CELL
const { next } = await client.getWalletNonce(address);    // nonce for the next transfer
const status = await client.getNodeStatus();
console.log({ balance, next, tip: status.chainTip });
```

Public routes need no credentials. `setToken(jwt)` is only for operator
routes.

## Sending CELL (signed transfers)

A CELLS node accepts a CELL transfer from a client only as an envelope signed
with **ML-DSA-87**, posted to `POST /api/v1/wallet/submit-signed` (Ed25519
transactions were retired on 2026-10-16). Since block 816,600 each transfer
must also name **Proof-of-Entanglement parents**: 2 to 10 transactions
committed in the last 8,640 blocks (about 24 hours). Without them the node
answers HTTP 422 `proof-of-entanglement: ...`.

The SDK never holds a private key. Use the path that matches where the key
lives.

### In a browser, with the CELLS Wallet extension

The extension injects `window.CELLS`. It does not sign arbitrary transfers for
a website: it asks CELLS Hive to send CELL. Hive shows the recipient and
amount, and after the user approves it reads the nonce and the parents, signs
with the wallet's ML-DSA-87 key and submits to the node Hive is configured
for (fee 0).

```js
const { waitForWalletProvider, sendTransferWithProvider } = require('CELLS-sdk');

const provider = await waitForWalletProvider(); // null when the extension is missing
if (!provider) {
    location.assign('https://CELLS.tech/wallet-start.html?login=new');
} else {
    const result = await sendTransferWithProvider(provider, {
        recipient: '1111111111111111111111111111111111111111111111111111111111111111',
        amount: 1.25, // CELL, at most 8 decimals
    });
    // { transaction_id, status: 'pending', broadcast, sender }
}
```

`sendTransferWithProvider` first calls `CELLS_requestAccounts` (Hive asks the
user to connect the site only the first time), then `CELLS_sendTransaction`.

### With your own signer

`sendTransfer` reads the nonce and the parents from the node, builds the
envelope, hands it with its signing bytes to your `signTransfer`, and submits
the result:

```js
const { CELLSClient } = require('CELLS-sdk');

const client = new CELLSClient('https://api.CELLS.tech');
const out = await client.sendTransfer({
    sender: myAddress, // hex(sha256(ML-DSA-87 public key))
    recipient: '1111111111111111111111111111111111111111111111111111111111111111',
    amount: 1.25,
    fee: 0,
    // An ML-DSA-87 (FIPS 204) signature over signingBytes with an empty
    // context string, made by a key you hold.
    signTransfer: async (envelope, signingBytes) => ({
        signature: await mySigner.sign(signingBytes),
        public_key: mySigner.publicKey,
    }),
});
// out: { transaction_id, status: 'pending', broadcast: 'block-pending' }
const receipt = await client.getReceipt(out.transaction_id); // 404 until it is in a block
```

`signTransfer` returns `{ signature, public_key }` (hex strings or
`Uint8Array`s), or the complete signed envelope: an object, or its JSON as
`CELLScli wallet sign-tx` and the web wallet's `CELLS_wallet_sign_transaction`
WASM helper print it. The SDK refuses to submit a returned envelope that
differs from the one it prepared.

Step by step the same is `prepareTransfer` → `transferSigningBytes` → your
signer → `submitSignedTransfer`. An envelope signed entirely elsewhere (for
example `CELLScli wallet sign-tx --auto-nonce --auto-parents`) goes straight to
`submitSignedTransfer`. Parents expire, so sign shortly before you submit.
`prepareTransfer` generates the transfer `id` with Web Crypto
(`globalThis.crypto`, available in browsers and Node 19+); on Node 18 pass
`id` yourself (16-128 characters of `0-9 A-Z a-z _ -`).

`transferSigningBytes(envelope)` returns exactly what the node verifies: the
envelope with `signature: ""` and without `public_key`, encoded as Go's
`encoding/json` encodes it (field order `id, sender, recipient, amount, fee,
geotag, parent_cells, nonce, signature, timestamp`; `<`, `>`, `&`, U+2028 and
U+2029 escaped). The tests pin it to bytes printed by the node's own code.

### Refusals

`submitSignedTransfer` and `sendTransfer` throw `TransferError` (an
`ApiError` with `reason`, `serverMessage` and, for duplicates,
`transactionId`):

| `reason` | HTTP | What to do |
|---|---|---|
| `poe_parents` (`isPoERejection`) | 422 | Parents missing or outside the window: call `sendTransfer` again (fresh parents, same nonce). |
| `signature` | 422 | Sign `transferSigningBytes()` of exactly the envelope you submit. |
| `nonce` | 409 | Nonce replay or gap: prepare the transfer again. |
| `duplicate` | 409 | This ID, or another transfer with this nonce, is already waiting for the next block (one per sender per block). Follow it with `getReceipt`. |
| `insufficient_balance` | 402 | Balance below amount + fee. |
| `invalid` | 400 | See `serverMessage`. |
| `rate_limited` | 429 | Wait, then retry. |
| `unavailable` | 501, 503 | Queue full, read-only follower or writes paused; retry later. |

`sendTransfer` makes one attempt; every call reads a fresh nonce and fresh
parents.

## CELLS-20 tokens

CELLS-20 tokens (specification: `CELLS/docs/docs/CELLS_20.md`) are created,
sent, minted, burned and renounced with transactions signed by the same
ML-DSA-87 key that holds your CELL. The signature covers a 32-byte digest of
the sender, the nonce, the fee and the action's fields; `signCELLS20` gets
that digest. Nodes refuse token transactions until the network sets an
activation height
(`(await client.getNodeStatus()).extra.consensus_auth.CELLS20_active`).

```js
const { CELLSClient } = require('CELLS-sdk');
const client = new CELLSClient('https://api.CELLS.tech');

const created = await client.sendCELLS20({
    sender: myAddress,
    payload: {
        action: 'create', symbol: 'GROOT', name: 'Groot Coin', decimals: 8,
        max_supply: '2100000000000000',    // base units: 21 million tokens of 8 decimals
        initial_supply: '100000000000000', // 1 million of them to the creator
        mint: 'issuer',
    },
    feeDust: 0,
    // Sign the digest with ML-DSA-87 (empty context); return { signature, public_key }.
    signCELLS20: async (digest) => mySigner.sign(digest),
});
// created.token is the new token's id; follow created.tx_id with getReceipt().

await client.sendCELLS20({
    sender: myAddress,
    payload: { action: 'transfer', token: created.token, to: payee, amount: '150000000' }, // 1.5 tokens
    signCELLS20: async (digest) => mySigner.sign(digest),
});
```

Amounts are whole base units as decimal strings or bigints, up to
2^256 - 1; numbers are refused because they cannot hold every amount. Nonces
and fees may be numbers, bigints or decimal strings. `validateCELLS20Action`
applies the node's field rules offline, `CELLS20TokenId(creator, nonce)` gives
a token's id before its create is sent, and `CELLS20SigningDigest(tx)` is the
exact 32 bytes a node verifies (pinned to vectors the node's own code
computes). A refusal throws an `ApiError`: HTTP 422 for a broken rule, with
nothing charged. With `CELLScli`, `wallet sign-token-tx` signs offline and
`CELLS20 submit` posts the result. The CELLS Wallet extension cannot sign token
transactions yet.

## API

| Method | Endpoint | Public |
|--------|----------|--------|
| `getBalance(address)` | `GET /api/v1/wallet/balance` | ✓ |
| `getWalletNonce(address)` | `GET /api/v1/wallet/nonce` | ✓ |
| `getChainParents({ limit })` | `GET /api/v1/chain/parents` | ✓ |
| `getPoEParents()` | `GET /api/v1/chain/parents` (receipts fallback when the route is missing) | ✓ |
| `getReceipt(txID)` / `getReceipts({ from, to, limit })` | `GET /api/v1/receipts/{tx_id}` / `GET /api/v1/receipts` | ✓ |
| `prepareTransfer(request)` | the nonce and parents reads above | ✓ |
| `submitSignedTransfer(envelope)` / `sendTransfer(request)` | `POST /api/v1/wallet/submit-signed` | ✓ (the signature is the authorization) |
| `getAccount(address)` | `GET /api/v1/mining/account` | ✓ |
| `prepareCELLS20(request)` / `submitCELLS20(tx)` / `sendCELLS20(request)` | `POST /api/v1/CELLS20/submit` | ✓ (the signature is the authorization); refused until the activation height |
| `getCELLS20Tokens({ cursor, limit })` / `getCELLS20Token(id)` / `getCELLS20Holders(id, { cursor, limit })` / `getCELLS20Balances(address)` | `GET /api/v1/CELLS20/tokens`, `/tokens/{id}`, `/tokens/{id}/holders`, `/balances/{address}` | ✓ |
| `getStreamActionNonce(address)` | `GET /api/v1/streams/nonce` | ✓ |
| `getStreams(filters)` / `getStream(streamID)` | `GET /api/v1/streams`, `GET /api/v1/streams/{stream_id}` | ✓ |
| `submitStreamAction(envelope)` | `POST /api/v1/streams/actions/submit-signed` | ✓, but 503 while the pilot pauses stream actions |
| `getLiveness()` / `getReadiness()` / `getHealth()` | `GET /api/v1/health/*` | ✓ |
| `getNodeStatus()` | `GET /api/v1/status` | ✓ |
| `sendTransaction(from, to, amount)` | none | ⚠ deprecated 0.4.0 — rejects without a request; see below |
| `getTransaction(txID)` | `GET /api/v1/transactions/{id}` | ⚠ deprecated 0.4.0 — operator JWT (401 otherwise); use `getReceipt` |
| `getNetworkTopology()` | `GET /api/v1/network/topology` | ⚠ deprecated 0.4.0 — operator JWT (401 otherwise); use `getNodeStatus().peers` |
| `getRecentTransactions(address, limit)` | `GET /api/v1/wallet/transactions` | ⚠ deprecated 0.3.1 — not registered on any node; use `getReceipts` |
| `getPeers()` | `GET /api/v1/network/peers` | ⚠ deprecated 0.3.1 — not registered on any node; use `getNodeStatus().peers` |
| `getMetricsJSON()` / `getMetricsPrometheus()` | `GET /api/metrics`, `/api/metrics/prometheus` | ⚠ deprecated 0.3.1 — operator dashboard only |

Deprecated methods are kept for compatibility; the routes they call fail for
public clients (401 or 404). `sendTransaction` used to post an unsigned
`{from, to, amount}` body to `POST /api/v1/wallet/send`. That route is
operator-only (an operator JWT plus a request signature made with the node's
own key), it spends from the node's own wallet rather than from `from`, and
it reads `{recipient, amount, fee, geotag, parent_cells}`, so the call never
worked against a node. It now rejects with
`err.code === 'CELLS_UNSIGNED_SEND_REMOVED'` and points to the signed path.

All methods return `Promise<T>`. Errors on non-2xx responses are thrown as `ApiError`
with `status`, `url`, and `body` fields — use the `isNotFound` / `isUnauthorized`
helpers for common cases.

## Active-use CELL billing

Version `0.3.3` adds the crash-safe runtime for `CELLS/streams/v1` and reads
the action nonce directly from consensus before signing:

```js
const {
    CELLSClient,
    CellStreamWallet,
    CellStreamServiceMeter,
} = require('CELLS-sdk');

const client = new CELLSClient('https://api.CELLS.tech');
const wallet = new CellStreamWallet({
    client,
    address: activeWalletAddress,
    // CELLS Hive, a native wallet bridge, or another local signer supplies this.
    // The SDK never receives or stores the wallet private key.
    signAction: (action, bytes) => walletBridge.signStreamAction(action, bytes),
});

const meter = new CellStreamServiceMeter({
    wallet,
    storage: durableNonSecretStorage,
    sessionSigner: secureDeviceSessionSigner,
    // This endpoint verifies the session receipt, wraps it in the provider's
    // wallet-signed receipt action, and resolves { confirmed: true } only
    // after CELLS confirmation.
    receiptSubmitter: (receipt) => serviceAPI.submitUsageReceipt(receipt),
});

await meter.initialize();
await meter.recover(await service.isActuallyActive());

service.on('started', () => meter.onServiceStarted({
    streamId: 'vpn-device-001',
    provider: providerWalletAddress,
    serviceId: 'CELLS-vpn',
    deviceIdHash: saltedDeviceIDHash,
    priceDust: 200000000,
    pricePeriodSeconds: 2592000,
    budgetDust: 200000000,
    maxActiveSeconds: 2592000,
    expiresAt: '2027-09-01T00:00:00Z',
}));
service.on('stopped', () => meter.onServiceStopped());
```

The service must call `onServiceStarted` only after its tunnel, session, or
other paid capability is actually available. It must call `onServiceStopped`
as soon as that capability stops. The meter:

- checkpoints active time to durable storage;
- submits cumulative receipts every 30 seconds and at lifecycle boundaries;
- retries the same signed receipt after uncertain delivery;
- does not count application downtime after a crash;
- serializes wallet actions so one SDK instance cannot race its own nonce; and
- stores public runtime state only, never wallet or session private keys.

The temporary Ed25519 session key must live in the platform secure keystore.
The provider-side `receiptSubmitter` must be idempotent by
`(stream_id, sequence)`, return `{ confirmed: true }` only after chain
confirmation, and must not hold its CELLS wallet key in a public client. A
backend can use `CellStreamWallet.submitAction` with `action: 'receipt'` after
authenticating the service session.

## Options

```js
new CELLSClient('http://node:8080', {
    fetch: myFetchImpl,     // override global fetch (useful for Node < 18)
    timeoutMs: 10_000,      // per-request timeout; 0 disables
});
```

`setToken(jwt)` sends `Authorization: Bearer` for operator routes.
`setAPIKey(key)` sends `X-API-Key`, which CELLS nodes do not read (kept for
gateways that do).

## Testing

```bash
cd sdk/javascript
npm test
```

The suite needs Node 19+ (built-in `fetch`, `node:test` and
`globalThis.crypto`); the SDK itself runs on Node 18. The same command runs as
`prepublishOnly`, so a broken build cannot reach the registry.

## Releasing

The package is published from CI by `.github/workflows/sdk-javascript-publish.yml`,
through npm trusted publishing: npm accepts the workflow run itself, so no npm
token is needed. Tag the repo with the matching version:

```bash
# bump version field in package.json + CHANGELOG.md, commit, then:
git tag sdk-js-vX.Y.Z    # X.Y.Z = the version in package.json
git push origin sdk-js-vX.Y.Z
```

The workflow verifies the tag suffix matches `package.json`, runs the test
suite, and publishes with `--provenance` (Sigstore attestation linking the
tarball to the GitHub Actions run). An `NPM_TOKEN` repository secret is only a
fallback, used if npm refuses the run's identity.

## License

MIT — see [`LICENSE`](LICENSE) and [`CHANGELOG.md`](CHANGELOG.md).
