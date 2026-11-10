(() => {
  "use strict";

  const DOWNLOAD_URL = "/download.html#wallet-extension-title";
  const status = document.getElementById("wallet-handoff-status");
  const openButton = document.getElementById("wallet-handoff-open");
  let opening = false;

  const provider = () =>
    window.CELLS?.isCELLSHive === true &&
    typeof window.CELLS.request === "function"
      ? window.CELLS
      : null;

  const openOnboarding = async () => {
    if (opening) return;
    const CELLS = provider();
    if (!CELLS) {
      status.textContent = "CELLS Wallet is not installed. Opening the official download...";
      window.setTimeout(() => window.location.replace(DOWNLOAD_URL), 500);
      return;
    }

    opening = true;
    openButton.disabled = true;
    status.textContent = "CELLS Wallet detected. Opening onboarding...";
    try {
      const result = await CELLS.request({
        method: "CELLS_openOnboarding",
        params: { login: "new" },
      });
      if (!result?.opened) throw new Error("The extension did not open onboarding");
      status.textContent = "CELLS Wallet opened in a new tab. You can close this page.";
    } catch (error) {
      status.textContent =
        error instanceof Error ? error.message : "CELLS Wallet could not be opened.";
      openButton.disabled = false;
      opening = false;
    }
  };

  const detect = () => {
    if (provider()) {
      void openOnboarding();
      return true;
    }
    return false;
  };

  openButton.addEventListener("click", () => void openOnboarding());
  window.addEventListener("CELLS#initialized", detect, { once: true });

  if (!detect()) {
    let attempts = 0;
    const timer = window.setInterval(() => {
      attempts += 1;
      if (detect()) {
        window.clearInterval(timer);
      } else if (attempts >= 6) {
        window.clearInterval(timer);
        status.textContent = "CELLS Wallet is not installed. Opening the official download...";
        window.setTimeout(() => window.location.replace(DOWNLOAD_URL), 700);
      }
    }, 250);
  }
})();
