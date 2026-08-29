import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  ErrorCode,
  Kind,
  MemoryBackend,
  RecordingDispatcher,
  SSPError,
  decodePublishDocument,
  encodePublishDocument,
  encodeSubscriptionsResponseDocument,
  encodeWire,
  handleMessage,
  matches,
  newPublish,
  newSubscribe,
  newSubscriptionsRequest,
  newUnsubscribe,
  normalizeSubscriptions,
  parse,
  parsePublish,
  parseSubscriptionsResponse,
  subscribe,
  unsubscribe,
} from "../dist/index.js";

const testDirectory = dirname(fileURLToPath(import.meta.url));
const fixtureDirectory = resolve(testDirectory, "../../testdata/v1");

async function fixture(name) {
  return JSON.parse(await readFile(resolve(fixtureDirectory, name), "utf8"));
}

function fromHex(value) {
  assert.equal(value.length % 2, 0);
  const result = new Uint8Array(value.length / 2);
  for (let index = 0; index < result.length; index += 1) {
    result[index] = Number.parseInt(value.slice(index * 2, index * 2 + 2), 16);
  }
  return result;
}

function toHex(value) {
  return Array.from(value, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function expectCode(callback, code) {
  assert.throws(callback, (error) => error instanceof SSPError && error.code === code);
}

test("shared Wire fixtures encode and decode byte-for-byte", async () => {
  const fixtureData = await fixture("wire-v1.json");
  const seenKinds = new Set();
  for (const testCase of fixtureData.cases) {
    const wire = fromHex(testCase.wire_hex);
    const message = parse(wire);
    assert.equal(message.kind, testCase.kind);
    assert.equal(toHex(encodeWire(message.kind, message.document)), testCase.wire_hex);
    seenKinds.add(message.kind);
    if (message.kind === Kind.Publish) {
      const publish = parsePublish(wire);
      assert.equal(publish.channel, testCase.channel);
      assert.equal(toHex(publish.contentJson), testCase.content_json_hex);
      const document = encodePublishDocument(publish);
      assert.deepEqual(Array.from(document), Array.from(message.document));
    }
    if (message.kind === Kind.SubscriptionsResponse) {
      assert.deepEqual(parseSubscriptionsResponse(wire), testCase.channels);
    }
  }
  assert.deepEqual([...seenKinds].sort((left, right) => left - right), [1, 2, 3, 4, 5]);
});

test("shared invalid fixtures return the same stable error codes", async () => {
  const fixtureData = await fixture("invalid-v1.json");
  for (const testCase of fixtureData.cases) {
    expectCode(() => parse(fromHex(testCase.wire_hex)), ErrorCode[testCase.expected_code]);
  }
});

test("subscription state fixtures and matching are exact", async () => {
  const fixtureData = await fixture("subscriptions-v1.json");
  for (const testCase of fixtureData.normalization) {
    assert.deepEqual(normalizeSubscriptions(testCase.input), testCase.expected);
  }
  for (const testCase of fixtureData.transitions) {
    const operation = testCase.operation === "subscribe" ? subscribe : unsubscribe;
    assert.deepEqual(operation(testCase.initial, testCase.channel), testCase.expected);
  }
  assert.equal(matches(["news"], "news"), true);
  assert.equal(matches(["news"], "alerts"), false);
  assert.equal(matches(["*"], "anything"), true);
  expectCode(() => matches(["news", "news"], "news"), ErrorCode.INVALID_SUBSCRIPTIONS);
});

test("MemoryBackend keeps concurrent-style updates and retries idempotent", async () => {
  const key = fromHex("021111111111111111111111111111111111111111111111111111111111111111");
  const backend = new MemoryBackend();
  const channels = Array.from({ length: 100 }, (_, index) => `channel-${index}`);
  await Promise.all(channels.map((channel) => backend.subscribe(key, channel)));
  await Promise.all(channels.map((channel) => backend.subscribe(key, channel)));
  assert.equal((await backend.subscriptions(key)).length, 100);
});

test("client owns JSON inputs and Publish parser owns returned bytes", () => {
  const content = new TextEncoder().encode(" {\"n\":1} ");
  const original = toHex(content);
  const wire = newPublish("news", content);
  content[0] = 0x78;
  const publish = parsePublish(wire);
  assert.equal(toHex(publish.contentJson), original);
  publish.contentJson[0] = 0x78;
  assert.equal(toHex(parsePublish(wire).contentJson), original);
});

test("service returns direct responses only for queries and preserves Publish event", async () => {
  const key = fromHex("023333333333333333333333333333333333333333333333333333333333333333");
  const backend = new MemoryBackend();
  const dispatcher = new RecordingDispatcher();
  const subscribeResult = await handleMessage(key, newSubscribe("news"), backend, dispatcher);
  assert.deepEqual(subscribeResult, {});

  const requestResult = await handleMessage(key, newSubscriptionsRequest(), backend, dispatcher);
  assert.ok(requestResult.response);
  assert.deepEqual(parseSubscriptionsResponse(requestResult.response), ["news"]);

  const content = new TextEncoder().encode("  {\"n\":1} \n");
  const publishWire = newPublish("news", content);
  const publishResult = await handleMessage(key, publishWire, backend, dispatcher);
  assert.deepEqual(publishResult, {});
  const events = dispatcher.events();
  assert.equal(events.length, 1);
  assert.equal(toHex(events[0].publisherPublicKey), toHex(key));
  assert.equal(toHex(events[0].contentJson), toHex(content));
  assert.equal(toHex(events[0].wire), toHex(publishWire));

  await handleMessage(key, newUnsubscribe("news"), backend, dispatcher);
  const afterUnsubscribe = await handleMessage(key, newSubscriptionsRequest(), backend, dispatcher);
  assert.deepEqual(parseSubscriptionsResponse(afterUnsubscribe.response), []);
});

test("service rejects a SubscriptionsResponse used as a command", async () => {
  const key = fromHex("023333333333333333333333333333333333333333333333333333333333333333");
  const backend = new MemoryBackend();
  const dispatcher = new RecordingDispatcher();
  const responseWire = encodeWire(Kind.SubscriptionsResponse, encodeSubscriptionsResponseDocument([]));
  await assert.rejects(
    () => handleMessage(key, responseWire, backend, dispatcher),
    (error) => error instanceof SSPError && error.code === ErrorCode.INVALID_WIRE,
  );
});

test("public-key and JSON validation do not leak content", async () => {
  await assert.rejects(
    () => handleMessage(new Uint8Array(33), newSubscriptionsRequest(), new MemoryBackend(), new RecordingDispatcher()),
    (error) => error instanceof SSPError && error.code === ErrorCode.INVALID_PUBLIC_KEY,
  );
  expectCode(() => newPublish("*", new TextEncoder().encode("{}")), ErrorCode.INVALID_CHANNEL);
  expectCode(() => decodePublishDocument(fromHex("82646e657773417b")), ErrorCode.INVALID_JSON);
});

test("strict parser keeps mutated inputs in stable error categories", () => {
  const seed = newPublish("news", new TextEncoder().encode("{}"));
  for (let index = 0; index < 128; index += 1) {
    const mutated = new Uint8Array(seed);
    mutated[index % mutated.length] ^= (index * 29 + 1) & 0xff;
    try {
      parse(mutated);
    } catch (error) {
      assert.ok(error instanceof SSPError);
      assert.ok(Object.values(ErrorCode).includes(error.code));
    }
  }
});
