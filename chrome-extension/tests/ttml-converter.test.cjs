const test = require("node:test");
const assert = require("node:assert/strict");
const { DOMParser } = require("@xmldom/xmldom");
const { convertTTML, convertPayload, parseClockTime } = require("../ttml-converter.js");

test("converts line-timed TTML and sorts by start time stably", () => {
  const ttml = `<tt xmlns="http://www.w3.org/ns/ttml"><head/><body>
    <div><p begin="1.236">First line</p><p begin="00:00:01.236">Second line</p>
    <p begin="1.1">Earlier line</p></div>
  </body></tt>`;

  assert.equal(
    convertTTML(ttml, DOMParser),
    "[00:01.10]Earlier line\n[00:01.24]First line\n[00:01.24]Second line"
  );
});

test("collapses word timings into one paragraph-timed LRC line", () => {
  const ttml = `<tt xmlns="http://www.w3.org/ns/ttml"><body><p begin="1:02.345">
    <span begin="1:02.345" end="1:02.700">Hello</span>
    <span begin="1:02.700" end="1:03.000"> world</span>
  </p></body></tt>`;

  assert.equal(convertTTML(ttml, DOMParser), "[01:02.35]Hello world");
});

test("excludes pronunciation transliterations from TTML metadata", () => {
  const ttml = `<tt xmlns="http://www.w3.org/ns/ttml"><head><metadata>
    <transliteration><text for="L1">phonetic pronunciation</text></transliteration>
  </metadata></head><body><p begin="3.25">Actual lyric</p></body></tt>`;

    assert.equal(convertTTML(ttml, DOMParser), "[00:03.25]Actual lyric");
});

test("matches the requested catalog ID before converting", () => {
  const payload = {
    data: [
      { id: "123", catalogId: "123", ttmlLocalizations: "<tt><body><p begin='1'>Wrong</p></body></tt>" },
      { id: "6825795628", catalogId: "6825795628", ttmlLocalizations: "<tt><body><p begin='2'>Right</p></body></tt>" }
    ]
  };

  assert.deepEqual(convertPayload(payload, "6825795628", DOMParser), {
    trackId: "6825795628",
    lyrics: "[00:02.00]Right"
  });
});

test("supports seconds and millisecond TTML offsets", () => {
  assert.equal(parseClockTime("1.5s"), 1500);
  assert.equal(parseClockTime("1250ms"), 1250);
  assert.equal(parseClockTime("1:02.345"), 62345);
  assert.equal(parseClockTime("0:01:02.345"), 62345);
  assert.equal(parseClockTime("1:60.000"), null);
});

test("returns null for empty lyrics and rejects malformed XML", () => {
  assert.equal(convertTTML("<tt><body><p begin='1'> </p></body></tt>", DOMParser), null);
  assert.throws(() => convertTTML("<tt><body><p>", DOMParser), /Invalid TTML XML/);
});