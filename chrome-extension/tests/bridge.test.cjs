const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const path = require("node:path");

const extensionDir = path.join(__dirname, "..");

function loadMainWorld(payloadByPath) {
  const messages = [];
  const logs = [];
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
    console: { log: (...args) => logs.push(args) },
    URL
  });
  return { pageWindow, messages, logs };
}

function loadContentBridge() {
  let messageListener;
  const uploads = [];
  const pageWindow = {
    addEventListener(type, listener) {
      if (type === "message") messageListener = listener;
    }
  };
  vm.runInNewContext(fs.readFileSync(path.join(extensionDir, "content.js"), "utf8"), {
    window: pageWindow,
    location: { origin: "https://music.apple.com" },
    AppleMusicLyricsConverter: {
      convertPayload: (_payload, catalogId) => ({ trackId: catalogId, lyrics: "[00:01.00] Synthetic lyric" })
    },
    chrome: {
      runtime: {
        sendMessage: async (message) => {
          uploads.push(message);
          return { ok: true, unchanged: false };
        }
      }
    },
    Blob,
    console: { info() {}, log() {}, error() {} }
  });

  return {
    uploads,
    dispatch: (data) => messageListener({ source: pageWindow, origin: "https://music.apple.com", data })
  };
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
  const { pageWindow, messages, logs } = loadMainWorld(new Map([
    ["/v1/catalog/my/albums", album],
    [`/v1/catalog/my/songs/${catalogId}/syllable-lyrics`, lyrics]
  ]));

  await pageWindow.fetch(`https://amp-api.music.apple.com/v1/catalog/my/albums?include=tracks&ids=6812476961`);
  await pageWindow.fetch(`https://amp-api.music.apple.com/v1/catalog/my/songs/${catalogId}/syllable-lyrics?extend=ttmlLocalizations`);
  await new Promise(setImmediate);

  assert.equal(messages.length, 2);
  const albumMessage = messages.find(({ message }) => message.kind === "collection-tracks").message;
  assert.equal(albumMessage.tracks[0].catalogId, catalogId);
  assert.equal(albumMessage.tracks[0].artistName, "Example Artist");
  assert.equal(albumMessage.tracks[0].trackName, "Example Song");
  assert.equal(albumMessage.tracks[0].albumName, "Example Album");
  assert.equal(albumMessage.tracks[0].duration, 162);
  assert.equal(logs.length, 1);
  assert.equal(logs[0][0], "[Apple Music Lyrics Bridge] Received album track metadata");
  assert.equal(logs[0][1].tracks[0].trackName, "Example Song");
  assert.equal("catalogId" in logs[0][1].tracks[0], false);

  const lyricsMessage = messages.find(({ message }) => message.kind === "lyrics").message;
  assert.equal(lyricsMessage.catalogId, catalogId);
  assert.equal(lyricsMessage.items[0].ttmlLocalizations, lyrics.data[0].attributes.ttmlLocalizations);
});

test("extracts metadata from a library playlist using its catalog song ID", async () => {
  const catalogId = "6812476964";
  const playlistId = "p.PkxV8JPu2xVE1YG";
  const playlist = {
    data: [{
      id: playlistId,
      attributes: { name: "Example Playlist" },
      relationships: {
        tracks: {
          data: [{
            id: "a.6812476964",
            type: "library-songs",
            attributes: {
              albumName: "Example Album",
              artistName: "Example Artist",
              durationInMillis: 161733,
              name: "Example Song",
              playParams: { catalogId, id: "a.6812476964", kind: "song" }
            }
          }]
        }
      }
    }]
  };
  const { pageWindow, messages, logs } = loadMainWorld(new Map([
    [`/v1/me/library/playlists/${playlistId}`, playlist]
  ]));

  await pageWindow.fetch(`https://amp-api.music.apple.com/v1/me/library/playlists/${playlistId}?l=en-US&include=tracks&extend=hasCollaboration`);
  await new Promise(setImmediate);

  assert.equal(messages.length, 1);
  const message = messages[0].message;
  assert.equal(message.kind, "collection-tracks");
  assert.equal(message.tracks[0].catalogId, catalogId);
  assert.equal(message.tracks[0].artistName, "Example Artist");
  assert.equal(message.tracks[0].trackName, "Example Song");
  assert.equal(message.tracks[0].albumName, "Example Album");
  assert.equal(message.tracks[0].duration, 162);
  assert.equal(logs.length, 1);
  assert.equal(logs[0][0], "[Apple Music Lyrics Bridge] Received playlist track metadata");
});

test("extracts metadata from a catalog playlist response", async () => {
  const playlistId = "pl.pm-d5779e520ff52d7f2da23c375ba13042";
  const playlist = {
    data: [{
      id: playlistId,
      type: "playlists",
      attributes: { name: "Your Essentials" },
      relationships: {
        tracks: {
          data: [{
            id: "6812635409",
            type: "songs",
            attributes: {
              albumName: "The Land of Maybe",
              artistName: "Hebe Tien",
              durationInMillis: 202307,
              name: "Time Well Wasted",
              playParams: { id: "6812635409", kind: "song" }
            }
          }]
        }
      }
    }]
  };
  const { pageWindow, messages, logs } = loadMainWorld(new Map([
    ["/v1/catalog/my/playlists", playlist]
  ]));

  await pageWindow.fetch(`https://amp-api.music.apple.com/v1/catalog/my/playlists?include=tracks&ids=${playlistId}`);
  await new Promise(setImmediate);

  assert.equal(messages.length, 1);
  const message = messages[0].message;
  assert.equal(message.kind, "collection-tracks");
  assert.equal(message.tracks[0].catalogId, "6812635409");
  assert.equal(message.tracks[0].artistName, "Hebe Tien");
  assert.equal(message.tracks[0].trackName, "Time Well Wasted");
  assert.equal(message.tracks[0].albumName, "The Land of Maybe");
  assert.equal(message.tracks[0].duration, 202);
  assert.equal(logs[0][0], "[Apple Music Lyrics Bridge] Received playlist track metadata");
});

test("extracts metadata from the provided playlist response sample", async () => {
  const playlist = JSON.parse(fs.readFileSync(path.join(__dirname, "../../.example/playlist.json"), "utf8"));
  const playlistId = playlist.data[0].id;
  const { pageWindow, messages, logs } = loadMainWorld(new Map([
    [`/v1/me/library/playlists/${playlistId}`, playlist]
  ]));

  await pageWindow.fetch(`https://amp-api.music.apple.com/v1/me/library/playlists/${playlistId}?l=en-US&include=tracks&extend=hasCollaboration`);
  await new Promise(setImmediate);

  const tracks = messages[0]?.message?.tracks || [];
  const song = tracks.find((track) => track.catalogId === "1787701119");
  assert.ok(song, "expected catalog song from playlist sample");
  assert.equal(song.artistName, "Mayday");
  assert.equal(song.trackName, "任性 (電視劇《難哄》主題曲)");
  assert.equal(song.albumName, "任性 (電視劇《難哄》主題曲) - Single");
  assert.equal(song.duration, 265);
  assert.equal(logs[0][0], "[Apple Music Lyrics Bridge] Received playlist track metadata");
});

test("pairs pending lyrics when playlist metadata arrives afterward", async () => {
  const { uploads, dispatch } = loadContentBridge();
  const catalogId = "1787701119";
  const metadata = {
    catalogId,
    artistName: "Mayday",
    trackName: "Example Song",
    albumName: "Example Album",
    duration: 265
  };

  await dispatch({
    type: "apple-music-lyrics-bridge",
    kind: "lyrics",
    catalogId,
    items: [{ ttmlLocalizations: "<tt/>" }]
  });
  await dispatch({
    type: "apple-music-lyrics-bridge",
    kind: "collection-tracks",
    tracks: [metadata]
  });

  assert.equal(uploads.length, 1);
  assert.equal(uploads[0].metadata.artistName, "Mayday");
  assert.equal(uploads[0].metadata.trackName, "Example Song");
  assert.equal(uploads[0].metadata.albumName, "Example Album");
  assert.equal(uploads[0].metadata.duration, 265);
  assert.equal("catalogId" in uploads[0], false);
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