(() => {
  const messageType = "apple-music-lyrics-bridge";
  const apiHost = "amp-api.music.apple.com";
  const xhrRequests = new WeakMap();

  function watchedRequest(input, init) {
    try {
      const requestURL = new URL(typeof input === "string" ? input : input.url, location.href);
      const method = String(init?.method || input?.method || "GET").toUpperCase();
      if (requestURL.hostname !== apiHost || method !== "GET") return null;

      const lyricsMatch = requestURL.pathname.match(/^\/v1\/catalog\/[^/]+\/songs\/(\d+)\/syllable-lyrics\/?$/);
      if (lyricsMatch) return { kind: "lyrics", catalogId: lyricsMatch[1] };

      if (/^\/v1\/catalog\/[^/]+\/albums\/?$/.test(requestURL.pathname) &&
          requestURL.searchParams.get("include")?.split(",").includes("tracks")) {
        const albumIDs = (requestURL.searchParams.get("ids") || "").split(",").filter(Boolean);
        if (albumIDs.length > 0) return { kind: "album-tracks", albumIDs };
      }
      return null;
    } catch {
      return null;
    }
  }

  function postMatchingLyrics(catalogId, payload) {
    if (!Array.isArray(payload?.data)) return;

    const matchingItems = payload.data
      .filter((item) => {
        const itemCatalogId = item?.attributes?.playParams?.catalogId;
        return String(item?.id || "") === catalogId || String(itemCatalogId || "") === catalogId;
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
      kind: "lyrics",
      catalogId,
      items: matchingItems
    }, location.origin);
  }

  function postAlbumTracks(albumIDs, payload) {
    if (!Array.isArray(payload?.data)) return;
    const requestedAlbums = new Set(albumIDs);
    const tracks = [];

    for (const album of payload.data) {
      if (!requestedAlbums.has(String(album?.id || ""))) continue;
      const albumName = album?.attributes?.name || "";
      for (const track of album?.relationships?.tracks?.data || []) {
        const attributes = track?.attributes || {};
        const durationInMillis = Number(attributes.durationInMillis);
        const artistName = String(attributes.artistName || "").trim();
        const trackName = String(attributes.name || "").trim();
        const resolvedAlbumName = String(attributes.albumName || albumName).trim();
        if (!track?.id || !artistName || artistName.toLowerCase() === "unknown" || !trackName || !resolvedAlbumName || !Number.isFinite(durationInMillis) || durationInMillis <= 0) continue;

        tracks.push({
          catalogId: String(track.id),
          artistName,
          trackName,
          albumName: resolvedAlbumName,
          duration: Math.round(durationInMillis / 1000)
        });
      }
    }

    if (tracks.length === 0) return;
    window.postMessage({ type: messageType, kind: "album-tracks", tracks }, location.origin);
  }

  function handleResponse(request, response) {
    if (!request || !response.ok) return;
    response.clone().json().then((payload) => {
      if (request.kind === "lyrics") postMatchingLyrics(request.catalogId, payload);
      if (request.kind === "album-tracks") postAlbumTracks(request.albumIDs, payload);
    }).catch(() => {});
  }

  const originalFetch = window.fetch;
  window.fetch = function (input, init) {
    const request = watchedRequest(input, init);
    const responsePromise = Reflect.apply(originalFetch, this, arguments);
    if (!request) return responsePromise;
    return responsePromise.then((response) => {
      handleResponse(request, response);
      return response;
    });
  };

  const xhrOpen = XMLHttpRequest.prototype.open;
  const xhrSend = XMLHttpRequest.prototype.send;

  XMLHttpRequest.prototype.open = function (method, url) {
    xhrRequests.set(this, watchedRequest(url, { method }));
    return Reflect.apply(xhrOpen, this, arguments);
  };

  XMLHttpRequest.prototype.send = function () {
    const request = xhrRequests.get(this);
    if (request) {
      this.addEventListener("load", () => {
        if (this.status < 200 || this.status >= 300) return;
        try {
          const payload = this.responseType === "json" ? this.response : JSON.parse(this.responseText);
          if (request.kind === "lyrics") postMatchingLyrics(request.catalogId, payload);
          if (request.kind === "album-tracks") postAlbumTracks(request.albumIDs, payload);
        } catch {
          // Ignore responses that are not readable JSON.
        }
      }, { once: true });
    }
    return Reflect.apply(xhrSend, this, arguments);
  };
})();