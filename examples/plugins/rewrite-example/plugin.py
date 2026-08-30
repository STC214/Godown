#!/usr/bin/env python3
"""Minimal Ghost Downloader JSON-RPC stdio plugin.

Try it with an example+https:// URL after copying this directory into the
application data plugins directory.
"""

import json
import sys


MANIFEST = {
    "id": "rewrite-example",
    "name": "Example URL Rewriter",
    "version": "1.0.0",
    "protocolVersion": 1,
    "executable": "python",
}


def handle(method, params):
    if method == "manifest":
        return MANIFEST
    if method == "matches":
        return {"matched": str(params.get("url", "")).startswith("example+")}
    if method == "parse":
        source = str(params.get("url", ""))
        return {
            "kind": "http",
            "url": source.removeprefix("example+"),
            "title": "example-download.bin",
        }
    raise ValueError(f"unknown method: {method}")


def main():
    request = json.loads(sys.stdin.readline())
    try:
        result = handle(request.get("method"), request.get("params") or {})
        response = {"jsonrpc": "2.0", "id": request.get("id"), "result": result}
    except Exception as exc:
        response = {
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "error": {"code": -32000, "message": str(exc)},
        }
    print(json.dumps(response), flush=True)


if __name__ == "__main__":
    main()
