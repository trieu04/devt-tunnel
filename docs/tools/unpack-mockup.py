#!/usr/bin/env python3
"""Unpack docs/devTTunel UI.html into one HTML file per board (plus fonts/assets).

Usage: python3 docs/tools/unpack-mockup.py [out_dir]   (default: /tmp/devttunel-mockup)
Writes <out_dir>/NN-<title>.html in page_order, and assets under <out_dir>/assets/.
"""
import base64, gzip, json, re, sys, pathlib

src = pathlib.Path(__file__).resolve().parents[1] / "devTTunel UI.html"
out = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "/tmp/devttunel-mockup")
html = src.read_text(encoding="utf8")

def block(kind):
    m = re.search(r'<script type="__bundler/%s"[^>]*>(.*?)</script>' % kind, html, re.S)
    return json.loads(m.group(1))

manifest, order = block("manifest"), block("page_order")
(out / "assets").mkdir(parents=True, exist_ok=True)
ext = {"text/html": ".html", "font/woff2": ".woff2", "text/css": ".css", "image/png": ".png", "image/svg+xml": ".svg"}
for i, key in enumerate(order, 1):
    e = manifest[key]
    raw = base64.b64decode(e["data"])
    if e.get("compressed"):
        raw = gzip.decompress(raw)
    text = raw.decode("utf8", "replace")
    t = re.search(r"<title>(.*?)</title>", text, re.S)
    slug = re.sub(r"[^A-Za-z0-9]+", "-", t.group(1)).strip("-") if t else key[:8]
    (out / f"{i:02d}-{slug}.html").write_text(text, encoding="utf8")
    print(f"{i:02d}", key, len(text), t.group(1) if t else "")
for key, e in manifest.items():
    if key in order:
        continue
    raw = base64.b64decode(e["data"])
    if e.get("compressed"):
        raw = gzip.decompress(raw)
    (out / "assets" / (key + ext.get(e["mime"], ".bin"))).write_bytes(raw)
print("assets:", len(manifest) - len(order), "->", out)
