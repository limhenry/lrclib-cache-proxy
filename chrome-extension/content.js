(() => {
  const messageType = "apple-music-lyrics-bridge";
  const metadataByCatalogId = new Map();
  const pendingLyricsByCatalogId = new Map();
  const uploadsInProgress = new Map();

  function metadataKey(metadata) {
    return [metadata.artistName, metadata.trackName, metadata.albumName, metadata.duration]
      .map((value) => String(value).trim().toLowerCase())
      .join("\u001f");
  }

  function trimMap(map, maxSize) {
    while (map.size > maxSize) map.delete(map.keys().next().value);
  }

  async function convertAndUpload(catalogId, items) {
    const metadata = metadataByCatalogId.get(catalogId);
    if (!metadata) {
      pendingLyricsByCatalogId.set(catalogId, items);
      trimMap(pendingLyricsByCatalogId, 100);
      console.info("[Apple Music Lyrics Bridge] Lyrics received; waiting for album track metadata.");
      return;
    }

    let lyrics;
    const key = metadataKey(metadata);
    try {
      const result = globalThis.AppleMusicLyricsConverter.convertPayload({ data: items }, catalogId);
      if (!result) {
        console.info(`[Apple Music Lyrics Bridge] No convertible lyrics for ${metadata.artistName} - ${metadata.trackName}.`);
        return;
      }
      lyrics = result.lyrics;
      if (uploadsInProgress.get(key) === lyrics) {
        console.info(`[Apple Music Lyrics Bridge] Identical upload already in progress for ${metadata.trackName}.`);
        return;
      }
      if (new Blob([lyrics]).size > 1 << 20) {
        throw new Error("Converted LRC exceeds the proxy's 1 MB upload limit");
      }
      uploadsInProgress.set(key, lyrics);

      console.info(`[Apple Music Lyrics Bridge] Detected lyrics for ${metadata.artistName} - ${metadata.trackName}.`);
      console.log(`[Apple Music Lyrics Bridge] Converted LRC for ${metadata.trackName}:\n${lyrics}`);

      const upload = await chrome.runtime.sendMessage({
        type: "upload-lyrics",
        metadata,
        lyrics
      });
      if (!upload?.ok) throw new Error(upload?.error || "Upload failed");
      if (upload.unchanged) {
        console.info(`[Apple Music Lyrics Bridge] ${metadata.trackName} is already up to date.`);
      } else {
        console.info(`[Apple Music Lyrics Bridge] Uploaded lyrics for ${metadata.artistName} - ${metadata.trackName}.`);
      }
    } catch (error) {
      console.error(`[Apple Music Lyrics Bridge] ${metadata.trackName}: ${error.message}`);
    } finally {
      if (uploadsInProgress.get(key) === lyrics) uploadsInProgress.delete(key);
    }
  }

  window.addEventListener("message", async (event) => {
    if (event.source !== window || event.origin !== location.origin) return;
    const message = event.data;
    if (message?.type !== messageType) return;

    if (message.kind === "collection-tracks" && Array.isArray(message.tracks)) {
      for (const metadata of message.tracks) {
        const catalogId = String(metadata?.catalogId || "");
        if (!/^\d+$/.test(catalogId) || !metadata.artistName || !metadata.trackName || !metadata.albumName || !Number.isFinite(metadata.duration)) continue;
        metadataByCatalogId.set(catalogId, metadata);
        const pending = pendingLyricsByCatalogId.get(catalogId);
        if (pending) {
          pendingLyricsByCatalogId.delete(catalogId);
          void convertAndUpload(catalogId, pending);
        }
      }
      trimMap(metadataByCatalogId, 500);
      return;
    }

    if (message.kind === "lyrics" && /^\d+$/.test(String(message.catalogId || "")) && Array.isArray(message.items)) {
      await convertAndUpload(String(message.catalogId), message.items);
    }
  });
})();