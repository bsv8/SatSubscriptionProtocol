import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  ActionErrorCode,
  ErrorCode,
  Kind,
  MemoryBackend,
  RecordingDispatcher,
  Session,
  encodeSubscriptionsResponseDocument,
  encodeWire,
  newPublish,
  newActionResult,
  newBillingRequest,
  newBillingResponse,
  newSubscriptionsResponse,
  newSubscribe,
  newSubscriptionsRequest,
  newUnsubscribe,
  normalizeSubscriptions,
  parse,
  parseActionResult,
  parseBillingResponse,
  parseSubscriptionsResponse,
  matches,
  subscribe,
  unsubscribe,
} from "../dist/index.js";

const fixtureDirectory = resolve(dirname(fileURLToPath(import.meta.url)), "../../testdata/v1");

function fromHex(value) {
  const result = new Uint8Array(value.length / 2);
  for (let index = 0; index < result.length; index += 1) {
    result[index] = Number.parseInt(value.slice(index * 2, index * 2 + 2), 16);
  }
  return result;
}

function toHex(value) {
  return value ? Array.from(value, (byte) => byte.toString(16).padStart(2, "0")).join("") : "";
}

async function readFixture(name) {
  return JSON.parse(await readFile(resolve(fixtureDirectory, name), "utf8"));
}

async function run() {
  const mode = process.argv[2];
  let value;
  switch (mode) {
    case "encode":
      value = await encodeFixtures();
      break;
    case "decode":
      value = await decodeExchange(process.argv[3]);
      break;
    case "state":
      value = await stateExchange();
      break;
    case "invalid":
      value = await invalidExchange();
      break;
    case "service":
      value = await serviceExchange();
      break;
    case "dispatch":
      value = await dispatchExchange();
      break;
    case "billing":
      value = await billingExchange();
      break;
    case "billing-query":
      value = await billingQueryExchange();
      break;
    default:
      throw new Error(`unknown mode ${mode}`);
  }
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

async function encodeFixtures() {
  const fixture = await readFixture("wire-v1.json");
  return fixture.cases.map((item) => {
    let wire;
    switch (item.kind) {
      case Kind.Publish:
        wire = newPublish(fromHex(item.request_id_hex), item.channel, fromHex(item.content_json_hex));
        break;
      case Kind.Subscribe:
        wire = newSubscribe(fromHex(item.request_id_hex), item.channel);
        break;
      case Kind.Unsubscribe:
        wire = newUnsubscribe(fromHex(item.request_id_hex), item.channel);
        break;
      case Kind.SubscriptionsRequest:
        wire = newSubscriptionsRequest(fromHex(item.request_id_hex));
        break;
      case Kind.BillingRequest:
        wire = newBillingRequest(
          fromHex(item.request_id_hex),
          BigInt(item.from_ms),
          BigInt(item.to_ms),
          item.limit,
          item.cursor,
        );
        break;
      case Kind.SubscriptionsResponse:
        wire = newSubscriptionsResponse({
          requestId: fromHex(item.request_id_hex),
          chargedAmount: item.charged_amount,
          channels: item.channels,
        });
        break;
      case Kind.ActionResult:
        wire = newActionResult({
          requestId: fromHex(item.request_id_hex),
          success: item.success,
          chargedAmount: item.charged_amount,
          errorCode: item.error_code,
        });
        break;
      case Kind.BillingResponse:
        wire = newBillingResponse({
          requestId: fromHex(item.request_id_hex),
          success: item.success,
          currency: item.currency,
          network: item.network,
          records: item.records.map((record) => ({
            chargeId: record.charge_id,
            occurredAtMs: BigInt(record.occurred_at_ms),
            action: record.action,
            channel: record.channel,
            sourceRequestId: fromHex(record.source_request_id_hex),
            chargedAmount: record.charged_amount,
          })),
          nextCursor: item.next_cursor,
          errorCode: item.error_code,
        });
        break;
      default:
        throw new Error(`unknown fixture kind ${item.kind}`);
    }
    return { name: item.name, wire_hex: toHex(wire) };
  });
}

async function decodeExchange(path) {
  const input = JSON.parse(await readFile(path, "utf8"));
  return input.map((item) => {
    const message = parse(fromHex(item.wire_hex));
    return { name: item.name, wire_hex: toHex(encodeWire(message.kind, message.document)) };
  });
}

async function stateExchange() {
  const fixture = await readFixture("subscriptions-v1.json");
  return {
    normalization: fixture.normalization.map((item) => ({ name: item.name, result: normalizeSubscriptions(item.input) })),
    transitions: fixture.transitions.map((item) => ({
      name: item.name,
      result: (item.operation === "subscribe" ? subscribe : unsubscribe)(item.initial, item.channel),
    })),
  };
}

async function invalidExchange() {
  const fixture = await readFixture("invalid-v1.json");
  return fixture.cases.map((item) => {
    try {
      parse(fromHex(item.wire_hex));
    } catch (error) {
      return { name: item.name, code: error?.code ?? "" };
    }
    throw new Error(`invalid fixture ${item.name} unexpectedly parsed`);
  });
}

async function serviceExchange() {
  const fixture = await readFixture("dispatch-v1.json");
  const key = fromHex(fixture.publish_event.payer_id_hex);
  const backend = new MemoryBackend();
  const dispatcher = new RecordingDispatcher();
  const session = new Session(key, backend, dispatcher);
  try {
    const result = await session.handleMessage(fromHex(fixture.publish_event.wire_hex));
    return {
      response_hex: toHex(result.response),
      has_response: Boolean(result.response),
      events: dispatcher.events().map((event) => ({
        request_id_hex: toHex(event.requestId),
        payer_id_hex: toHex(event.payerId),
        channel: event.channel,
        content_json_hex: toHex(event.contentJson),
      })),
    };
  } finally {
    session.close();
  }
}

async function billingExchange() {
  const fixture = await readFixture("billing-v1.json");
  const key = fromHex(fixture.payer_id_hex);
  const memory = new MemoryBackend();
  const backend = {
    subscribe(remotePublicKey, requestId, channel) {
      if (channel === "reject") {
        throw Object.assign(new Error("余额不足"), { code: ActionErrorCode.REJECTED });
      }
      memory.subscribe(remotePublicKey, requestId, channel);
      return "0";
    },
    unsubscribe(remotePublicKey, requestId, channel) {
      memory.unsubscribe(remotePublicKey, requestId, channel);
      return "0.001";
    },
    subscriptions(remotePublicKey, requestId) {
      const result = memory.subscriptions(remotePublicKey, requestId);
      return { channels: result.channels, chargedAmount: "12.34" };
    },
  };
  const dispatcher = {
    dispatch(event) {
      if (event.channel === "internal") {
        throw new Error("广播 Dispatcher 不可用");
      }
      return "1.2";
    },
  };
  const session = new Session(key, backend, dispatcher);
  try {
    const result = [];
    for (const item of fixture.cases) {
      const requestWire = billingRequest(item);
      const handled = await session.handleMessage(requestWire);
      if (!handled.response) {
        throw new Error(`billing service ${item.name} returned no response`);
      }
      validateBillingResponse(item, handled.response);
      result.push({ name: item.name, response_hex: toHex(handled.response) });
    }
    return result;
  } finally {
    session.close();
  }
}

async function billingQueryExchange() {
  const fixture = await readFixture("billing-query-v1.json");
  const key = fromHex(fixture.payer_id_hex);
  const backend = {
    subscribe() { return "0"; },
    unsubscribe() { return "0"; },
    subscriptions() { return { channels: [], chargedAmount: "0" }; },
    listChargeRecords(_remotePublicKey, request) {
      if (request.cursor === "reject") {
        throw Object.assign(new Error("余额不足"), { code: ActionErrorCode.REJECTED });
      }
      if (request.cursor === "cursor-2") {
        return { currency: "BSV", network: "testnet", records: [], nextCursor: "" };
      }
      return {
        currency: "BSV",
        network: "testnet",
        records: [{
          chargeId: "operation-1",
          occurredAtMs: 1_700_000_000_123n,
          action: "publish",
          channel: "news",
          sourceRequestId: fromHex("4a".repeat(32)),
          chargedAmount: "0.001",
        }],
        nextCursor: "cursor-2",
      };
    },
  };
  const session = new Session(key, backend, new RecordingDispatcher());
  try {
    const result = [];
    for (const item of fixture.cases) {
      const requestWire = newBillingRequest(
        fromHex(item.request_id_hex),
        BigInt(item.from_ms),
        BigInt(item.to_ms),
        item.limit,
        item.cursor,
      );
      const handled = await session.handleMessage(requestWire);
      if (!handled.response) {
        throw new Error(`billing query ${item.name} returned no response`);
      }
      const response = parseBillingResponse(handled.response);
      validateBillingQueryResponse(item, response);
      result.push({ name: item.name, response_hex: toHex(handled.response) });
    }
    return result;
  } finally {
    session.close();
  }
}

function validateBillingQueryResponse(item, response) {
  if (toHex(response.requestId) !== item.request_id_hex
    || response.success !== item.expected_success
    || response.currency !== item.expected_currency
    || response.network !== item.expected_network
    || response.nextCursor !== item.expected_next_cursor
    || response.errorCode !== item.expected_error_code
    || response.records.length !== item.expected_records.length) {
    throw new Error(`billing query response ${item.name} does not match expected fields`);
  }
  response.records.forEach((record, index) => {
    const expected = item.expected_records[index];
    if (record.chargeId !== expected.charge_id
      || record.occurredAtMs !== BigInt(expected.occurred_at_ms)
      || record.action !== expected.action
      || record.channel !== expected.channel
      || toHex(record.sourceRequestId) !== expected.source_request_id_hex
      || record.chargedAmount !== expected.charged_amount) {
      throw new Error(`billing query response ${item.name} record ${index} does not match`);
    }
  });
}

function billingRequest(item) {
  const requestId = fromHex(item.request_id_hex);
  switch (item.kind) {
    case Kind.Publish:
      return newPublish(requestId, item.channel, fromHex(item.content_json_hex));
    case Kind.Subscribe:
      return newSubscribe(requestId, item.channel);
    case Kind.Unsubscribe:
      return newUnsubscribe(requestId, item.channel);
    case Kind.SubscriptionsRequest:
      return newSubscriptionsRequest(requestId);
    default:
      throw new Error(`billing fixture ${item.name} is not a client request kind`);
  }
}

function validateBillingResponse(item, response) {
  const message = parse(response);
  if (message.kind !== item.expected_response_kind) {
    throw new Error(`billing response ${item.name} kind=${message.kind}, want ${item.expected_response_kind}`);
  }
  if (message.kind === Kind.ActionResult) {
    const result = parseActionResult(response);
    if (toHex(result.requestId) !== item.request_id_hex
      || result.success !== item.expected_success
      || result.chargedAmount !== item.expected_charged_amount
      || result.errorCode !== item.expected_error_code) {
      throw new Error(`billing ActionResult ${item.name} does not match expected fields`);
    }
    return;
  }
  if (message.kind === Kind.SubscriptionsResponse) {
    const result = parseSubscriptionsResponse(response);
    if (toHex(result.requestId) !== item.request_id_hex
      || result.chargedAmount !== item.expected_charged_amount
      || JSON.stringify(result.channels) !== JSON.stringify(item.expected_channels)) {
      throw new Error(`billing SubscriptionsResponse ${item.name} does not match expected fields`);
    }
    return;
  }
  throw new Error(`billing response ${item.name} uses unsupported response kind ${message.kind}`);
}

async function dispatchExchange() {
  const fixture = await readFixture("dispatch-v1.json");
  const matchCases = fixture.match_cases.map((item) => {
    const matched = matches(item.subscriptions, item.publish_channel);
    if (matched !== item.expected) {
      throw new Error(`dispatch fixture ${item.name} expected matched=${item.expected}, got ${matched}`);
    }
    return { name: item.name, matched };
  });
  const result = {
    match_cases: matchCases,
    unique_matching_identity_hex: [],
  };
  const seen = new Set();
  for (const identity of fixture.duplicate_identity_case.online_identities) {
    const matched = matches(identity.subscriptions, fixture.duplicate_identity_case.publish_channel);
    if (matched && !seen.has(identity.remote_public_key_hex)) {
      seen.add(identity.remote_public_key_hex);
      result.unique_matching_identity_hex.push(identity.remote_public_key_hex);
    }
  }
  if (JSON.stringify(result.unique_matching_identity_hex) !== JSON.stringify(fixture.duplicate_identity_case.expected_unique_matching_identity_hex)) {
    throw new Error("dispatch fixture unique identities do not match expected result");
  }
  return result;
}

run().catch((error) => {
  if (error?.code && Object.values(ErrorCode).includes(error.code)) {
    process.stderr.write(`${error.code}\n`);
  } else {
    process.stderr.write(`${error?.message ?? error}\n`);
  }
  process.exitCode = 1;
});
