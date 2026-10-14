const { test } = require("node:test");
const assert = require("node:assert/strict");
const { registryDigest } = require("./registry.cjs");
const digest = "sha256:" + "a".repeat(64);
test("distinguishes a missing manifest from authentication and registry failures", async () => {
  for (const status of [200, 404, 401, 403, 429, 500]) {
    const calls = [];
    const fetch = async (url, options) => {
      calls.push({ url, options });
      if (calls.length === 1)
        return new Response(JSON.stringify({ token: "scoped-token" }));
      return new Response(null, {
        status,
        headers: { "docker-content-digest": digest },
      });
    };
    const promise = registryDigest({
      owner: "msoldin",
      repo: "porty",
      tag: "v0.1.0",
      actor: "test",
      token: "secret",
      fetch,
    });
    if (status === 200) assert.equal(await promise, digest);
    else if (status === 404) assert.equal(await promise, null);
    else await assert.rejects(promise, /Registry request failed/);
    assert.equal(calls[1].options.method, "HEAD");
  }
});
test("rejects failed token lookup and malformed registry digests", async () => {
  await assert.rejects(
    registryDigest({
      owner: "msoldin",
      repo: "porty",
      tag: "v0.1.0",
      fetch: async () => new Response(null, { status: 403 }),
    }),
    /authentication/,
  );
  let count = 0;
  await assert.rejects(
    registryDigest({
      owner: "msoldin",
      repo: "porty",
      tag: "v0.1.0",
      fetch: async () =>
        ++count === 1
          ? new Response('{"token":"x"}')
          : new Response(null, { headers: { "docker-content-digest": "bad" } }),
    }),
    /digest/,
  );
});
