import { defineConfig, devices } from '@playwright/test'
import { chmodSync, mkdtempSync } from 'node:fs'
import path from 'node:path'
import { loadLocalAgentConfig } from './tests-local/agentConfig'

const selected = loadLocalAgentConfig()
// A fresh private output directory for EVERY invocation; never erase prior
// browser evidence or let Playwright clean a user-provided broad directory.
const outputDir = mkdtempSync(path.join(path.dirname(process.env.AGENT_LOCAL_E2E_CONFIG!), 'browser-results-'))
chmodSync(outputDir, 0o700)

export default defineConfig({
  testDir: './tests-local',
  testMatch: 'agent-retained.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  reporter: 'list',
  outputDir,
  use: {
    baseURL: selected.origin,
    // Authentication is real: do not retain tokens/passwords in traces/HARs.
    trace: 'off', video: 'off', screenshot: 'off', serviceWorkers: 'block',
  },
  projects: [{ name: 'local-agent-chromium', use: { ...devices['Desktop Chrome'] } }],
  // No webServer/reuseExistingServer: the operator owns this isolated stack.
})
