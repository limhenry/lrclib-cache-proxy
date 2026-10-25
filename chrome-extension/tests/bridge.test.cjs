const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const path = require("node:path");

const extensionDir = path.join(__dirname, "..");

function loadMainWorld(payloadByPath) {
  const messages = [];
  const pageWindow = {
    fetch(input) {
      const url = new URL(typeof input === "string" ? input : input.url);
      const payload = payloadByPath.get(url.pathname);
      return Promise.resolve({
        ok: true,
        clone: () => ({ json: () => Promise.resolve(payload) })
      });
    },
    postMessage(message, origin) {
      messages.push({ message, origin });
    }
  };

  class XMLHttpRequestMock {
    open() {}
    send() {}
    addEventListener() {}
  }

  vm.runInNewContext(fs.readFileSync(path.join(extensionDir, "main.js"), "utf8"), {
    window: pageWindow,
    location: { href: "https://music.apple.com/my/album/example", origin: "https://music.apple.com" },
    XMLHttpRequest: XMLHttpRequestMock,
    URL
  });
  return { pageWindow, messages };
}

test("pairs lyrics response with album track metadata in memory", async () => {
  const catalogId = "6812476964";
  const album = {
    data: [{
      id: "6812476961",
      attributes: { name: "Example Album" },
      relationships: {
        tracks: {
          data: [{
            id: catalogId,
            attributes: {
              artistName: "Example Artist",
              name: "Example Song",
              albumName: "Example Album",
              durationInMillis: 161733
            }
          }]
        }
      }
    }]
  };
  const lyrics = {
    data: [{
      id: catalogId,
      attributes: {
        playParams: { catalogId },
        ttmlLocalizations: "<tt><body><p begin='1'>Synthetic lyric</p></body></tt>"
      }
    }]
  };
  const { pageWindow, messages } = loadMainWorld(new Map([
    ["/v1/catalog/my/albums", album],
    [`/v1/catalog/my/songs/${catalogId}/syllable-lyrics`, lyrics]
  ]));

  await pageWindow.fetch(`https://amp-api.music.apple.com/v1/catalog/my/albums?include=tracks&ids=6812476961`);
  await pageWindow.fetch(`https://amp-api.music.apple.com/v1/catalog/my/songs/${catalogId}/syllable-lyrics?extend=ttmlLocalizations`);
  await new Promise(setImmediate);

  assert.equal(messages.length, 2);
  const albumMessage = messages.find(({ message }) => message.kind === "album-tracks").message;
  assert.equal(albumMessage.tracks[0].catalogId, catalogId);
  assert.equal(albumMessage.tracks[0].artistName, "Example Artist");
  assert.equal(albumMessage.tracks[0].trackName, "Example Song");
  assert.equal(albumMessage.tracks[0].albumName, "Example Album");
  assert.equal(albumMessage.tracks[0].duration, 162);

  const lyricsMessage = messages.find(({ message }) => message.kind === "lyrics").message;
  assert.equal(lyricsMessage.catalogId, catalogId);
  assert.equal(lyricsMessage.items[0].ttmlLocalizations, lyrics.data[0].attributes.ttmlLocalizations);
});

test("service worker uploads metadata and LRC without a catalog ID", async () => {
  let messageListener;
  const requests = [];
  const chrome = { runtime: { onMessage: { addListener(listener) { messageListener = listener; } } } };
  const fetch = async (url, options) => {
    requests.push({ url: String(url), options });
    return { ok: true, status: 201, json: async () => ({ ok: true }) };
  };

  vm.runInNewContext(fs.readFileSync(path.join(extensionDir, "service-worker.js"), "utf8"), {
    chrome,
    fetch,
    URL,
    URLSearchParams
  });

  const metadata = {
    artistName: "Example Artist",
    trackName: "Example Song",
    albumName: "Example Album",
    duration: 162
  };
  const response = await new Promise((resolve) => {
    messageListener({ type: "upload-lyrics", metadata, lyrics: "[00:01.00] Synthetic lyric" },
      { url: "https://music.apple.com/my/album/example" }, resolve);
  });

  assert.equal(response.ok, true);
  assert.equal(response.unchanged, false);
  assert.equal(requests.length, 1);
  const uploadURL = new URL(requests[0].url);
  assert.equal(uploadURL.pathname, "/api/upload");
  assert.equal(uploadURL.searchParams.get("source"), "applemusic");
  assert.equal(uploadURL.searchParams.get("artist_name"), metadata.artistName);
  assert.equal(uploadURL.searchParams.get("track_name"), metadata.trackName);
  assert.equal(uploadURL.searchParams.get("album_name"), metadata.albumName);
  assert.equal(uploadURL.searchParams.get("duration"), String(metadata.duration));
  assert.equal(uploadURL.searchParams.has("trackId"), false);
  assert.equal(requests[0].options.body, "[00:01.00] Synthetic lyric");
});