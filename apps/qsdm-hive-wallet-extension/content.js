(() => {
  "use strict";

  const REQUEST_SOURCE = "CELLS-hive-provider-request";
  const RESPONSE_SOURCE = "CELLS-hive-provider-response";
  const METHODS = new Set([
    "CELLS_requestAccounts",
    "CELLS_accounts",
    "CELLS_getBalance",
    "CELLS_signMessage",
    "CELLS_sendTransaction",
    "CELLS_disconnect",
    "CELLS_openOnboarding",
  ]);

  const postPageResponse = (id, response) =>
    window.postMessage(
      { ...response, source: RESPONSE_SOURCE, id },
      window.location.origin
    );

  chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
    if (
      message?.source !== "CELLS-hive-background-response" ||
      typeof message.id !== "string" ||
      !message.response ||
      typeof message.response !== "object"
    ) {
      return;
    }
    postPageResponse(message.id, message.response);
    sendResponse({ received: true });
  });

  window.addEventListener("message", (event) => {
    if (
      event.source !== window ||
      event.origin !== window.location.origin ||
      event.data?.source !== REQUEST_SOURCE ||
      typeof event.data?.id !== "string"
    ) {
      return;
    }

    const { id, method, params } = event.data;
    if (!METHODS.has(method)) {
      window.postMessage(
        {
          source: RESPONSE_SOURCE,
          id,
          ok: false,
          error: `Unsupported CELLS wallet method: ${String(method)}`,
        },
        window.location.origin
      );
      return;
    }

    chrome.runtime.sendMessage(
      {
        source: "CELLS-hive-content",
        id,
        method,
        params,
      },
      () => {
        const runtimeError = chrome.runtime.lastError;
        if (runtimeError) {
          postPageResponse(id, {
            ok: false,
            error: runtimeError.message,
          });
        }
      }
    );
  });
})();
