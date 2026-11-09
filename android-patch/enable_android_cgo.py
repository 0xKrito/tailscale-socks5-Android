#!/usr/bin/env python3
"""Enable Tailscale androidbin netmon fallback in Android cgo gomobile builds.

Upstream intentionally omits register.go for android && cgo because the
official Tailscale Android app supplies a Java interface getter. Embedded
tsnet apps do not have that registration. Patch only the isolated Tailscale
module copy, never the global Go module cache.
"""
from pathlib import Path
import sys


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: enable_android_cgo.py <copied-tailscale-module>")
    module_dir = Path(sys.argv[1]).resolve()
    register = module_dir / "feature" / "androidbin" / "register.go"
    original = "//go:build (linux && !android) || (android && !cgo)"
    replacement = "//go:build (linux && !android) || android"
    content = register.read_text()
    if content.count(original) == 1:
        register.write_text(content.replace(original, replacement, 1))
        print("Enabled androidbin fallback for Android cgo")
    elif content.count(replacement) == 1:
        print("Android cgo fallback already enabled")
    else:
        raise SystemExit("Unexpected Tailscale build constraint; refusing to patch")

if __name__ == "__main__":
    main()
