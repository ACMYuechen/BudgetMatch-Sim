// Opt-in acceptance support, not a deployed proxy or an automatic paid test.
// Prices verified 2026-09-20: DeepSeek Flash peak/cache-miss CNY 2/8 per 1M.
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import https from 'node:https';
import { timingSafeEqual } from 'node:crypto';
import { Transform, pipeline } from 'node:stream';
import { StringDecoder } from 'node:string_decoder';
import { EventEmitter } from 'node:events';

export const LIMITS = Object.freeze({ calls: 10, microCny: 5_000_000, context: 1_048_576, output: 1024, body: 65_536 });
const safeError = () => new Error('model acceptance guard rejected operation');
const cost = (input, output) => input * 2 + output * 8; // integer millionths of CNY

export class BudgetLedger {
  #fd;
  #failed = false;
  #calls = [];
  constructor(filename) {
    const parent = path.dirname(filename);
    const info = fs.lstatSync(parent);
    if (!path.isAbsolute(filename) || !info.isDirectory() || info.isSymbolicLink() ||
        (info.mode & 0o077) || fs.realpathSync(parent) !== parent) throw safeError();
    // Refuse reuse. A crashed run keeps its reservations; never silently reset it.
    this.#fd = fs.openSync(filename, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600);
    this.#append({ kind: 'limits', ...LIMITS, model: 'deepseek-flash', thinking: 'disabled', prices: [2, 8] });
    const dir = fs.openSync(parent, 'r');
    try { fs.fsyncSync(dir); } finally { fs.closeSync(dir); }
  }
  #append(record) {
    if (this.#failed || this.#fd === undefined) throw safeError();
    try {
      const data = Buffer.from(JSON.stringify(record) + '\n');
      let offset = 0;
      while (offset < data.length) {
        const written = fs.writeSync(this.#fd, data, offset, data.length - offset);
        if (written <= 0) throw safeError();
        offset += written;
      }
      fs.fsyncSync(this.#fd);
    } catch {
      this.#failed = true;
      throw safeError();
    }
  }
  reserve(maxTokens) {
    if (!Number.isSafeInteger(maxTokens) || maxTokens < 1 || maxTokens > LIMITS.output) throw safeError();
    const reserved = cost(LIMITS.context, maxTokens);
    const spent = this.#calls.reduce((sum, item) => sum + item.held_micro_cny, 0);
    if (this.#calls.length >= LIMITS.calls || spent + reserved > LIMITS.microCny) throw safeError();
    const call = { id: this.#calls.length + 1, max_tokens: maxTokens, held_micro_cny: reserved, usage: null };
    // Durable admission precedes ANY network call, including SDK retries.
    this.#append({ kind: 'reserve', ...call });
    this.#calls.push(call);
    return call.id;
  }
  settle(id, usage) {
    const call = this.#calls[id - 1];
    if (!call || call.usage || !validUsage(usage, call.max_tokens)) throw safeError();
    const held = cost(usage.prompt_tokens, usage.completion_tokens);
    const counts = { prompt_tokens: usage.prompt_tokens, completion_tokens: usage.completion_tokens, total_tokens: usage.total_tokens };
    this.#append({ kind: 'settle', id, held_micro_cny: held, usage: counts });
    call.usage = counts;
    call.held_micro_cny = held;
  }
  snapshot() {
    return {
      calls: this.#calls.length,
      held_micro_cny: this.#calls.reduce((sum, call) => sum + call.held_micro_cny, 0),
      known_micro_cny_upper_bound: this.#calls.filter((call) => call.usage).reduce((sum, call) => sum + call.held_micro_cny, 0),
      unknown_calls: this.#calls.filter((call) => !call.usage).length,
      failed_closed: this.#failed,
      requests: structuredClone(this.#calls),
    };
  }
  close() {
    if (this.#fd !== undefined) fs.closeSync(this.#fd);
    this.#fd = undefined;
  }
}

function validUsage(usage, maxTokens) {
  return usage && ['prompt_tokens', 'completion_tokens', 'total_tokens'].every((key) => Number.isSafeInteger(usage[key]) && usage[key] >= 0) &&
    usage.prompt_tokens > 0 && usage.prompt_tokens <= LIMITS.context && usage.completion_tokens <= maxTokens &&
    usage.prompt_tokens + usage.completion_tokens === usage.total_tokens;
}

export function flashRequest(raw) {
  if (!Buffer.isBuffer(raw) || raw.length > LIMITS.body) throw safeError();
  let value;
  try { value = JSON.parse(raw.toString('utf8')); } catch { throw safeError(); }
  const allowed = new Set(['model', 'messages', 'max_tokens', 'stream', 'stream_options', 'tools', 'tool_choice',
    'temperature', 'top_p', 'presence_penalty', 'frequency_penalty', 'stop', 'parallel_tool_calls', 'n', 'thinking']);
  if (!value || Array.isArray(value) || Object.keys(value).some((key) => !allowed.has(key)) ||
      value.model !== 'deepseek-flash' || value.stream !== true || (value.n !== undefined && value.n !== 1) ||
      !Number.isSafeInteger(value.max_tokens) || value.max_tokens < 1 || value.max_tokens > LIMITS.output ||
      !Array.isArray(value.messages) || !value.messages.length || value.messages.length > 64) throw safeError();
  if (value.thinking !== undefined && (!value.thinking || typeof value.thinking !== 'object' ||
      Array.isArray(value.thinking) || Object.keys(value.thinking).length !== 1 ||
      value.thinking.type !== 'disabled')) throw safeError();
  for (const message of value.messages) {
    if (!message || !['system', 'user', 'assistant', 'tool'].includes(message.role) ||
        (typeof message.content !== 'string' && !(message.role === 'assistant' && message.content == null)) ||
        Object.keys(message).some((key) => !['role', 'content', 'name', 'tool_calls', 'tool_call_id'].includes(key))) throw safeError();
  }
  if (value.tools !== undefined && (!Array.isArray(value.tools) || value.tools.length > 2 || value.tools.some((tool) =>
    tool?.type !== 'function' || !['search_products', 'select_bundle'].includes(tool.function?.name)))) throw safeError();
  // Accept the explicit application profile or the legacy omitted field, never
  // broaden the experiment to thinking mode. No application config is mutated.
  value.thinking = { type: 'disabled' };
  value.stream_options = { include_usage: true };
  return { body: Buffer.from(JSON.stringify(value)), maxTokens: value.max_tokens };
}

// Inspect only bounded metadata. Prompt, content and reasoning are never saved.
export class UsageProbe {
  #decoder = new StringDecoder('utf8');
  #pending = '';
  #bytes = 0;
  #usage = null;
  #invalid = false;
  #done = false;
  constructor(maxTokens) { this.maxTokens = maxTokens; }
  push(chunk) {
    this.#bytes += chunk.length;
    if (this.#bytes > 1_048_576) throw safeError();
    this.#pending += this.#decoder.write(chunk);
    let newline;
    while ((newline = this.#pending.indexOf('\n')) !== -1) {
      const line = this.#pending.slice(0, newline).replace(/\r$/, '');
      this.#pending = this.#pending.slice(newline + 1);
      if (line.length > 65_536) throw safeError();
      this.#line(line);
    }
    if (this.#pending.length > 65_536) throw safeError();
  }
  #line(line) {
    if (!line || line.startsWith(':')) return;
    if (!line.startsWith('data:')) { this.#invalid = true; return; }
    const data = line.slice(5).trim();
    if (this.#done) { this.#invalid = true; return; }
    if (data === '[DONE]') { this.#done = true; return; }
    let parsed;
    try { parsed = JSON.parse(data); } catch { this.#invalid = true; return; }
    if (!parsed || parsed.error) { this.#invalid = true; return; }
    if (parsed.usage != null) {
      const usage = { prompt_tokens: parsed.usage.prompt_tokens, completion_tokens: parsed.usage.completion_tokens, total_tokens: parsed.usage.total_tokens };
      if (!validUsage(usage, this.maxTokens) || (this.#usage && JSON.stringify(this.#usage) !== JSON.stringify(usage))) this.#invalid = true;
      else this.#usage = usage;
    }
  }
  finish() {
    this.#pending += this.#decoder.end();
    if (this.#pending.trim()) this.#invalid = true;
    return this.#done && !this.#invalid && this.#usage ? { ...this.#usage } : null;
  }
}

function realUpstream(apiKey, body, receive) {
  const request = https.request({
    hostname: 'api.deepseek.com', port: 443, path: '/v1/chat/completions', method: 'POST',
    rejectUnauthorized: true, agent: false,
    headers: { Authorization: `Bearer ${apiKey}`, 'Content-Type': 'application/json', 'Content-Length': body.length, Accept: 'text/event-stream' },
  }, receive);
  request.end(body);
  return request; // no redirects or retries; no inherited proxy configuration
}

export function createFlashGuard({ ledger, apiKey, localKey, openUpstream = realUpstream }) {
  if (!apiKey || !localKey || apiKey === localKey) throw safeError();
  const events = new EventEmitter();
  const requests = [];
  const active = new Set();
  const server = http.createServer(async (req, res) => {
    const supplied = Buffer.from(req.headers.authorization || '');
    const expected = Buffer.from(`Bearer ${localKey}`);
    const deny = () => {
      if (!res.headersSent && !res.destroyed) {
        res.writeHead(403, { 'Content-Type': 'application/json', Connection: 'close' });
        res.end('{"error":{"message":"acceptance request rejected","type":"guard_rejected"}}');
      } else res.destroy();
    };
    if (req.method !== 'POST' || req.url !== '/v1/chat/completions' || supplied.length !== expected.length || !timingSafeEqual(supplied, expected)) {
      deny(); return;
    }
    let upstream;
    let upstreamResponse;
    let timer;
    let settled = false;
    let record;
    const abort = () => { upstream?.destroy(); upstreamResponse?.destroy(); };
    const finish = (reason) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      active.delete(abort);
      if (record) {
        record.outcome = reason;
        record.duration_ms = Date.now() - record.started_at_ms;
        events.emit('finished', { ...record });
      }
    };
    const clientClosed = () => {
      if (res.writableFinished) return;
      abort(); finish('downstream_closed');
    };
    res.once('close', clientClosed);
    req.once('aborted', clientClosed);
    timer = setTimeout(() => { abort(); finish('deadline'); deny(); req.destroy(); }, 30_000);
    try {
      const chunks = [];
      let bytes = 0;
      for await (const chunk of req) {
        bytes += chunk.length;
        if (bytes > LIMITS.body) throw safeError();
        chunks.push(chunk);
      }
      if (res.destroyed || settled) throw safeError();
      const selected = flashRequest(Buffer.concat(chunks));
      const id = ledger.reserve(selected.maxTokens);
      record = { id, started_at_ms: Date.now(), max_tokens: selected.maxTokens, bytes: 0, usage_known: false, status: null };
      requests.push(record);
      active.add(abort);
      const probe = new UsageProbe(selected.maxTokens);
      upstream = openUpstream(apiKey, selected.body, (response) => {
        upstreamResponse = response;
        if (settled) { response.destroy(); return; }
        record.status = response.statusCode;
        if (response.statusCode !== 200 || !String(response.headers['content-type']).startsWith('text/event-stream')) {
          response.destroy(); finish('upstream_rejected'); deny(); return;
        }
        res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', 'X-Accel-Buffering': 'no' });
        res.flushHeaders();
        const inspect = new Transform({
          transform(chunk, encoding, callback) {
            try {
              probe.push(chunk);
              record.bytes += chunk.length;
              if (record.first_chunk_ms === undefined) {
                record.first_chunk_ms = Date.now() - record.started_at_ms;
                events.emit('first_chunk', { id, first_chunk_ms: record.first_chunk_ms });
              }
              callback(null, chunk);
            } catch { callback(safeError()); }
          },
          flush(callback) {
            try {
              const usage = probe.finish();
              if (usage && !settled) { ledger.settle(id, usage); record.usage_known = true; }
              callback();
            } catch { callback(safeError()); }
          },
        });
        // pipeline preserves bounded backpressure and tears down all three ends.
        pipeline(response, inspect, res, (error) => finish(error ? 'stream_interrupted' : 'eof'));
      });
      upstream.once('error', () => { finish('transport_error'); deny(); });
      events.emit('started', { id });
    } catch {
      abort(); finish('guard_rejected'); deny();
    }
  });
  server.requestTimeout = 30_000;
  server.headersTimeout = 5_000;
  server.on('clientError', (_, socket) => socket.destroy());
  return {
    server, events,
    snapshot: () => ({ ...ledger.snapshot(), transport: structuredClone(requests) }),
    async close() {
      for (const abort of active) abort();
      server.closeAllConnections();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}
