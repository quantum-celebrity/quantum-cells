# Downloads & verification

Where to get CELLS software and how to check that a file really comes from the
CELLS project before you run it.

## Current downloads

All downloads are linked from **[CELLS.tech/download.html](https://CELLS.tech/download.html)**.

| Product | Version | Platform | File | Size | SHA-256 |
|---|---|---|---|---|---|
| CELLS Hive | 1.4.22 | Windows x64 | `CELLS-hive-1.4.22-win-x64.exe` | 128.6 MB | `d2f604b6b3b8f70913510072c525544e22a4c0a10ef4c9540b36ab2a3667bfaa` |
| CELLS Hive | 1.4.22 | Linux x86-64 | `CELLS-hive-1.4.22-linux-x86_64.AppImage` | 163.8 MB | `e5fc9084a295dd2e0cec686977a0e83f2f7639b564418ec0f47e061762fd56ae` |
| CELLS Hive | 1.4.22 | Linux x86-64 | `CELLS-hive-1.4.22-linux-x64.tar.gz` | 161.6 MB | `abefcd1d7f69712cb306237c269e195e43948b9cdc3812ac709b5cc3690def0f` |

Hive 1.4.22 (published 10 October 2026) is served from the release channel
`https://CELLS.tech/downloads/hive-v2/`, together with checksum files
(`SHA256SUMS-win.txt`, `CELLS-hive-1.4.22-linux-SHA256SUMS.txt`) and the signed
release manifests (`CELLS-hive-release-windows-v2.json`,
`CELLS-hive-release-linux-v2.json`).

Other packages on the download page (browser wallet extension, Edge Agent, Core
validator and home gateway builds) come with their own `SHA256SUMS` files.

> [!NOTE]
> The Core validator package on the download page (v0.4.7-rc.9) is an operator
> build. It is not the release that runs the public pilot network today
> (`hardened-legacy-20261108-d7ffcd4-hl2-edge-go12610-ids-pqe-rc1108`, see [Network status](NETWORK_STATUS.md)).

> [!IMPORTANT]
> **Upgrading from Hive 1.4.20 (Windows) or 1.4.17 (Linux):** install 1.4.22 by
> hand once. Those versions trust the previous release key, which was rotated, so
> they cannot verify 1.4.22 on their own. Your wallet and settings are kept.
> Hive 1.4.17 stops working after 9 November 2026 and 1.4.20 after
> 19 December 2026.

## Check the SHA-256 (everyone)

Compare the result with the table above or with the checksum file. If it does
not match, delete the file and download it again from CELLS.tech.

**Windows (PowerShell):**

```powershell
Get-FileHash -Algorithm SHA256 .\CELLS-hive-1.4.22-win-x64.exe
```

**Linux:**

```bash
curl -fsSLO https://CELLS.tech/downloads/hive-v2/CELLS-hive-1.4.22-linux-SHA256SUMS.txt
sha256sum -c --ignore-missing CELLS-hive-1.4.22-linux-SHA256SUMS.txt
```

## What Hive checks for you

Hive does not trust an update just because it was downloaded from CELLS.tech.
Before installing an update it:

1. downloads the signed release manifest for its platform;
2. checks the **ML-DSA-87** signature against the release public key built into
   Hive, and rejects any other `key_id`;
3. rejects a manifest that has expired or that claims to be valid for more than
   120 days;
4. checks the name, size and SHA-256 of the updater metadata and the installer
   listed in the manifest.

If any step fails, Hive does not install the update.

## The release key

| | |
|---|---|
| Key | CELLS Hive release key **v2** |
| Algorithm | ML-DSA-87 (NIST FIPS 204) |
| `key_id` | `4081bf2c4755f4c5c1565b4fac75e14a7e0b52042ac3d34bf8a7f525866c64a9` |
| Public key | [`CELLS/deploy/release-trust/CELLS-hive-release-key-v2.json`](https://github.com/blackbeardONE/CELLS/blob/main/CELLS/deploy/release-trust/CELLS-hive-release-key-v2.json) |
| In use since | Hive 1.4.21 (October 2026) |
| Previous key | `10ab9c57…07a1`, **rotated** in October 2026; it no longer signs new releases |

The `key_id` is the SHA-256 of the raw public key bytes, so you can recompute it
from the public key file.

Signed manifests are re-issued at least every 90 days. Each manifest states
its own `issued_at` and `expires_at` dates; check them as described below.

## Check the signature yourself (advanced)

You need Python 3 and `CELLScli` (built from this repository, `CELLS/source/cmd/CELLScli`).

```bash
# 1. The signed manifest and the release public key
curl -fsSLO https://CELLS.tech/downloads/hive-v2/CELLS-hive-release-linux-v2.json
curl -fsSL https://raw.githubusercontent.com/blackbeardONE/CELLS/main/CELLS/deploy/release-trust/CELLS-hive-release-key-v2.json \
  -o CELLS-hive-release-key-v2.json

# 2. Split the envelope and print the key ids
python3 - <<'PY'
import base64, hashlib, json
env = json.load(open("CELLS-hive-release-linux-v2.json"))
key = json.load(open("CELLS-hive-release-key-v2.json"))
open("manifest.json", "wb").write(base64.b64decode(env["manifest_base64"]))
open("manifest.sig", "w").write(env["signature"])
open("release-key-v2.hex", "w").write(key["public_key"])
print("envelope key_id :", env["key_id"])
print("computed key_id :", hashlib.sha256(bytes.fromhex(key["public_key"])).hexdigest())
PY

# 3. Verify the ML-DSA-87 signature
CELLScli wallet verify --public-key-file release-key-v2.hex \
  --message-file manifest.json --signature-file manifest.sig
```

Both key ids must be `4081bf2c…c64a9` and the signature must verify. Then open
`manifest.json`: it lists every release file with its size and SHA-256, and the
`issued_at` / `expires_at` dates. The installer's SHA-256 must match the entry
in the manifest. For Windows use `CELLS-hive-release-windows-v2.json`.

## Windows SmartScreen

Windows installers are **not Authenticode-signed** yet, so Windows may show
"Windows protected your PC". `Get-AuthenticodeSignature` reports `NotSigned`.
The ML-DSA-87 release signature above protects updates inside Hive, but it does
not give Windows a verified publisher. Check the SHA-256, then choose
**More info → Run anyway**. See the
[code signing policy](https://github.com/blackbeardONE/CELLS/blob/main/CODE_SIGNING_POLICY.md)
for the plan.

CELLS will never ask you to install a root certificate, disable antivirus, or run
a download that is not linked from CELLS.tech.

## Report a bad download

If a hash or signature does not match, do not run the file. Report it as
described in the [security policy](https://github.com/blackbeardONE/CELLS/blob/main/SECURITY.md).
