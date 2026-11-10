# Browser store release

CELLS Wallet should reach consumers through each browser's signed extension
store. Do not rename a ZIP to `.crx` or `.xpi`; consumer browsers reject an
unsigned or off-store package.

## Build the submissions

```powershell
./package-extension.ps1 -OutputDirectory ./dist
```

Use these files in the matching publisher portal:

| Browser | Publisher portal | Submission file |
| --- | --- | --- |
| Google Chrome | Chrome Web Store Developer Dashboard | `CELLS-hive-wallet-extension-<version>-chrome.zip` |
| Microsoft Edge | Partner Center / Edge Add-ons | `CELLS-hive-wallet-extension-<version>-edge.zip` |
| Brave | Chrome Web Store listing | `CELLS-hive-wallet-extension-<version>-brave.zip` |
| Mozilla Firefox | addons.mozilla.org Developer Hub | `CELLS-hive-wallet-extension-<version>-firefox.zip` |

The Chrome and Brave payloads are intentionally identical. Brave installs the
Chrome Web Store release, but the separately named artifact keeps release
evidence clear. The Edge package is also the same Chromium payload and is
submitted independently to Microsoft.

## Required listing links

- Privacy policy: `https://github.com/quantum-celebrity/CELLS/blob/main/PRIVACY.md`
- Support and documentation: `https://CELLS.tech/docs/#/CELLS-hive`
- Security reporting: `https://github.com/quantum-celebrity/CELLS/security/policy`
- Product download page: `https://CELLS.tech/download.html#browser-extension`

Explain that the extension does not store a keystore, passphrase, private key,
or recovery words. It sends an authenticated request to the active CELLS Hive
native host and requires a separate Hive approval for website access,
signatures, and CELL transfers.

## Activate a store listing

1. Record the approved store URL and assigned extension ID.
2. Add the assigned Chromium ID to Hive's native-host allowlist if it differs
   from `habkkkednignfkoffhpbjahcjbikkahh`, then release that Hive update first.
3. Set the matching channel to `published` and its `install_url` in
   `CELLS/deploy/landing/assets/browser-extension-distribution.json`.
4. Deploy the website and test installation, connection approval, signing
   approval, transfer approval, revocation, and update delivery in that browser.

The website script accepts install URLs only from the official Chrome, Edge,
or Firefox store host. A pending channel never renders an active install link.

## Official distribution references

- Chrome: <https://developer.chrome.com/docs/extensions/how-to/distribute>
- Edge: <https://learn.microsoft.com/en-us/microsoft-edge/extensions/publish/publish-extension>
- Firefox: <https://extensionworkshop.com/documentation/publish/signing-and-distribution-overview/>
