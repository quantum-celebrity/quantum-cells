# Apps (non-core)

Everything here is **optional** relative to the **`CELLS/`** node (the CELLS ledger). The node builds and runs without these folders.

| App | Role |
|-----|------|
| **`CELLS-hive/`** | Desktop client (Windows/Linux): CELL wallets, Task Studio, NVIDIA mining, Mother Hive edge pools, Sky Fang linking. Public downloads at [CELLS.tech/download.html](https://CELLS.tech/download.html). |
| **`CELLS-hive-wallet-extension/`** | Chrome/Edge provider that connects HTTPS sites to the active Hive-held wallet through the authenticated native messaging bridge. It holds no private keys. |
| **`CELLS-edge-agent/`** | Edge Agent, Relay, and Edge Control utilities for pooled CPU/GPU/RAM work. |
| **`CELLS-tray-monitor/`** | Windows tray health monitor for the local home validator stack. |
| **`CELLS-nvidia-ngc/`** | Optional Docker NGC GPU attestation sidecar; pairs with `CELLS_NGC_INGEST_SECRET` on the node. |
| **`CELLS-landing/`** | Legacy marketing stub. **Production site is `CELLS/deploy/landing/`** (served at CELLS.tech). |

To promote an app to its own repository later, copy one folder and add a client SDK from `CELLS/source/sdk/`.
