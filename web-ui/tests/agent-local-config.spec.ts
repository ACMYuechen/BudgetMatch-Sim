import { expect, test } from '@playwright/test'
import { chmodSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { loadLocalAgentConfig } from '../tests-local/agentConfig'

let directory = ''
const selected = {
  origin: 'http://127.0.0.1:14173', seedConversation: 'dev-demo-v1-local-fixture',
  owner: { id: 'owner', username: 'owner', password: 'synthetic-private-marker' },
  writer: { id: 'writer', username: 'writer', password: 'synthetic-private-marker' },
}
let sequence = 0
function source(value: unknown = selected) {
  const filename = path.join(directory, `${sequence++}.json`)
  writeFileSync(filename, typeof value === 'string' ? value : JSON.stringify(value), { flag: 'wx', mode: 0o600 })
  return filename
}
// Fully parallel scheduling may run afterAll between groups in one worker.
// Each test owns its directory, including repeated runs in that same worker.
test.beforeEach(() => {
  directory = mkdtempSync(path.join(os.tmpdir(), 'budgetmatch-browser-config-'))
  sequence = 0
})
test.afterEach(() => {
  if (directory) rmSync(directory, { recursive: true })
  directory = ''
})

test('live Agent browser configuration is opt-in with no implicit target', () => {
  const file = source()
  expect(() => loadLocalAgentConfig(file, '')).toThrow('explicit opt-in')
  expect(() => loadLocalAgentConfig('', '1')).toThrow('explicit opt-in')
  expect(() => loadLocalAgentConfig('relative.json', '1')).toThrow('explicit opt-in')
  expect(loadLocalAgentConfig(file, '1')).toEqual(selected)
})

test('live Agent browser configuration refuses non-loopback or ambiguous origins', () => {
  for (const origin of ['https://127.0.0.1:14173', 'http://localhost:14173', 'http://192.0.2.1:14173',
    'http://0.0.0.0:14173', 'http://127.0.0.1:80', 'http://127.0.0.1:65536', 'http://127.0.0.1:014173',
    'http://127.0.0.1:14173/', 'http://127.0.0.1:14173?x=1', 'http://user:secret@127.0.0.1:14173']) {
    expect(() => loadLocalAgentConfig(source({ ...selected, origin }), '1')).toThrow('details suppressed')
  }
})

test('live Agent browser configuration requires private regular bounded input', () => {
  const file = source()
  chmodSync(file, 0o644)
  expect(() => loadLocalAgentConfig(file, '1')).toThrow('details suppressed')
  chmodSync(file, 0o600)
  const link = path.join(directory, 'linked.json')
  symlinkSync(file, link)
  for (const filename of [link, directory, source('x'.repeat(16 * 1024 + 1)), source('invalid synthetic-private-marker')]) {
    let message = ''
    try { loadLocalAgentConfig(filename, '1') } catch (error) { message = (error as Error).message }
    expect(message).toContain('details suppressed')
    expect(message).not.toContain('synthetic-private-marker')
  }
})

test('live Agent browser configuration requires two distinct accounts and a marked seed', () => {
  for (const change of [
    { writer: selected.owner }, { writer: { ...selected.writer, id: selected.owner.id } },
    { writer: { ...selected.writer, password: '' } }, { owner: null },
    { seedConversation: 'arbitrary-history' }, { seedConversation: '../escape' },
  ]) expect(() => loadLocalAgentConfig(source({ ...selected, ...change }), '1')).toThrow('details suppressed')
})
