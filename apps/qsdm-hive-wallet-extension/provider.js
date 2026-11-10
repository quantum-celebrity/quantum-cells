(() => {
  "use strict";

  if (window.CELLS?.isCELLSHive) return;

  const REQUEST_SOURCE = "CELLS-hive-provider-request";
  const RESPONSE_SOURCE = "CELLS-hive-provider-response";
  const pending = new Map();
  const listeners = new Map();

  const emit = (event, value) => {
    const callbacks = listeners.get(event) || [];
    callbacks.forEach((callback) => callback(value));
  };

  window.addEventListener("message", (event) => {
    if (
      event.source !== window ||
      event.origin !== window.location.origin ||
      event.data?.source !== RESPONSE_SOURCE ||
      typeof event.data?.id !== "string"
    ) {
      return;
    }
    const request = pending.get(event.data.id);
    if (!request) return;
    pending.delete(event.data.id);
    clearTimeout(request.timeout);
    if (event.data.ok) {
      if (request.method === "CELLS_requestAccounts") {
        emit("accountsChanged", event.data.result);
      } else if (request.method === "CELLS_disconnect") {
        emit("accountsChanged", []);
      }
      request.resolve(event.data.result);
    } else {
      const error = new Error(event.data.error || "CELLS wallet request failed");
      // 4001: the person cancelled it in the CELLS Wallet window.
      if (Number.isInteger(event.data.code)) error.code = event.data.code;
      request.reject(error);
    }
  });

  // Requests a person answers in the CELLS Wallet window and then in CELLS Hive
  // get more time than the others.
  const INTERACTIVE = new Set([
    "CELLS_requestAccounts",
    "CELLS_sendTransaction",
    "CELLS_signMessage",
  ]);

  const provider = Object.freeze({
    isCELLSHive: true,
    version: "CELLS-provider/v1",
    request({ method, params } = {}) {
      if (typeof method !== "string" || !method.startsWith("CELLS_")) {
        return Promise.reject(
          new Error("A valid CELLS provider method is required")
        );
      }
      const id = crypto.randomUUID();
      return new Promise((resolve, reject) => {
        const timeout = setTimeout(
          () => {
            pending.delete(id);
            reject(new Error("CELLS Hive did not answer the wallet request"));
          },
          INTERACTIVE.has(method) ? 15 * 60 * 1000 : 125000
        );
        pending.set(id, { resolve, reject, timeout, method });
        window.postMessage(
          { source: REQUEST_SOURCE, id, method, params },
          window.location.origin
        );
      });
    },
    on(event, callback) {
      if (typeof callback !== "function") return provider;
      listeners.set(event, [...(listeners.get(event) || []), callback]);
      return provider;
    },
    removeListener(event, callback) {
      listeners.set(
        event,
        (listeners.get(event) || []).filter((entry) => entry !== callback)
      );
      return provider;
    },
  });

  Object.defineProperty(window, "CELLS", {
    value: provider,
    configurable: false,
    enumerable: false,
    writable: false,
  });
  window.dispatchEvent(new Event("CELLS#initialized"));
})();
