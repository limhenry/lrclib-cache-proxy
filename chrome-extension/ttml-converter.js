(() => {
  function parseXML(xml, Parser = globalThis.DOMParser) {
    if (typeof Parser !== "function") throw new Error("DOMParser is unavailable");

    let parseError = false;
    let document;
    try {
      const parser = new Parser({
        onError: (level) => { if (level === "error" || level === "fatalError") parseError = true; },
        errorHandler: {
          warning: () => {},
          error: () => { parseError = true; },
          fatalError: () => { parseError = true; }
        }
      });
      document = parser.parseFromString(xml, "application/xml");
    } catch {
      throw new Error("Invalid TTML XML");
    }

    const parserErrors = Array.from(document?.getElementsByTagName?.("*") || [])
      .some((element) => element.localName === "parsererror");
    if (!document?.documentElement || parseError || parserErrors) {
      throw new Error("Invalid TTML XML");
    }
    return document;
  }

  function findElements(parent, localName) {
    return Array.from(parent.getElementsByTagName("*"))
      .filter((element) => element.localName === localName || element.nodeName === localName || element.nodeName.endsWith(`:${localName}`));
  }

  function parseClockTime(value) {
    const input = String(value || "").trim();
    const offset = input.match(/^(\d+(?:\.\d+)?)(ms|s)$/i);
    if (offset) {
      const amount = Number(offset[1]);
      return offset[2].toLowerCase() === "ms" ? amount : amount * 1000;
    }

    const parts = input.split(":");
    if (parts.length === 1 && /^\d+(?:\.\d+)?$/.test(parts[0])) {
      return Number(parts[0]) * 1000;
    }
    if (parts.length !== 2 && parts.length !== 3) return null;
    if (!parts.every((part) => /^\d+(?:\.\d+)?$/.test(part))) return null;

    const seconds = Number(parts[parts.length - 1]);
    const minutes = Number(parts[parts.length - 2]);
    const hours = parts.length === 3 ? Number(parts[0]) : 0;
    if (seconds >= 60 || (parts.length === 3 && minutes >= 60)) return null;
    return (hours * 3600 + minutes * 60 + seconds) * 1000;
  }

  function formatLrcTimestamp(milliseconds) {
    const totalCentiseconds = Math.round(milliseconds / 10);
    const minutes = Math.floor(totalCentiseconds / 6000);
    const seconds = Math.floor(totalCentiseconds / 100) % 60;
    const centiseconds = totalCentiseconds % 100;
    return `[${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}.${String(centiseconds).padStart(2, "0")}]`;
  }

  function convertTTML(ttml, Parser = globalThis.DOMParser) {
    if (typeof ttml !== "string" || ttml.trim() === "") return null;
    const document = parseXML(ttml, Parser);
    const bodies = findElements(document, "body");
    if (bodies.length === 0) throw new Error("TTML body not found");

    const paragraphs = findElements(bodies[0], "p");
    const timedLines = paragraphs.map((paragraph, index) => {
      const start = parseClockTime(paragraph.getAttribute("begin"));
      const text = String(paragraph.textContent || "").replace(/[\t\n\r ]+/g, " ").trim();
      if (start === null || text === "") return null;
      return { start, index, text };
    }).filter(Boolean);

    timedLines.sort((left, right) => left.start - right.start || left.index - right.index);
    if (timedLines.length === 0) return null;
    return timedLines.map(({ start, text }) => `${formatLrcTimestamp(start)}${text}`).join("\n");
  }

  function convertPayload(payload, requestedTrackId, Parser = globalThis.DOMParser) {
    const trackId = String(requestedTrackId || "");
    if (!/^\d+$/.test(trackId) || !Array.isArray(payload?.data)) return null;

    const item = payload.data.find((entry) => {
      const catalogId = entry?.catalogId ?? entry?.attributes?.playParams?.catalogId;
      return String(entry?.id || "") === trackId || String(catalogId || "") === trackId;
    });
    if (!item || typeof item.ttmlLocalizations !== "string") return null;

    const lyrics = convertTTML(item.ttmlLocalizations, Parser);
    return lyrics ? { trackId, lyrics } : null;
  }

  const converter = { convertTTML, convertPayload, parseClockTime };
  globalThis.AppleMusicLyricsConverter = converter;
  if (typeof module !== "undefined" && module.exports) module.exports = converter;
})();