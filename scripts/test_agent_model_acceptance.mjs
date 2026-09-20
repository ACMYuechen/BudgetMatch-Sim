import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import { once } from 'node:events';
import { BudgetLedger, LIMITS, UsageProbe, flashRequest, createFlashGuard } from './agent-model-acceptance.mjs';

function temporary(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-model-guard-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true }));
  return dir;
}
const input = () => ({ model: 'deepseek-flash', stream: true, max_tokens: 1024, messages: [{ role: 'user', content: 'synthetic only' }] });
const usage = { prompt_tokens: 100, completion_tokens: 10, total_tokens: 110 };
const complete = `data: {"choices":[{"delta":{"content":"合成文本"}}]}\n\ndata: ${JSON.stringify({ choices: [], usage })}\n\ndata: [DONE]\n\n`;
const requestBytes = (value = input()) => Buffer.from(JSON.stringify(value));

test('full-context reservations retain unknown costs and refuse a third uncertain call', (t) => {
  const file = path.join(temporary(t), 'ledger.jsonl');
  const ledger = new BudgetLedger(file);
  assert.equal(ledger.reserve(1024), 1);
  assert.equal(ledger.reserve(1024), 2);
  assert.throws(() => ledger.reserve(1024));
  assert.equal(ledger.snapshot().held_micro_cny, 4_210_688);
  assert.equal(ledger.snapshot().unknown_calls, 2);
  assert.equal(fs.statSync(file).mode & 0o777, 0o600);
  ledger.close();
  assert.throws(() => new BudgetLedger(file));
  assert.equal(fs.readFileSync(file, 'utf8').trim().split('\n').length, 3);
});

test('known usage releases only the excess reservation; ten real attempts are the limit', (t) => {
  const ledger = new BudgetLedger(path.join(temporary(t), 'ledger.jsonl'));
  for (let i = 1; i <= LIMITS.calls; i++) {
    assert.equal(ledger.reserve(1024), i);
    ledger.settle(i, { ...usage, ignored_private_field: 'must not persist' });
  }
  assert.throws(() => ledger.reserve(1));
  assert.throws(() => ledger.settle(1, usage));
  assert.equal(ledger.snapshot().held_micro_cny, 2800);
  assert.equal(JSON.stringify(ledger.snapshot()).includes('must not persist'), false);
  const snapshot = ledger.snapshot(); snapshot.requests[0].held_micro_cny = 0;
  assert.equal(ledger.snapshot().requests[0].held_micro_cny, 280);
  ledger.close();
  assert.throws(() => ledger.reserve(1024));
});

test('invalid usage cannot release a reservation', (t) => {
  const ledger = new BudgetLedger(path.join(temporary(t), 'ledger.jsonl'));
  ledger.reserve(512);
  for (const invalid of [null, {}, { ...usage, total_tokens: 0 }, { ...usage, prompt_tokens: -1 },
    { prompt_tokens: 100, completion_tokens: 513, total_tokens: 613 },
    { prompt_tokens: LIMITS.context + 1, completion_tokens: 0, total_tokens: LIMITS.context + 1 }]) {
    assert.throws(() => ledger.settle(1, invalid));
  }
  assert.equal(ledger.snapshot().unknown_calls, 1);
  ledger.close();
});

test('journal refuses permissive directories and symlink aliases', (t) => {
  const dir = temporary(t);
  fs.mkdirSync(path.join(dir, 'public'), { mode: 0o755 });
  assert.throws(() => new BudgetLedger(path.join(dir, 'public', 'ledger')));
  fs.symlinkSync(dir, path.join(dir, 'alias'));
  assert.throws(() => new BudgetLedger(path.join(dir, 'alias', 'ledger')));
});

test('durable reservation failure fails closed before any new attempt', (t) => {
  const ledger = new BudgetLedger(path.join(temporary(t), 'ledger'));
  const original = fs.writeSync;
  fs.writeSync = () => { throw new Error('synthetic storage failure'); };
  try { assert.throws(() => ledger.reserve(1024)); } finally { fs.writeSync = original; }
  assert.throws(() => ledger.reserve(1024));
  assert.equal(ledger.snapshot().calls, 0);
  assert.equal(ledger.snapshot().failed_closed, true);
  ledger.close();
});

test('Flash override is explicit, bounded, text-only and tool-allowlisted', () => {
  const accepted = JSON.parse(flashRequest(requestBytes()).body.toString());
  assert.deepEqual(accepted.thinking, { type: 'disabled' });
  assert.deepEqual(accepted.stream_options, { include_usage: true });
  for (const patch of [{ model: 'deepseek-v4-pro' }, { stream: false }, { n: 2 }, { max_tokens: 0 },
    { max_tokens: 1025 }, { thinking: { type: 'enabled' } }, { reasoning_effort: 'max' }, { user: 'secret' },
    { messages: [{ role: 'user', content: [{ type: 'image_url', image_url: { url: 'https://example.com' } }] }] },
    { tools: [{ type: 'function', function: { name: 'read_file' } }] }]) {
    assert.throws(() => flashRequest(requestBytes({ ...input(), ...patch })));
  }
  assert.throws(() => flashRequest(Buffer.alloc(LIMITS.body + 1)));
  assert.throws(() => flashRequest(Buffer.from('null')));
  assert.throws(() => flashRequest(Buffer.from('{')));
});

test('SSE metadata parsing survives arbitrary UTF-8 chunk boundaries and CRLF', () => {
  const probe = new UsageProbe(1024);
  for (const byte of Buffer.from(complete.replaceAll('\n', '\r\n'))) probe.push(Buffer.from([byte]));
  assert.deepEqual(probe.finish(), usage);
});

test('missing, conflicting, malformed or truncated final usage stays unknown', () => {
  for (const content of [complete.replace('data: [DONE]\n\n', ''), 'data: [DONE]\n\n',
    complete.replace('"total_tokens":110', '"total_tokens":109'), complete + 'data: {}\n\n',
    complete.replace('data: [DONE]', 'data: invalid\n\ndata: [DONE]'),
    complete.replace('data: [DONE]', `data: ${JSON.stringify({ usage: { ...usage, prompt_tokens: 101, total_tokens: 111 } })}\n\ndata: [DONE]`),
    complete.slice(0, -2), 'data: {"error":{"message":"private"}}\n\ndata: [DONE]\n\n']) {
    const probe = new UsageProbe(1024); probe.push(Buffer.from(content));
    assert.equal(probe.finish(), null);
  }
  const probe = new UsageProbe(1024);
  assert.throws(() => probe.push(Buffer.alloc(1_048_577)));
});

async function fixture(t, respond) {
  const ledger = new BudgetLedger(path.join(temporary(t), 'ledger'));
  const seen = [];
  const upstream = http.createServer(async (req, res) => {
    let body = ''; for await (const chunk of req) body += chunk.toString();
    seen.push({ url: req.url, authorization: req.headers.authorization, body: JSON.parse(body) });
    respond(req, res);
  });
  upstream.listen(0, '127.0.0.1'); await once(upstream, 'listening');
  const guard = createFlashGuard({ ledger, apiKey: 'fake-provider-key', localKey: 'fake-local-key',
    openUpstream(key, body, receive) {
      const req = http.request({ hostname: '127.0.0.1', port: upstream.address().port, path: '/fixed', method: 'POST',
        headers: { Authorization: `Bearer ${key}`, 'Content-Length': body.length } }, receive);
      req.end(body); return req;
    },
  });
  guard.server.listen(0, '127.0.0.1'); await once(guard.server, 'listening');
  t.after(async () => { await guard.close(); upstream.closeAllConnections(); await new Promise((r) => upstream.close(r)); ledger.close(); });
  const url = `http://127.0.0.1:${guard.server.address().port}/v1/chat/completions`;
  const post = (body = input(), headers = { Authorization: 'Bearer fake-local-key' }) => fetch(url, { method: 'POST', body: JSON.stringify(body), headers, redirect: 'error' });
  return { guard, ledger, seen, url, post };
}

test('guard forwards genuine bytes with private key replacement and settles only at EOF', async (t) => {
  const f = await fixture(t, (_, res) => { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.end(complete); });
  const response = await f.post();
  assert.equal(response.status, 200);
  assert.equal(await response.text(), complete);
  assert.equal(f.seen.length, 1);
  assert.equal(f.seen[0].authorization, 'Bearer fake-provider-key');
  assert.deepEqual(f.seen[0].body.thinking, { type: 'disabled' });
  assert.equal(f.ledger.snapshot().unknown_calls, 0);
  assert.equal(f.ledger.snapshot().held_micro_cny, 280);
  assert.equal(JSON.stringify(f.guard.snapshot()).includes('合成文本'), false);
  assert.equal(JSON.stringify(f.guard.snapshot()).includes('fake-provider-key'), false);
});

test('unauthenticated and out-of-scope requests make no upstream calls', async (t) => {
  const f = await fixture(t, (_, res) => res.end());
  assert.equal((await f.post(input(), {})).status, 403);
  assert.equal((await f.post({ ...input(), model: 'deepseek-v4-pro' })).status, 403);
  assert.equal((await fetch(f.url, { headers: { Authorization: 'Bearer fake-local-key' } })).status, 403);
  assert.equal(f.seen.length, 0);
  assert.equal(f.ledger.snapshot().calls, 0);
});

test('HTTP retries cannot bypass the global ten-attempt limit', async (t) => {
  const f = await fixture(t, (_, res) => { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.end(complete); });
  for (let i = 0; i < 12; i++) {
    const response = await f.post();
    assert.equal(response.status, i < 10 ? 200 : 403);
    await response.text();
  }
  assert.equal(f.seen.length, 10);
  assert.equal(f.ledger.snapshot().calls, 10);
  assert.equal(f.ledger.snapshot().unknown_calls, 0);
});

test('redirects and upstream errors never leak raw bodies or reset uncertain cost', async (t) => {
  const f = await fixture(t, (_, res) => { res.writeHead(302, { Location: 'https://example.com/private' }); res.end('private error'); });
  for (let i = 0; i < 3; i++) {
    const response = await f.post();
    assert.equal(response.status, 403);
    assert.equal((await response.text()).includes('private'), false);
  }
  assert.equal(f.seen.length, 2);
  assert.equal(f.ledger.snapshot().unknown_calls, 2);
});

test('client disconnect cancels the upstream socket and retains its reservation', async (t) => {
  let upstreamClosed;
  const closed = new Promise((r) => { upstreamClosed = r; });
  const f = await fixture(t, (_, res) => {
    res.on('close', upstreamClosed);
    res.writeHead(200, { 'Content-Type': 'text/event-stream' });
    res.write('data: {"choices":[{"delta":{"content":"partial"}}]}\n\n');
  });
  const controller = new AbortController();
  const response = await fetch(f.url, { method: 'POST', headers: { Authorization: 'Bearer fake-local-key' }, body: JSON.stringify(input()), signal: controller.signal });
  await response.body.getReader().read();
  controller.abort();
  await closed;
  assert.equal(f.seen.length, 1);
  assert.equal(f.ledger.snapshot().unknown_calls, 1);
  assert.equal(f.ledger.snapshot().held_micro_cny, 2_105_344);
});
