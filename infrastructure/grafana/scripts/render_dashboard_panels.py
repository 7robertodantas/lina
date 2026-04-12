#!/usr/bin/env python3
"""
Export every visualization panel from a Grafana dashboard JSON as PNG via the
image renderer (/render/d-solo/...).

Example:
  ./render_dashboard_panels.py \\
    --dashboard-json ../dashboards/monitoring-systemd.json \\
    --range-json range.json \\
    --out-dir ./panel-images \\
    --base-url http://localhost:3000 \\
    --user admin --password admin

  # or inline:
  ./render_dashboard_panels.py ... \\
    --range '{"from":"2026-04-12T18:11:45.000Z","to":"2026-04-12T18:54:49.000Z"}'
"""

from __future__ import annotations

import argparse
import base64
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def _parse_iso_utc(s: str) -> datetime:
    s = s.strip()
    if s.endswith("Z"):
        s = s[:-1] + "+00:00"
    dt = datetime.fromisoformat(s)
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.astimezone(timezone.utc)


def _ms(dt: datetime) -> int:
    return int(dt.timestamp() * 1000)


def _walk_renderable_panels(panels: list[dict[str, Any]]) -> list[tuple[int, str]]:
    """Collect (panel_id, title) for panels that are not layout rows."""
    out: list[tuple[int, str]] = []
    for p in panels:
        ptype = p.get("type")
        if ptype == "row":
            nested = p.get("panels") or []
            out.extend(_walk_renderable_panels(nested))
            continue
        pid = p.get("id")
        if pid is None:
            continue
        title = p.get("title") or f"panel-{pid}"
        out.append((int(pid), title))
    return out


def _slug_filename(panel_id: int, title: str) -> str:
    base = re.sub(r"[^a-zA-Z0-9._-]+", "_", title.strip())
    base = base.strip("_") or "untitled"
    if len(base) > 120:
        base = base[:120].rstrip("_")
    return f"{panel_id:04d}_{base}.png"


def _build_render_url(
    base: str,
    uid: str,
    org_id: int,
    panel_id: int,
    from_ms: int,
    to_ms: int,
    width: int,
    height: int,
    tz: str,
    theme: str,
    timeout_sec: int,
    extra_query: list[tuple[str, str]],
) -> str:
    path = f"/render/d-solo/{urllib.parse.quote(uid, safe='')}"
    q: list[tuple[str, str]] = [
        ("orgId", str(org_id)),
        ("from", str(from_ms)),
        ("to", str(to_ms)),
        ("panelId", str(panel_id)),
        ("width", str(width)),
        ("height", str(height)),
        ("tz", tz),
        ("theme", theme),
        ("timeout", str(timeout_sec)),
    ]
    q.extend(extra_query)
    query = urllib.parse.urlencode(q)
    base = base.rstrip("/")
    return f"{base}{path}?{query}"


def _parse_range_object(data: dict[str, Any]) -> tuple[int, int]:
    raw_from = data.get("from")
    raw_to = data.get("to")
    if raw_from is None or raw_to is None:
        raise SystemExit('range JSON must include "from" and "to" (ISO-8601 strings)')
    return _ms(_parse_iso_utc(str(raw_from))), _ms(_parse_iso_utc(str(raw_to)))


def _basic_auth_header(user: str, password: str) -> str:
    raw = f"{user}:{password}".encode("utf-8")
    return "Basic " + base64.b64encode(raw).decode("ascii")


def _parse_range_json_text(text: str) -> tuple[int, int]:
    try:
        data = json.loads(text)
    except json.JSONDecodeError as e:
        raise SystemExit(f"Invalid range JSON: {e}") from e
    if not isinstance(data, dict):
        raise SystemExit("range JSON must be an object with from/to")
    return _parse_range_object(data)


def main() -> None:
    ap = argparse.ArgumentParser(description="Render all Grafana dashboard panels to PNG files.")
    ap.add_argument(
        "--dashboard-json",
        type=Path,
        required=True,
        help="Path to exported dashboard JSON (e.g. from UI or API).",
    )
    range_src = ap.add_mutually_exclusive_group(required=True)
    range_src.add_argument(
        "--range-json",
        type=Path,
        metavar="PATH",
        help='JSON file: {"from":"...ISO...","to":"...ISO..."}',
    )
    range_src.add_argument(
        "--range",
        metavar="JSON",
        help='Same object as --range-json but inline, e.g. \'{"from":"2026-04-12T18:11:45.000Z","to":"2026-04-12T18:54:49.000Z"}\'',
    )
    ap.add_argument("--out-dir", type=Path, required=True, help="Directory for PNG files.")
    ap.add_argument("--base-url", default="http://localhost:3000", help="Grafana root URL.")
    ap.add_argument("--user", default="", help="Basic auth username (optional).")
    ap.add_argument("--password", default="", help="Basic auth password (optional).")
    ap.add_argument("--uid", default="", help="Dashboard UID (default: read from JSON).")
    ap.add_argument("--org-id", type=int, default=1, help="Grafana org id.")
    ap.add_argument("--width", type=int, default=600)
    ap.add_argument("--height", type=int, default=448)
    ap.add_argument("--tz", default="Europe/Zurich", help="Timezone query param for rendering.")
    ap.add_argument("--theme", default="light", choices=("light", "dark"))
    ap.add_argument(
        "--timeout",
        type=int,
        default=120,
        metavar="SEC",
        help="Panel render timeout in seconds (Grafana query param; forwarded to image renderer). "
        "Raise if you see HTTP 408/500 from slow queries or Prometheus.",
    )
    ap.add_argument(
        "--var",
        action="append",
        default=[],
        metavar="KEY=VALUE",
        help="Template variable (repeatable), e.g. --var instance=.*",
    )
    args = ap.parse_args()

    dash = json.loads(args.dashboard_json.read_text(encoding="utf-8"))
    uid = args.uid or dash.get("uid")
    if not uid:
        raise SystemExit("Dashboard JSON has no uid; pass --uid.")

    panels = dash.get("panels")
    if not isinstance(panels, list):
        raise SystemExit("Invalid dashboard JSON: missing panels array.")

    items = _walk_renderable_panels(panels)
    if not items:
        raise SystemExit("No renderable panels found.")

    range_text = (
        args.range_json.read_text(encoding="utf-8")
        if args.range_json is not None
        else args.range
    )
    from_ms, to_ms = _parse_range_json_text(range_text)
    if from_ms >= to_ms:
        raise SystemExit("Invalid range: from must be before to.")

    extra_q: list[tuple[str, str]] = []
    for raw in args.var:
        if "=" not in raw:
            raise SystemExit(f"Invalid --var (expected KEY=VALUE): {raw!r}")
        k, v = raw.split("=", 1)
        extra_q.append((f"var-{urllib.parse.quote(k)}", v))

    args.out_dir.mkdir(parents=True, exist_ok=True)

    # Grafana often returns 200 + HTML for unauthenticated /render requests (no 401),
    # so HTTPBasicAuthHandler never sends credentials. Match curl user:pass@ by sending
    # Authorization on the first request.
    auth_header: str | None = None
    if args.user:
        auth_header = _basic_auth_header(args.user, args.password)

    # Wait longer than the renderer: urllib timeout is for the whole HTTP request to Grafana.
    http_wait_sec = max(300, args.timeout + 90)

    ok = 0
    for panel_id, title in sorted(items, key=lambda x: x[0]):
        url = _build_render_url(
            args.base_url,
            uid,
            args.org_id,
            panel_id,
            from_ms,
            to_ms,
            args.width,
            args.height,
            args.tz,
            args.theme,
            args.timeout,
            extra_q,
        )
        out_path = args.out_dir / _slug_filename(panel_id, title)
        req = urllib.request.Request(url)
        if auth_header:
            req.add_header("Authorization", auth_header)
        try:
            with urllib.request.urlopen(req, timeout=http_wait_sec) as resp:
                body = resp.read()
            if not body.startswith(b"\x89PNG"):
                snippet = body[:400].decode("utf-8", errors="replace")
                print(
                    f"FAIL panel {panel_id} {title!r}: not a PNG (body starts: {snippet!r})",
                    file=sys.stderr,
                )
                continue
        except urllib.error.HTTPError as e:
            detail = ""
            try:
                chunk = e.read(800)
                if chunk:
                    detail = " " + chunk.decode("utf-8", errors="replace").strip().replace("\n", " ")
            except OSError:
                pass
            print(f"FAIL panel {panel_id} {title!r}: HTTP {e.code}{detail}", file=sys.stderr)
            continue
        except OSError as e:
            print(f"FAIL panel {panel_id} {title!r}: {e}", file=sys.stderr)
            continue

        out_path.write_bytes(body)
        print(f"OK {out_path.name}")
        ok += 1

    if ok == 0:
        raise SystemExit(1)
    print(f"Wrote {ok} image(s) to {args.out_dir.resolve()}")


if __name__ == "__main__":
    main()
