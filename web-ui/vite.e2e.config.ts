import { defineConfig } from 'vite'
import base from './vite.config'

// Browser fixtures must never load deployment env files or proxy real APIs.
export default defineConfig({ ...base, envDir: false, server: { ...base.server, proxy: undefined } })
