#!/usr/bin/env python3
"""Repairs known-stale import paths in a checked-out copy of the official
UCP conformance suite (github.com/Universal-Commerce-Protocol/conformance).

Why this exists: the suite's own test files still import several things —
`payment`, `payment_create_request`, `payment_update_request`,
`types.reverse_domain_name`, `types.error_response`, at least — from
`ucp_sdk.models.schemas.shopping`, matching where they lived as of the
python-sdk's 2026-01-23 release. A later python-sdk reorganization moved
them under `ucp_sdk.models.schemas.common.types`, and the conformance
suite's main branch was never updated to follow: its own conftest fails to
import against any current python-sdk, reproducing the official README's
own walkthrough instructions verbatim (main checked out next to main, per
`git log` on both repos as of this writing). This is upstream's bug, not
Sarvex's or this script's — it patches around it so
scripts/ucp-conformance/run.sh can still exercise the suite's actual
protocol assertions while the fix works its way upstream. Delete this
script (and the run.sh step that calls it) once a `conformance` release
imports cleanly again.

Two distinct breakage shapes, both handled the same way — relocate the
dotted path, verify empirically that every file imports afterward, and
fail loudly (not silently) if anything still doesn't:

  1. `from ucp_sdk.models.schemas.shopping.payment import Payment` — the
     module named in `from ... import` no longer exists at all.
  2. `from ucp_sdk.models.schemas.shopping import payment_create_request`
     — the package still exists, but no longer has that name to hand out;
     this codebase's own convention is that a bare name imported this way
     is always a submodule, never a re-exported symbol, so a missing one
     is resolved as a submodule too.

Usage: fix_upstream_imports.py <conformance_dir> <python_sdk_repo>/src
(the second argument is the directory directly containing the top-level
ucp_sdk package, i.e. python-sdk's src/, not the repo root).
"""

import ast
import pathlib
import re
import subprocess
import sys


def module_path_exists(sdk_src: pathlib.Path, dotted: str) -> bool:
    """True if `dotted` (e.g. ucp_sdk.models.schemas.shopping.payment)
    resolves to a real file or package under sdk_src, the directory that
    directly contains the top-level ucp_sdk package."""
    rel = pathlib.Path(*dotted.split("."))
    base = sdk_src / rel
    return base.with_suffix(".py").is_file() or (base / "__init__.py").is_file()


def is_package(sdk_src: pathlib.Path, dotted: str) -> bool:
    """True if `dotted` resolves to a package directory (not a plain
    module file) — the only case where this codebase's own convention
    (module docstring) says a bare imported name must be a submodule."""
    rel = pathlib.Path(*dotted.split("."))
    return (sdk_src / rel / "__init__.py").is_file()


def find_broken_dotted_paths(conformance_dir: pathlib.Path, sdk_src: pathlib.Path) -> set[str]:
    """Every `ucp_sdk...` dotted path this suite's own files reference,
    directly or as a bare name imported from a package, that does not
    resolve against sdk_src."""
    broken = set()
    for path in sorted(conformance_dir.glob("*.py")):
        tree = ast.parse(path.read_text(), filename=str(path))
        for node in ast.walk(tree):
            if not (isinstance(node, ast.ImportFrom) and node.module and node.module.startswith("ucp_sdk.")):
                continue
            if not module_path_exists(sdk_src, node.module):
                broken.add(node.module)
                continue
            if not is_package(sdk_src, node.module):
                # A concrete module file, not a package: imported names
                # are symbols defined in it, never submodules to relocate.
                continue
            # A package; check each bare name this codebase imports from
            # it as a submodule (its own convention — see module docstring).
            for alias in node.names:
                candidate = f"{node.module}.{alias.name}"
                if not module_path_exists(sdk_src, candidate):
                    broken.add(candidate)
    return broken


def find_replacement(sdk_src: pathlib.Path, dotted: str) -> str:
    """Finds where a module that no longer resolves at `dotted` actually
    lives now, by its final path segment being unique somewhere under
    sdk_src. Raises if the segment is missing or ambiguous — a silent
    wrong guess here would be worse than a loud failure."""
    leaf = dotted.rsplit(".", 1)[-1]
    candidates = sorted((sdk_src / "ucp_sdk").rglob(f"{leaf}.py"))
    if len(candidates) != 1:
        raise RuntimeError(f"cannot uniquely relocate {dotted!r}: found {len(candidates)} file(s) named {leaf}.py under {sdk_src}")
    rel = candidates[0].relative_to(sdk_src).with_suffix("")
    return ".".join(rel.parts)


def patch_file(path: pathlib.Path, remap: dict[str, str]) -> bool:
    """Rewrites path in place for every remap entry that applies to it.
    Handles both breakage shapes: `old` naming the whole `from ... import`
    module (rewrite the module clause), and `old` naming a package plus a
    bare submodule name (rewrite the module clause, keep the name)."""
    text = path.read_text()
    changed = False
    for old, new in remap.items():
        if "." not in old:
            continue
        pattern_whole = re.compile(r"^from " + re.escape(old) + r"(?= import\b)", re.MULTILINE)
        new_text = pattern_whole.sub(f"from {new}", text)
        if new_text != text:
            text, changed = new_text, True
            continue

        pkg, name = old.rsplit(".", 1)
        new_pkg = new.rsplit(".", 1)[0]
        pattern_bare = re.compile(r"^from " + re.escape(pkg) + r" import " + re.escape(name) + r"\s*$", re.MULTILINE)
        new_text = pattern_bare.sub(f"from {new_pkg} import {name}", text)
        if new_text != text:
            text, changed = new_text, True

    if changed:
        path.write_text(text)
    return changed


def verify_imports(conformance_dir: pathlib.Path) -> list[str]:
    """Returns the .py files that still fail to import, each compiled and
    imported in its own subprocess so one bad file can't hide the rest."""
    failures = []
    for path in sorted(conformance_dir.glob("*.py")):
        code = "import importlib.util as u; s = u.spec_from_file_location('m', %r); m = u.module_from_spec(s); s.loader.exec_module(m)" % str(path)
        result = subprocess.run(
            [sys.executable, "-c", code],
            cwd=conformance_dir,
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            last_line = result.stderr.strip().splitlines()[-1] if result.stderr.strip() else "unknown error"
            failures.append(f"{path.name}: {last_line}")
    return failures


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    conformance_dir = pathlib.Path(sys.argv[1]).resolve()
    sdk_src = pathlib.Path(sys.argv[2]).resolve()

    broken = find_broken_dotted_paths(conformance_dir, sdk_src)
    if not broken:
        print("no known-stale imports found; upstream may already be fixed")
        return 0

    remap = {}
    for old in sorted(broken):
        try:
            remap[old] = find_replacement(sdk_src, old)
        except RuntimeError as e:
            print(f"error: {e}", file=sys.stderr)
            return 1
        print(f"remapping {old} -> {remap[old]}")

    patched_files = 0
    for path in sorted(conformance_dir.glob("*.py")):
        if patch_file(path, remap):
            patched_files += 1

    failures = verify_imports(conformance_dir)
    if failures:
        print("error: some files still fail to import after patching:", file=sys.stderr)
        for f in failures:
            print(f"  {f}", file=sys.stderr)
        return 1

    print(f"remapped {len(remap)} stale import path(s) across {patched_files} file(s); every .py file imports cleanly")
    return 0


if __name__ == "__main__":
    sys.exit(main())
