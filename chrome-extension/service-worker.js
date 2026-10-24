const proxyBaseURL = "http://localhost:3000";
const pendingUploads = new Map();

function isAppleMusicPage(url) {
  try {
    return new URL(url).hostname === "music.apple.com";
  } catch {
    return false;
  }
}

async function uploadIfChanged(trackId, lyrics) {
  const lookupURL = `${proxyBaseURL}/api/get?trackId=${encodeURIComponent(trackId)}`;
  const existingResponse = await fetch(lookupURL, { method: "GET", credentials: "omit" });
  if (existingResponse.ok) {
    const existing = await existingResponse.json();
    if (existing.syncedLyrics === lyrics) return { ok: true, unchanged: true };
  } else if (existingResponse.status !== 404) {
    throw new Error(`Proxy lookup returned HTTP ${existingResponse.status}`);
  }

  const uploadResponse = await fetch(
    `${proxyBaseURL}/api/upload?trackId=${encodeURIComponent(trackId)}`,
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
  return { ok: true, unchanged: false };
}

function queueUpload(trackId, lyrics) {
  const previous = pendingUploads.get(trackId) || Promise.resolve();
  const task = previous.catch(() => {}).then(() => uploadIfChanged(trackId, lyrics));
  pendingUploads.set(trackId, task);

  return task.finally(() => {
    if (pendingUploads.get(trackId) === task) pendingUploads.delete(trackId);
  });
}

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message?.type !== "upload-lyrics") return;
  if (!isAppleMusicPage(sender.url)) {
    sendResponse({ ok: false, error: "Lyrics uploads are only accepted from music.apple.com." });
    return;
  }

  const trackId = String(message.trackId || "");
  const lyrics = message.lyrics;
  if (!/^\d{1,19}$/.test(trackId) || typeof lyrics !== "string" || lyrics.trim() === "") {
    sendResponse({ ok: false, error: "Invalid track ID or empty LRC." });
    return;
  }

  queueUpload(trackId, lyrics)
    .then(sendResponse)
    .catch((error) => sendResponse({ ok: false, error: error.message }));
  return true;
});