#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Check the licence of every third-party Go module the repository builds
against (including test dependencies) and, with --npm, the console's
production npm packages.

Exits non-zero if a module has no licence file or is under a licence that
is incompatible with distributing Sarvex under Apache-2.0 (GPL, AGPL,
SSPL, BUSL, ...). MPL-2.0 (file-level copyleft) is reported but allowed.

    scripts/check-licenses.py          # Go modules
    scripts/check-licenses.py --npm    # also the console's production deps
    scripts/check-licenses.py --tsv    # print "module<TAB>version<TAB>licence"

Use --tsv output to refresh THIRD_PARTY_NOTICES.md when dependencies change.
"""
import glob
import os
import re
import subprocess
import sys

DENY = {"GPL", "LGPL", "AGPL", "SSPL", "BUSL"}


def classify(text: str) -> str:
    t = re.sub(r"\s+", " ", text.lower())
    if "apache license" in t and "version 2.0" in t:
        return "Apache-2.0"
    if "mozilla public license" in t:
        return "MPL-2.0" if "version 2.0" in t else "MPL"
    if "gnu affero" in t:
        return "AGPL"
    if "gnu lesser general" in t:
        return "LGPL"
    if "gnu general public" in t:
        return "GPL"
    if "server side public" in t:
        return "SSPL"
    if "business source license" in t:
        return "BUSL"
    if "permission is hereby granted, free of charge" in t:
        return "MIT"
    if "isc license" in t or "permission to use, copy, modify, and/or distribute" in t:
        return "ISC"
    if "redistribution and use in source and binary forms" in t:
        return "BSD-3-Clause" if ("neither the name" in t or "endorse" in t) else "BSD-2-Clause"
    if "unlicense" in t or "public domain" in t:
        return "Unlicense"
    return "UNKNOWN"


def go_modules():
    out = subprocess.check_output(
        ["go", "list", "-deps", "-test", "-f",
         "{{if not .Standard}}{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}", "./..."],
        text=True, stderr=subprocess.DEVNULL)
    for line in sorted(set(out.splitlines())):
        if line and not line.startswith("github.com/sarvexlabs/"):
            yield line.split("|")


def main() -> int:
    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
    tsv = "--tsv" in sys.argv
    bad = 0
    for path, version, directory in go_modules():
        files = [f for f in glob.glob(directory + "/*")
                 if re.match(r"(licen[cs]e|copying|unlicense)", os.path.basename(f), re.I) and os.path.isfile(f)]
        lic = "NONE"
        for f in sorted(files):
            c = classify(open(f, errors="ignore").read())
            lic = c
            if c != "UNKNOWN":
                break
        if tsv:
            print(f"{path}\t{version}\t{lic}")
        elif lic in DENY or lic in ("NONE", "UNKNOWN"):
            print(f"REVIEW {path} {version}: {lic}")
            bad = 1
    if "--npm" in sys.argv:
        deny = "GPL-2.0;GPL-3.0;AGPL-3.0;AGPL-3.0-or-later;GPL-3.0-or-later;SSPL-1.0;BUSL-1.1"
        rc = subprocess.call(["npx", "--yes", "license-checker-rseidelsohn", "--production",
                              "--summary", "--failOn", deny], cwd="web/console")
        bad = bad or rc
    if not tsv and not bad:
        print("licence check passed")
    return bad


if __name__ == "__main__":
    sys.exit(main())
