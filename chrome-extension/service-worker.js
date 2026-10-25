const proxyBaseURL = "http://localhost:3000";
const pendingUploads = new Map();
const lastUploadedLyrics = new Map();

function isAppleMusicPage(url) {
  try {
    return new URL(url).hostname === "music.apple.com";
  } catch {
    return false;
  }
}

function metadataParams(metadata) {
  return new URLSearchParams({
    artist_name: metadata.artistName,
    track_name: metadata.trackName,
    album_name: metadata.albumName,
    duration: String(metadata.duration)
  });
}

function metadataKey(metadata) {
  return [metadata.artistName, metadata.trackName, metadata.albumName, metadata.duration]
    .map((value) => String(value).trim().toLowerCase())
    .join("\u001f");
}

async function uploadIfChanged(metadata, lyrics) {
  const key = metadataKey(metadata);
  if (lastUploadedLyrics.get(key) === lyrics) return { ok: true, unchanged: true };

  const params = metadataParams(metadata);
  const uploadParams = new URLSearchParams(params);
  uploadParams.set("source", "applemusic");
  const uploadResponse = await fetch(
    `${proxyBaseURL}/api/upload?${uploadParams}`,
    {
      method: "POST",
      headers: { "Content-Type": "text/plain; charset=utf-8" },
      body: lyrics,
      credentials: "omit"
    }
  );
  if (!uploadResponse.ok) {
    let message = `Upload returned HTTP ${uploadResponse.status}`;
    try {
      const body = await uploadResponse.json();
      if (body.message) message = body.message;
    } catch {
      // Keep the HTTP status message when the proxy response is not JSON.
    }
    throw new Error(message);
  }
  lastUploadedLyrics.set(key, lyrics);
  return { ok: true, unchanged: false };
}

function queueUpload(metadata, lyrics) {
  const key = metadataKey(metadata);
  const previous = pendingUploads.get(key) || Promise.resolve();
  const task = previous.catch(() => {}).then(() => uploadIfChanged(metadata, lyrics));
  pendingUploads.set(key, task);

  return task.finally(() => {
    if (pendingUploads.get(key) === task) pendingUploads.delete(key);
  });
}

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message?.type !== "upload-lyrics") return;
  if (!isAppleMusicPage(sender.url)) {
    sendResponse({ ok: false, error: "Lyrics uploads are only accepted from music.apple.com." });
    return;
  }

  const metadata = message.metadata || {};
  const lyrics = message.lyrics;
  if (typeof metadata.artistName !== "string" || !metadata.artistName.trim() || metadata.artistName.toLowerCase() === "unknown" ||
      typeof metadata.trackName !== "string" || !metadata.trackName.trim() ||
      typeof metadata.albumName !== "string" || !metadata.albumName.trim() ||
      !Number.isInteger(metadata.duration) || metadata.duration < 0 ||
      typeof lyrics !== "string" || lyrics.trim() === "") {
    sendResponse({ ok: false, error: "Valid artist, track, album, duration, and nonempty LRC are required." });
    return;
  }

  queueUpload(metadata, lyrics)
    .then(sendResponse)
    .catch((error) => sendResponse({ ok: false, error: error.message }));
  return true;
});