#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Add or check the SPDX licence header on first-party source files.

    scripts/license-headers.py          add missing headers
    scripts/license-headers.py --check  exit 1 listing files without one

Covers Go, TypeScript, Vue and PHP sources tracked by git. Generated files
and test fixtures are skipped.
"""
import subprocess
import sys

HOLDER = "Sarvex Labs"
YEAR = "2026"
SPDX = "SPDX-License-Identifier: Apache-2.0"
SKIP_PREFIXES = (
    "web/console/node_modules/", "web/console/dist/",
)
SKIP_PARTS = ("/testdata/", "/node_modules/", "/vendor/")


def skip(path: str) -> bool:
    if path.startswith(SKIP_PREFIXES) or any(p in "/" + path for p in SKIP_PARTS):
        return True
    return path.endswith(".d.ts")


def header_for(path: str):
    line = f"Copyright {YEAR} {HOLDER}"
    if path.endswith((".go", ".ts")):
        return f"// {line}\n// {SPDX}\n\n"
    if path.endswith(".vue"):
        return f"<!--\n  {line}\n  {SPDX}\n-->\n"
    return None


def main() -> int:
    check = "--check" in sys.argv
    files = subprocess.check_output(["git", "ls-files"], text=True).splitlines()
    missing = []
    for path in files:
        if skip(path) or not path.endswith((".go", ".ts", ".vue", ".php")):
            continue
        try:
            with open(path, encoding="utf-8") as f:
                text = f.read()
        except (OSError, UnicodeDecodeError):
            continue
        if SPDX in text[:600] or "Code generated" in text[:400] or "DO NOT EDIT" in text[:400]:
            continue
        missing.append(path)
        if check:
            continue
        if path.endswith(".php"):
            if not text.startswith("<?php"):
                continue
            body = text[len("<?php"):].lstrip("\n")
            new = f"<?php\n// Copyright {YEAR} {HOLDER}\n// {SPDX}\n\n" + body
        else:
            new = header_for(path) + text
        with open(path, "w", encoding="utf-8") as f:
            f.write(new)
    if check and missing:
        print("missing SPDX header:\n  " + "\n  ".join(missing))
        return 1
    if not check:
        print(f"updated {len(missing)} files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
