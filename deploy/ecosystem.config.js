// PM2 process file for kungal-link-live-checker — no Docker needed. The binary
// is a zero-dependency static Go executable; PM2 just supervises it.
//
// First start (pass secrets via the shell env; never commit them):
//   cd /opt/link-checker && \
//   LLC_API_KEYS=<comma-separated-keys> pm2 start src/deploy/ecosystem.config.js && pm2 save
//
// `pm2 save` persists the captured env for reboot (`pm2 resurrect` / `pm2 startup`).
// Updates afterwards: src/deploy/deploy.sh (git pull + build + `pm2 reload`).
module.exports = {
  apps: [
    {
      name: 'link-checker',
      script: '/opt/link-checker/llc',
      cwd: '/opt/link-checker',
      env: {
        // Bind PRIVATE only. 127.0.0.1 = reachable via an SSH reverse tunnel;
        // set a Tailscale/WireGuard IP to expose over the private mesh instead.
        // Never bind 0.0.0.0 — this service must not be on the public internet.
        LLC_ADDR: process.env.LLC_ADDR || '127.0.0.1:8080',
        // s2s API keys, comma-separated, one per consumer. Empty => fail-closed (all 401).
        LLC_API_KEYS: process.env.LLC_API_KEYS,
        LLC_RATE_RPS: process.env.LLC_RATE_RPS || '5',
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
