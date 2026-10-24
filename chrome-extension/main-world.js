(() => {
  const messageType = "apple-music-lyrics-bridge";
  const apiHost = "amp-api.music.apple.com";
  const xhrRequests = new WeakMap();

  function lyricsRequest(input, init) {
    try {
      const requestURL = new URL(typeof input === "string" ? input : input.url, location.href);
      const method = String(init?.method || input?.method || "GET").toUpperCase();
      if (requestURL.hostname !== apiHost || method !== "GET") return null;

      const match = requestURL.pathname.match(/^\/v1\/catalog\/[^/]+\/songs\/(\d+)\/syllable-lyrics\/?$/);
      return match ? match[1] : null;
    } catch {
      return null;
    }
  }

  function postMatchingLyrics(trackId, payload) {
    if (!Array.isArray(payload?.data)) return;

    const matchingItems = payload.data
      .filter((item) => {
        const catalogId = item?.attributes?.playParams?.catalogId;
        return String(item?.id || "") === trackId || String(catalogId || "") === trackId;
      })
      .map((item) => ({
        id: item.id,
        catalogId: item?.attributes?.playParams?.catalogId,
        ttmlLocalizations: item?.attributes?.ttmlLocalizations
      }))
      .filter((item) => typeof item.ttmlLocalizations === "string");

    if (matchingItems.length === 0) return;

    window.postMessage({
      type: messageType,
      trackId,
      items: matchingItems
    }, location.origin);
  }

  const originalFetch = window.fetch;
  window.fetch = function (input, init) {
    const trackId = lyricsRequest(input, init);
    const responsePromise = Reflect.apply(originalFetch, this, arguments);
    if (!trackId) return responsePromise;

    return responsePromise.then((response) => {
      if (response.ok) {
        response.clone().json()
          .then((payload) => postMatchingLyrics(trackId, payload))
          .catch(() => {});
      }
      return response;
    });
  };

  const xhrOpen = XMLHttpRequest.prototype.open;
  const xhrSend = XMLHttpRequest.prototype.send;

  XMLHttpRequest.prototype.open = function (method, url) {
    xhrRequests.set(this, lyricsRequest(url, { method }));
    return Reflect.apply(xhrOpen, this, arguments);
  };

  XMLHttpRequest.prototype.send = function () {
    const trackId = xhrRequests.get(this);
    if (trackId) {
      this.addEventListener("load", () => {
        if (this.status < 200 || this.status >= 300) return;
        try {
          const payload = this.responseType === "json" ? this.response : JSON.parse(this.responseText);
          postMatchingLyrics(trackId, payload);
        } catch {
          // Ignore responses that are not readable JSON.
        }
      }, { once: true });
    }
    return Reflect.apply(xhrSend, this, arguments);
  };
})();