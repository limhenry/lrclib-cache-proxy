(() => {
  const messageType = "apple-music-lyrics-bridge";
  const inProgress = new Map();

  window.addEventListener("message", async (event) => {
    if (event.source !== window || event.origin !== location.origin) return;
    const message = event.data;
    if (message?.type !== messageType || !/^\d+$/.test(String(message.trackId || ""))) return;

    const trackId = String(message.trackId);
    let lyrics;
    try {
      const result = globalThis.AppleMusicLyricsConverter.convertPayload(
        { data: message.items },
        trackId
      );
      if (!result) {
        console.info(`[Apple Music Lyrics Bridge] No convertible lyrics for track ${trackId}.`);
        return;
      }
      lyrics = result.lyrics;
      if (inProgress.get(trackId) === lyrics) {
        console.info(`[Apple Music Lyrics Bridge] Identical upload already in progress for track ${trackId}.`);
        return;
      }
      if (new Blob([lyrics]).size > 1 << 20) {
        throw new Error("Converted LRC exceeds the proxy's 1 MB upload limit");
      }
      inProgress.set(trackId, lyrics);

      console.info(`[Apple Music Lyrics Bridge] Detected lyrics for track ${trackId}.`);
      console.log(`[Apple Music Lyrics Bridge] Converted LRC for track ${trackId}:\n${lyrics}`);

      const upload = await chrome.runtime.sendMessage({
        type: "upload-lyrics",
        trackId,
        lyrics
      });
      if (!upload?.ok) throw new Error(upload?.error || "Upload failed");
      if (upload.unchanged) {
        console.info(`[Apple Music Lyrics Bridge] Track ${trackId} is already up to date.`);
      } else {
        console.info(`[Apple Music Lyrics Bridge] Uploaded lyrics for track ${trackId}.`);
      }
    } catch (error) {
      console.error(`[Apple Music Lyrics Bridge] Track ${trackId}: ${error.message}`);
    } finally {
      if (inProgress.get(trackId) === lyrics) inProgress.delete(trackId);
    }
  });
})();