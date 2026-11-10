# CELLS Tray Monitor

Tiny Windows notification-area monitor for the local CELLS home node.
Documented on the public docs portal at
[CELLS.tech/docs/#/tray-monitor](https://CELLS.tech/docs/#/tray-monitor)
(after the landing deploy that ships this entry).

It watches:

- validator readiness, chain progress, peers, build, configured/active mode,
  process count, and task-action readiness
- CELLSMiner service/process state and recent accepted-proof activity
- home gateway process and public relay status
- attester health and listener exposure
- referral/faucet treasury signer health
- local stack watchdog and local GUI processes
- monitored TCP listeners; Home Server services must remain loopback-only

Every poll writes a machine-readable snapshot to
`%APPDATA%\CELLS-Tray-Monitor\status.json`.

The app has no normal Exit command. It is tray-only and meant to stay running.
It can still be stopped by the operator with Task Manager or by removing its
Startup launcher.

Build:

```powershell
dotnet publish .\CELLS-tray-monitor.csproj -c Release -r win-x64 --self-contained false -p:PublishSingleFile=true -o .\dist
```

Install at user logon:

```powershell
.\install_startup.ps1
```
