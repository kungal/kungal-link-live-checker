// PM2 process file for kungal-link-live-checker — no Docker needed. The binary
// is a zero-dependency static Go executable; PM2 just supervises it.
//
// Config + secrets are read from /opt/link-checker/.env (chmod 600, NOT in git):
//   LLC_ADDR=127.0.0.1:8080         # bind PRIVATE only (loopback / Tailscale IP); never 0.0.0.0
//   LLC_API_KEYS=<comma-separated>  # s2s keys, one per consumer; empty => fail-closed (all 401)
//   LLC_RATE_RPS=5
//
// Start:   cd /opt/link-checker && pm2 start ecosystem.config.js && pm2 save
// Update:  scp a fresh ./llc binary (or build on host), then `pm2 reload link-checker`
const fs = require('fs');

function readEnvFile(path) {
  const env = {};
  try {
    for (const line of fs.readFileSync(path, 'utf8').split('\n')) {
      const m = line.match(/^\s*([A-Z_][A-Z0-9_]*)\s*=\s*(.*?)\s*$/);
      if (m) env[m[1]] = m[2];
    }
  } catch (_) {
    /* file is optional — fall back to process.env */
  }
  return env;
}

const cfg = { ...process.env, ...readEnvFile('/opt/link-checker/.env') };

module.exports = {
  apps: [
    {
      name: 'link-checker',
      script: '/opt/link-checker/llc',
      cwd: '/opt/link-checker',
      env: {
        LLC_ADDR: cfg.LLC_ADDR || '127.0.0.1:8080',
        LLC_API_KEYS: cfg.LLC_API_KEYS || '',
        LLC_RATE_RPS: cfg.LLC_RATE_RPS || '5',
      },
      autorestart: true,
      max_restarts: 10,
      max_memory_restart: '128M',
      out_file: '/opt/link-checker/logs/out.log',
      error_file: '/opt/link-checker/logs/err.log',
      time: true,
    },
  ],
};
