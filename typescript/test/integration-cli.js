import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  ErrorCode,
  Kind,
  MemoryBackend,
  RecordingDispatcher,
  encodeSubscriptionsResponseDocument,
  encodeWire,
  handleMessage,
  newPublish,
  newSubscribe,
  newSubscriptionsRequest,
  newUnsubscribe,
  normalizeSubscriptions,
  parse,
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
        wire = newPublish(item.channel, fromHex(item.content_json_hex));
        break;
      case Kind.Subscribe:
        wire = newSubscribe(item.channel);
        break;
      case Kind.Unsubscribe:
        wire = newUnsubscribe(item.channel);
        break;
      case Kind.SubscriptionsRequest:
        wire = newSubscriptionsRequest();
        break;
      case Kind.SubscriptionsResponse:
        wire = encodeWire(Kind.SubscriptionsResponse, encodeSubscriptionsResponseDocument(item.channels));
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
  const key = fromHex(fixture.publish_event.publisher_public_key_hex);
  const backend = new MemoryBackend();
  const dispatcher = new RecordingDispatcher();
  const result = await handleMessage(key, fromHex(fixture.publish_event.wire_hex), backend, dispatcher);
  return {
    response_hex: toHex(result.response),
    has_response: Boolean(result.response),
    events: dispatcher.events().map((event) => ({
      publisher_public_key_hex: toHex(event.publisherPublicKey),
      channel: event.channel,
      content_json_hex: toHex(event.contentJson),
      wire_hex: toHex(event.wire),
    })),
  };
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
