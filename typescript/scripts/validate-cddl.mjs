import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { parse } from "cddl";

const specPath = resolve(process.argv[2] ?? "../spec/v1/ssp.cddl");
const source = await readFile(specPath, "utf8");

const requiredBindings = [
  "ssp-wire = publish-wire",
  "ssp-wire = publish-wire\n         / subscribe-wire",
  "publish-wire = [wire-version, publish-kind, publish-document]",
  "subscribe-wire = [wire-version, subscribe-kind, subscribe-document]",
  "unsubscribe-wire = [wire-version, unsubscribe-kind, unsubscribe-document]",
  "subscriptions-request-wire = [wire-version, subscriptions-request-kind, subscriptions-request-document]",
  "subscriptions-response-wire = [wire-version, subscriptions-response-kind, subscriptions-response-document]",
  "action-result-wire = [wire-version, action-result-kind, action-result-document]",
  "publish-document = (bstr .cbor publish) .size (1..1048512)",
  "subscribe-document = (bstr .cbor subscribe) .size (1..1048512)",
  "unsubscribe-document = (bstr .cbor unsubscribe) .size (1..1048512)",
  "subscriptions-request-document = (bstr .cbor subscriptions-request) .size (1..1048512)",
  "subscriptions-response-document = (bstr .cbor subscriptions-response) .size (1..1048512)",
  "action-result-document = (bstr .cbor action-result) .size (1..1048512)",
  "action-result = [request-id, true, charged-amount, \"\"] / [request-id, false, \"0\", action-error-code]",
];

for (const binding of requiredBindings) {
  if (!source.includes(binding)) {
    throw new Error(`缺少 CDDL Kind/document 绑定：${binding}`);
  }
}

// cddl@0.21.1 尚未实现 .cbor 控制操作；先验证其支持的 RFC 8610 语法，
// 上面的精确断言负责防止 .cbor 绑定被删除或改错。支持该操作的 CDDL
// 实现可以直接解析原始 spec/v1/ssp.cddl。
const compatibleSource = source
  .replace(/\.cbor\s+[A-Za-z][A-Za-z0-9-]*/g, "")
  .replace(/\(bstr\s*\)\s*\.size\s*\(1\.\.1048512\)/g, "bstr")
  // cddl@0.21.1 also cannot parse a choice between fixed array groups;
  // the exact ActionResult choice is checked by requiredBindings above.
  .replace(
    'action-result = [request-id, true, charged-amount, ""] / [request-id, false, "0", action-error-code]',
    "action-result = [request-id, any, charged-amount, any]",
  );
const temporaryDirectory = await mkdtemp(join(tmpdir(), "ssp-cddl-"));
const temporaryPath = join(temporaryDirectory, "ssp.cddl");
try {
  await writeFile(temporaryPath, compatibleSource, "utf8");
  parse(temporaryPath);
} finally {
  await rm(temporaryDirectory, { recursive: true, force: true });
}

console.log("✅ Valid CDDL file and Kind/document bindings!");
