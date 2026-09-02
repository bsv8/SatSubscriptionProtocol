import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  ActionErrorCode,
  ErrorCode,
  Kind,
  MAX_AMOUNT_SCALE,
  MAX_CHANNEL_UTF8_BYTES,
  MAX_CONTENT_JSON_BYTES,
  MAX_REQUEST_IDS_PER_SESSION,
  MAX_SUBSCRIPTIONS,
  MemoryBackend,
  RecordingDispatcher,
  SSPError,
  Session,
  decodeActionResultDocument,
  decodePublishDocument,
  encodeActionResultDocument,
  encodePublishDocument,
  encodeSubscriptionsResponseDocument,
  encodeWire,
  matches,
  newActionResult,
  newAmount,
  newPublish,
  newRequestId,
  newSubscribe,
  newSubscriptionsRequest,
  newSubscriptionsResponse,
  newUnsubscribe,
  normalizeSubscriptions,
  parse,
  parseActionResult,
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

function id(value) {
  const result = new Uint8Array(32);
  result.fill(value);
  return result;
}

function expectCode(callback, code) {
  assert.throws(callback, (error) => error instanceof SSPError && error.code === code);
}

function validJsonBytes(byteLength) {
  const result = new Uint8Array(byteLength);
  result[0] = 0x22;
  result.fill(0x61, 1, byteLength - 1);
  result[byteLength - 1] = 0x22;
  return result;
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
    const requestId = fromHex(testCase.request_id_hex);
    if (message.kind === Kind.Publish) {
      const publish = parsePublish(wire);
      assert.equal(toHex(publish.requestId), testCase.request_id_hex);
      assert.equal(publish.channel, testCase.channel);
      assert.equal(toHex(publish.contentJson), testCase.content_json_hex);
      assert.deepEqual(Array.from(encodePublishDocument(publish)), Array.from(message.document));
    }
    if (message.kind === Kind.Subscribe) {
      assert.equal(toHex(decodeRequestIdFromChannel(message.document)), testCase.request_id_hex);
    }
    if (message.kind === Kind.SubscriptionsResponse) {
      const response = parseSubscriptionsResponse(wire);
      assert.equal(toHex(response.requestId), testCase.request_id_hex);
      assert.equal(response.chargedAmount, testCase.charged_amount);
      assert.deepEqual(response.channels, testCase.channels);
    }
    if (message.kind === Kind.ActionResult) {
      const result = parseActionResult(wire);
      assert.equal(toHex(result.requestId), testCase.request_id_hex);
      assert.equal(result.success, testCase.success);
      assert.equal(result.chargedAmount, testCase.charged_amount);
      assert.equal(result.errorCode, testCase.error_code);
    }
    assert.equal(requestId.byteLength, 32);
  }
  assert.deepEqual([...seenKinds].sort((left, right) => left - right), [1, 2, 3, 4, 5, 6]);
});

test("shared invalid fixtures return the same stable error codes", async () => {
  const fixtureData = await fixture("invalid-v1.json");
  for (const testCase of fixtureData.cases) {
    expectCode(() => parse(fromHex(testCase.wire_hex)), ErrorCode[testCase.expected_code]);
  }
});

test("amount validation rejects non-canonical values and accepts exact small units", () => {
  for (const value of ["0", "1", "12", "0.1", "0.001", "12.34", "0.123456789012345678"]) {
    assert.equal(newAmount(value), value);
  }
  for (const value of ["-1", "+1", "00.1", "1.0", "1.2300", ".1", "1.", "1e-3", "0.0000000000000000001"]) {
    expectCode(() => newAmount(value), ErrorCode.INVALID_WIRE);
  }
  assert.equal(MAX_AMOUNT_SCALE, 18);
});

test("frozen protocol size limits accept exact boundary and reject one byte over", () => {
  const channelAtLimit = "a".repeat(MAX_CHANNEL_UTF8_BYTES);
  assert.doesNotThrow(() => newSubscribe(id(1), channelAtLimit));
  expectCode(() => newSubscribe(id(1), `${channelAtLimit}a`), ErrorCode.INVALID_CHANNEL);

  const contentJsonAtLimit = validJsonBytes(MAX_CONTENT_JSON_BYTES);
  assert.equal(contentJsonAtLimit.byteLength, MAX_CONTENT_JSON_BYTES);
  assert.doesNotThrow(() => newPublish(id(1), "news", contentJsonAtLimit));
  expectCode(
    () => newPublish(id(1), "news", validJsonBytes(MAX_CONTENT_JSON_BYTES + 1)),
    ErrorCode.MESSAGE_TOO_LARGE,
  );

  const subscriptionsAtLimit = Array.from(
    { length: MAX_SUBSCRIPTIONS },
    (_, index) => `channel-${String(index).padStart(4, "0")}`,
  );
  assert.equal(normalizeSubscriptions(subscriptionsAtLimit).length, MAX_SUBSCRIPTIONS);
  assert.doesNotThrow(() => encodeSubscriptionsResponseDocument({ requestId: id(1), chargedAmount: "0", channels: subscriptionsAtLimit }));
  expectCode(
    () => normalizeSubscriptions([...subscriptionsAtLimit, "channel-1024"]),
    ErrorCode.INVALID_SUBSCRIPTIONS,
  );
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

test("MemoryBackend accepts request IDs and returns exact free amounts", async () => {
  const key = publicKey(2);
  const backend = new MemoryBackend();
  await Promise.all(Array.from({ length: 100 }, (_, index) => backend.subscribe(key, id(index % 256), `channel-${index}`)));
  const result = await backend.subscriptions(key, id(1));
  assert.equal(result.chargedAmount, "0");
  assert.equal(result.channels.length, 100);
});

test("client owns JSON and request ID inputs and parsers own returned bytes", () => {
  const requestId = id(3);
  const content = new TextEncoder().encode(" {\"n\":1} ");
  const original = toHex(content);
  const originalId = toHex(requestId);
  const wire = newPublish(requestId, "news", content);
  content[0] = 0x78;
  requestId[0] = 0x78;
  const publish = parsePublish(wire);
  assert.equal(toHex(publish.contentJson), original);
  assert.equal(toHex(publish.requestId), originalId);
  publish.contentJson[0] = 0x78;
  publish.requestId[0] = 0x78;
  assert.equal(toHex(parsePublish(wire).contentJson), original);
  assert.equal(toHex(parsePublish(wire).requestId), originalId);
});

test("Publish forwarding uses a new request ID and exact content bytes", () => {
  const inboundRequestId = id(40);
  const deliveryRequestId = id(41);
  const contentJson = new TextEncoder().encode('  {"n":1,"text":"exact"} \n');

  const inboundWire = newPublish(inboundRequestId, "news", contentJson);
  const inbound = parsePublish(inboundWire);
  const deliveryWire = newPublish(deliveryRequestId, inbound.channel, inbound.contentJson);
  const delivery = parsePublish(deliveryWire);

  assert.notDeepEqual(Array.from(delivery.requestId), Array.from(inbound.requestId));
  assert.equal(delivery.channel, inbound.channel);
  assert.equal(toHex(delivery.contentJson), toHex(contentJson));
  assert.equal(toHex(delivery.contentJson), toHex(inbound.contentJson));
  assert.notEqual(toHex(deliveryWire), toHex(inboundWire));
});

test("service returns ActionResult for every successful request and preserves Publish event", async () => {
  const key = publicKey(4);
  const backend = new MemoryBackend();
  const dispatcher = new RecordingDispatcher();
  const session = new Session(key, backend, dispatcher);
  const subscribeId = id(5);
  const subscribeResult = await session.handleMessage(newSubscribe(subscribeId, "news"));
  const subscribeAction = parseActionResult(subscribeResult.response);
  assert.equal(toHex(subscribeAction.requestId), toHex(subscribeId));
  assert.equal(subscribeAction.success, true);
  assert.equal(subscribeAction.chargedAmount, "0");

  const queryId = id(6);
  const queryResult = await session.handleMessage(newSubscriptionsRequest(queryId));
  const response = parseSubscriptionsResponse(queryResult.response);
  assert.equal(toHex(response.requestId), toHex(queryId));
  assert.deepEqual(response.channels, ["news"]);

  const content = new TextEncoder().encode("  {\"n\":1} \n");
  const publishId = id(7);
  const publishWire = newPublish(publishId, "news", content);
  const publishResult = await session.handleMessage(publishWire);
  assert.equal(parseActionResult(publishResult.response).success, true);
  const events = dispatcher.events();
  assert.equal(toHex(events[0].requestId), toHex(publishId));
  assert.equal(toHex(events[0].payerId), toHex(key));
  assert.equal(toHex(events[0].contentJson), toHex(content));

  const unsubscribeId = id(8);
  const unsubscribeResult = await session.handleMessage(newUnsubscribe(unsubscribeId, "news"));
  assert.equal(parseActionResult(unsubscribeResult.response).success, true);
});

test("Session suppresses concurrent duplicate IDs and permits distinct IDs", async () => {
  const key = publicKey(9);
  let calls = 0;
  const backend = {
    async subscribe(_key, _requestId, _channel) {
      calls += 1;
      await new Promise((resolvePromise) => setTimeout(resolvePromise, 5));
      return "0";
    },
    async unsubscribe() { return "0"; },
    async subscriptions() { return { channels: [], chargedAmount: "0" }; },
  };
  const session = new Session(key, backend, new RecordingDispatcher());
  const requestWire = newSubscribe(id(10), "news");
  const results = await Promise.all(Array.from({ length: 32 }, () => session.handleMessage(requestWire)));
  assert.equal(results.filter((result) => result.response).length, 1);
  assert.equal(results.filter((result) => result.suppressed).length, 31);
  assert.equal(calls, 1);
  const [first, second] = await Promise.all([
    session.handleMessage(newSubscribe(id(11), "first")),
    session.handleMessage(newSubscribe(id(12), "second")),
  ]);
  assert.ok(first.response);
  assert.ok(second.response);
});

test("Session suppresses locally while persistent Backend deduplicates across Sessions", async () => {
  let calls = 0;
  const completed = new Map();
  const backend = {
    subscribe(key, requestId) {
      const keyString = `${toHex(key)}:${toHex(requestId)}`;
      const previous = completed.get(keyString);
      if (previous !== undefined) return previous;
      calls += 1;
      const amount = "0.001";
      completed.set(keyString, amount);
      return amount;
    },
    unsubscribe() { return "0"; },
    subscriptions() { return { channels: [], chargedAmount: "0" }; },
  };
  const requestWire = newSubscribe(id(30), "news");
  const firstSession = new Session(publicKey(30), backend, new RecordingDispatcher());
  assert.ok((await firstSession.handleMessage(requestWire)).response);
  assert.equal((await firstSession.handleMessage(requestWire)).suppressed, true);
  firstSession.close();
  const secondSession = new Session(publicKey(30), backend, new RecordingDispatcher());
  assert.ok((await secondSession.handleMessage(requestWire)).response);
  const differentPayerSession = new Session(publicKey(31), backend, new RecordingDispatcher());
  assert.ok((await differentPayerSession.handleMessage(requestWire)).response);
  assert.equal(calls, 2);
  secondSession.close();
  differentPayerSession.close();
});

test("the same subscription action with different IDs is charged twice", async () => {
  let subscribeCalls = 0;
  let unsubscribeCalls = 0;
  const backend = {
    subscribe() { subscribeCalls += 1; return "0.001"; },
    unsubscribe() { unsubscribeCalls += 1; return "0.001"; },
    subscriptions() { return { channels: [], chargedAmount: "0" }; },
  };
  const session = new Session(publicKey(31), backend, new RecordingDispatcher());
  for (const value of [32, 33]) {
    const result = await session.handleMessage(newSubscribe(id(value), "news"));
    assert.equal(parseActionResult(result.response).chargedAmount, "0.001");
  }
  for (const value of [34, 35]) {
    const result = await session.handleMessage(newUnsubscribe(id(value), "news"));
    assert.equal(parseActionResult(result.response).chargedAmount, "0.001");
  }
  assert.equal(subscribeCalls, 2);
  assert.equal(unsubscribeCalls, 2);
});

test("Session does not serialize distinct IDs behind one async backend call", async () => {
  let started = 0;
  let resolveBothStarted;
  const bothStarted = new Promise((resolve) => { resolveBothStarted = resolve; });
  let release;
  const released = new Promise((resolve) => { release = resolve; });
  const backend = {
    async subscribe() {
      started += 1;
      if (started === 2) resolveBothStarted();
      await released;
      return "0";
    },
    async unsubscribe() { return "0"; },
    async subscriptions() { return { channels: [], chargedAmount: "0" }; },
  };
  const session = new Session(publicKey(11), backend, new RecordingDispatcher());
  const first = session.handleMessage(newSubscribe(id(11), "first"));
  const second = session.handleMessage(newSubscribe(id(12), "second"));
  try {
    await Promise.race([
      bothStarted,
      new Promise((_, reject) => setTimeout(() => reject(new Error("different request IDs were serialized")), 1000)),
    ]);
  } finally {
    release();
  }
  const results = await Promise.all([first, second]);
  assert.ok(results[0].response);
  assert.ok(results[1].response);
});

test("business failures return ActionResult and preserve a local cause", async () => {
  const key = publicKey(13);
  const cause = Object.assign(new Error("余额不足"), { code: ActionErrorCode.REJECTED });
  const backend = {
    async subscribe() { throw cause; },
    async unsubscribe() { return "0"; },
    async subscriptions() { return { channels: [], chargedAmount: "0" }; },
  };
  const session = new Session(key, backend, new RecordingDispatcher());
  const result = await session.handleMessage(newSubscribe(id(14), "news"));
  const response = result.response;
  assert.equal(parseActionResult(response).errorCode, ActionErrorCode.REJECTED);
  assert.ok(result.cause instanceof SSPError);
  assert.equal(result.cause.code, ErrorCode.BACKEND_FAILURE);
  assert.equal(result.cause.cause, cause);
});

test("invalid Backend subscriptions keep INVALID_SUBSCRIPTIONS as the cause", async () => {
  const cause = new SSPError(ErrorCode.INVALID_SUBSCRIPTIONS, "订阅集合包含重复项");
  const backend = {
    subscribe() { return "0"; },
    unsubscribe() { return "0"; },
    subscriptions() { throw cause; },
  };
  const session = new Session(publicKey(14), backend, new RecordingDispatcher());
  const result = await session.handleMessage(newSubscriptionsRequest(id(15)));
  const action = parseActionResult(result.response);
  assert.equal(action.success, false);
  assert.equal(action.chargedAmount, "0");
  assert.equal(action.errorCode, ActionErrorCode.INTERNAL_ERROR);
  assert.ok(result.cause instanceof SSPError);
  assert.equal(result.cause.code, ErrorCode.BACKEND_FAILURE);
  assert.equal(result.cause.cause, cause);
});

test("service returns Backend and Dispatcher amounts without floating point conversion", async () => {
  const key = publicKey(19);
  const backend = {
    subscribe() { return "0.001"; },
    unsubscribe() { return "12.34"; },
    subscriptions() { return { channels: ["news"], chargedAmount: "0.123456789012345678" }; },
  };
  const dispatcher = { dispatch() { return "1.2"; } };
  const session = new Session(key, backend, dispatcher);

  const subscribe = await session.handleMessage(newSubscribe(id(20), "news"));
  assert.equal(parseActionResult(subscribe.response).chargedAmount, "0.001");
  const unsubscribe = await session.handleMessage(newUnsubscribe(id(21), "news"));
  assert.equal(parseActionResult(unsubscribe.response).chargedAmount, "12.34");
  const query = await session.handleMessage(newSubscriptionsRequest(id(22)));
  assert.equal(parseSubscriptionsResponse(query.response).chargedAmount, "0.123456789012345678");
  const publish = await session.handleMessage(newPublish(id(23), "news", new TextEncoder().encode("{}")));
  assert.equal(parseActionResult(publish.response).chargedAmount, "1.2");
});

test("response/action encoders reject illegal field combinations", () => {
  const requestId = id(15);
  expectCode(() => newActionResult({ requestId, success: true, chargedAmount: "0", errorCode: ActionErrorCode.REJECTED }), ErrorCode.INVALID_WIRE);
  expectCode(() => newActionResult({ requestId, success: false, chargedAmount: "1", errorCode: ActionErrorCode.REJECTED }), ErrorCode.INVALID_WIRE);
  expectCode(() => newActionResult({ requestId, success: false, chargedAmount: "0", errorCode: "" }), ErrorCode.INVALID_WIRE);
  assert.equal(parseActionResult(newActionResult({ requestId, success: false, chargedAmount: "0", errorCode: ActionErrorCode.INTERNAL_ERROR })).errorCode, ActionErrorCode.INTERNAL_ERROR);
  assert.equal(parseSubscriptionsResponse(newSubscriptionsResponse({ requestId, chargedAmount: "0.001", channels: [] })).chargedAmount, "0.001");
  assert.equal(decodeActionResultDocument(encodeActionResultDocument({ requestId, success: true, chargedAmount: "0", errorCode: "" })).success, true);
});

test("newRequestId uses the fixed secure-random shape", () => {
  assert.equal(newRequestId().byteLength, 32);
  assert.equal(MAX_REQUEST_IDS_PER_SESSION, 65_536);
});

test("Session rejects the 65537th new request ID and still suppresses duplicates", async () => {
  const session = new Session(publicKey(18), new MemoryBackend(), new RecordingDispatcher());
  let lastWire;
  for (let index = 0; index < MAX_REQUEST_IDS_PER_SESSION; index += 1) {
    lastWire = newSubscriptionsRequest(numberedId(index));
    const result = await session.handleMessage(lastWire);
    assert.ok(result.response);
    assert.equal(result.suppressed, false);
  }
  const overLimit = await session.handleMessage(newSubscriptionsRequest(numberedId(MAX_REQUEST_IDS_PER_SESSION))).catch((error) => error);
  assert.ok(overLimit instanceof SSPError);
  assert.equal(overLimit.code, ErrorCode.SESSION_LIMIT);
  const duplicate = await session.handleMessage(lastWire);
  assert.equal(duplicate.suppressed, true);
  assert.equal(duplicate.response, undefined);
});

function decodeRequestIdFromChannel(document) {
  // 订阅文档的固定布局是 [request_id, channel]，request_id 位于 2 字节头之后。
  return document.slice(3, 35);
}

function publicKey(value) {
  const result = new Uint8Array(33);
  result[0] = 0x02;
  result.fill(value, 1);
  return result;
}

function numberedId(value) {
  const result = new Uint8Array(32);
  result[28] = (value >>> 24) & 0xff;
  result[29] = (value >>> 16) & 0xff;
  result[30] = (value >>> 8) & 0xff;
  result[31] = value & 0xff;
  return result;
}
