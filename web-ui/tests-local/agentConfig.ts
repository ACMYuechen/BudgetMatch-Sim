import { constants, fstatSync, lstatSync, openSync, closeSync, readSync } from 'node:fs'
import path from 'node:path'

export interface LocalAgentUser { id: string; username: string; password: string }
export interface LocalAgentConfig {
  origin: string
  owner: LocalAgentUser
  writer: LocalAgentUser
  seedConversation: string
}

function refused(): never { throw new Error('Local Agent acceptance requires explicit opt-in and a valid private loopback configuration; details suppressed') }

// This is an operator-selected browser target, not proof of the upstream DB or
// model configuration. Verify those separately before authorizing retained writes.
export function loadLocalAgentConfig(
  source = process.env.AGENT_LOCAL_E2E_CONFIG,
  allowed = process.env.AGENT_LOCAL_E2E_ALLOW_RETAINED_WRITES
): LocalAgentConfig {
  if (allowed !== '1' || !source || !path.isAbsolute(source)) return refused()
  let data: unknown
  let fd: number | undefined
  try {
    const info = lstatSync(source)
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) || info.size > 16 * 1024) return refused()
    fd = openSync(source, constants.O_RDONLY | constants.O_NOFOLLOW)
    const opened = fstatSync(fd)
    if (opened.dev !== info.dev || opened.ino !== info.ino || (opened.mode & 0o077)) return refused()
    const buffer = Buffer.alloc(16 * 1024 + 1)
    const length = readSync(fd, buffer, 0, buffer.length, 0)
    if (length > 16 * 1024) return refused()
    data = JSON.parse(buffer.subarray(0, length).toString('utf8'))
  } catch {
    return refused()
  } finally {
    if (fd !== undefined) closeSync(fd)
  }
  if (!data || typeof data !== 'object' || Array.isArray(data)) return refused()
  const selected = data as Record<string, unknown>
  const user = (value: unknown): LocalAgentUser => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return refused()
    const candidate = value as Record<string, unknown>
    if (typeof candidate.id !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(candidate.id) ||
      typeof candidate.username !== 'string' || !/^[A-Za-z0-9_-]{1,50}$/.test(candidate.username) ||
      typeof candidate.password !== 'string' || !candidate.password || candidate.password.length > 256) return refused()
    return { id: candidate.id, username: candidate.username, password: candidate.password }
  }
  const owner = user(selected.owner)
  const writer = user(selected.writer)
  if (owner.id === writer.id || owner.username === writer.username) return refused()
  if (typeof selected.origin !== 'string' || !/^http:\/\/127\.0\.0\.1:[1-9][0-9]{3,4}$/.test(selected.origin)) return refused()
  const port = Number(selected.origin.slice(selected.origin.lastIndexOf(':') + 1))
  if (port < 1024 || port > 65535) return refused()
  if (typeof selected.seedConversation !== 'string' || !/^dev-demo-v1-[a-z0-9][a-z0-9_-]{0,47}$/.test(selected.seedConversation)) return refused()
  return { origin: selected.origin, owner, writer, seedConversation: selected.seedConversation }
}
