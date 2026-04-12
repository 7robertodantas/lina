# Grafana dashboard panel export

[`render_dashboard_panels.py`](./render_dashboard_panels.py) exports **every visualization panel** from a Grafana dashboard JSON file to **PNG** images using Grafana’s image renderer (`GET /render/d-solo/...`).

## Prerequisites

- Grafana reachable over HTTP(S) (e.g. `http://localhost:3000`).
- [Grafana Image Renderer](https://github.com/grafana/grafana-image-renderer) configured (e.g. `GF_RENDERING_SERVER_URL` and `GF_RENDERING_CALLBACK_URL` in Docker Compose).
- Python 3.9+ (stdlib only).

## Time range

Provide the same JSON object either as a **file** (`--range-json`) or an **inline string** (`--range`). You must pass exactly one of these.

**Absolute:** ISO-8601 instants (UTC `Z` is supported) or numeric epoch **milliseconds** in JSON. Those are sent to Grafana as millisecond strings (same as the UI when you pick fixed times).

```json
{
  "from": "2026-04-12T18:11:45.000Z",
  "to": "2026-04-12T18:54:49.000Z"
}
```

**Relative:** Use the same expressions as Grafana’s time picker / dashboard URL, e.g. `now`, `now-1h`, `now-7d`, `now/d` (start of day). Any `from`/`to` string that is **not** valid ISO is passed through unchanged.

```json
{
  "from": "now-1h",
  "to": "now"
}
```

Exports then reflect “last hour” (or whatever window you set) at the moment the script runs, not a fixed historical window.

**X-axis and legends.** Relative `from`/`to` only tells Grafana **which time window** to query (the same sliding window as “Last 1 hour” in the UI). Grafana still **resolves** that to concrete `from`/`to` instants when rendering. Time series panels therefore show the **usual clock times on the X-axis** (e.g. 20:15 … 21:15 in your timezone)—not tick labels like “−60m … 0”. Series **legends** (metric names, labels) come from the panel queries, not from relative vs absolute range. To match the live dashboard for a given export, keep **`--tz`** aligned with the dashboard time zone and watch for **per-panel time overrides** in the dashboard JSON, which can ignore the global range.

Inline example (quote the JSON so the shell parses it as one argument):

```bash
./render_dashboard_panels.py \
  --dashboard-json ../dashboards/monitoring-systemd.json \
  --range '{"from":"2026-04-12T18:11:45.000Z","to":"2026-04-12T18:54:49.000Z"}' \
  --out-dir ./panel-images \
  --base-url http://localhost:3000 \
  --user admin \
  --password admin
```

## Example

From this directory:

```bash
./render_dashboard_panels.py \
  --dashboard-json ../dashboards/monitoring-systemd.json \
  --range-json ./range.json \
  --out-dir ./panel-images \
  --base-url http://localhost:3000 \
  --user admin \
  --password admin \
  --tz Europe/Zurich \
  --theme light
```

- **Dashboard JSON**: export from the Grafana UI (JSON model) or API; the script reads `uid` from the root object (override with `--uid` if needed).
- **Output files**: names like `0006_Disk_Throughput.png` (panel id + sanitized title). Row panels are skipped; nested panels inside rows are included when present.

### Template variables

If the dashboard uses variables (e.g. `instance`), pass them like Grafana’s `var-*` query params:

```bash
./render_dashboard_panels.py \
  --dashboard-json ../dashboards/monitoring-systemd.json \
  --range-json ./range.json \
  --out-dir ./panel-images \
  --base-url http://localhost:3000 \
  --user admin \
  --password admin \
  --var instance='.*'
```

Repeat `--var KEY=VALUE` for each variable.

### Common options

| Option | Default | Description |
|--------|---------|-------------|
| `--width` | 600 | Render width (px) |
| `--height` | 448 | Render height (px) |
| `--timeout` | 120 | Panel capture timeout in seconds (passed to Grafana as `timeout=`; forwarded to the image renderer) |
| `--org-id` | 1 | Grafana organization |
| `--theme` | light | `light` or `dark` |

Run `./render_dashboard_panels.py --help` for the full list.

## Troubleshooting

**Responses look like HTML (`<!DOCTYPE html>`, title Grafana) instead of PNG.** Grafana often answers `/render/...` with **200** and the web app shell when the request is not authenticated—it does not always return **401**, so clients that only send Basic auth after a challenge never send credentials. The script sends `Authorization: Basic …` on every request when you pass `--user` / `--password` (same idea as `curl http://user:pass@host/...`). Ensure those flags match your Grafana admin user.

**HTTP 500, or renderer logs with `status=408` / “Request Timeout” after ~30–40s** even when the URL shows `timeout=120`. The **grafana-image-renderer** service uses a separate default, **`BROWSER_READINESS_TIMEOUT`** (**30s**), for how long the headless browser may wait for the page to become ready ([docs](https://grafana.com/docs/grafana/latest/setup-grafana/image-rendering/flags/)). That cap must be raised on the **renderer** container (e.g. `BROWSER_READINESS_TIMEOUT=120s`). Also set **`GF_RENDERING_TIMEOUT`** on Grafana (seconds) so Grafana keeps waiting for the renderer long enough. This repo’s `deployment/docker-compose.evaluation.external.yml` sets both. You can still raise the script’s `--timeout` so Grafana passes a larger `timeout=` into the render request; the renderer and Grafana env vars must align with that.

## Single-panel curl (reference)

Equivalent to one panel from the script (replace `panelId`, times, and credentials):

```bash
curl "http://admin:admin@localhost:3000/render/d-solo/lina-systemd?orgId=1&from=<ms>&to=<ms>&panelId=6&width=600&height=448&tz=Europe%2FZurich&theme=light&timeout=120" -o chart.png
```

Relative example: `from=now-1h&to=now` instead of millisecond `from`/`to`.

Rendering many panels issues one request per panel and can take several minutes; the script sets an HTTP client wait at least `--timeout` plus a buffer so Grafana can finish waiting on the image renderer.
