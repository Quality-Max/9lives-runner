"""Fail-closed provenance checks for the test-only upstream Python oracle."""

import hashlib
import importlib.abc
import importlib.machinery
import subprocess
import sys
from pathlib import Path
from types import ModuleType

COMPILED = {}

PINS = {
    "0.1.3": "8a40d8d5c83f27f84384f060aeed74a3ded7ab77",
    "0.2.1": "568c7a6882441c13cdb9bfe8c0190ca0bf7d8240",
}


def prepare(source, version):
    """Verify an exact clean checkout, then protect all ninelives imports."""
    source = Path(source).resolve()
    assert source.name == "src", "oracle requires the checkout src directory"
    verify_checkout(source.parent, PINS[version])
    install_source_only_imports(source)


def load_source(name, path):
    """Load an owned .py module from source without consulting bytecode caches."""
    path = Path(path).resolve()
    module = ModuleType(name)
    module.__file__ = str(path)
    sys.modules[name] = module
    exec(compile(path.read_bytes(), str(path), "exec"), module.__dict__)
    return module


def verify_checkout(directory, revision):
    directory = Path(directory).resolve()

    def git(*arguments):
        return subprocess.check_output(["git", "-C", str(directory), *arguments], text=True, timeout=10).strip()

    assert Path(git("rev-parse", "--show-toplevel")).resolve() == directory, (
        "upstream directory must be its own exact Git root"
    )
    assert git("rev-parse", "HEAD") == revision, "upstream revision is not pinned"
    assert not git("status", "--porcelain", "--untracked-files=all", "--", "src"), (
        "upstream src must have no tracked or untracked changes"
    )
    # Git ignores must not hide an extra importable module.
    tracked = set(git("ls-files", "--", "src").splitlines())
    for path in (directory / "src").rglob("*"):
        importable = path.suffix in {".py", ".pyi", ".so", ".pyd"} or (
            path.suffix == ".pyc" and "__pycache__" not in path.parts
        )
        if path.is_file() and importable:
            assert path.relative_to(directory).as_posix() in tracked, "upstream contains an untracked importable source"


def verify_import_identity(source, version, package, modules=()):
    source = Path(source).resolve()
    assert Path(package.__file__).resolve() == source / "ninelives/__init__.py", (
        "oracle imported an unexpected ninelives package"
    )
    assert package.__version__ == {"0.1.3": "0.1.0", "0.2.1": "0.2.1"}[version], "oracle runtime version mismatch"
    for module in modules:
        assert getattr(module, "__file__", None), "owned module has no source identity"
        assert Path(module.__file__).resolve().is_relative_to(source / "ninelives"), (
            "oracle helper imported outside pinned ninelives source"
        )


class PinnedSourceLoader(importlib.machinery.SourceFileLoader):
    """Compile this namespace's source, never read or write bytecode caches."""

    def get_code(self, fullname):
        data = self.get_data(self.path)
        COMPILED[fullname] = (str(Path(self.path).resolve()), hashlib.sha256(data).hexdigest())
        return self.source_to_code(data, self.path)


class PinnedSourceFinder(importlib.abc.MetaPathFinder):
    def __init__(self, source):
        self.package = Path(source).resolve() / "ninelives"

    def find_spec(self, fullname, path=None, target=None):
        if fullname != "ninelives" and not fullname.startswith("ninelives."):
            return None
        search = [str(self.package.parent)] if fullname == "ninelives" else path
        spec = importlib.machinery.PathFinder.find_spec(fullname, search)
        assert spec is not None and spec.origin, "pinned helper source missing"
        origin = Path(spec.origin).resolve()
        assert origin.is_relative_to(self.package), "helper outside pinned source"
        assert origin.suffix == ".py", "pinned helper must be Python source"
        spec.loader = PinnedSourceLoader(fullname, str(origin))
        return spec


def install_source_only_imports(source):
    assert not any(name == "ninelives" or name.startswith("ninelives.") for name in sys.modules), (
        "pinned namespace imported before source-only protection"
    )
    sys.meta_path.insert(0, PinnedSourceFinder(source))


def verify_loaded_identity(source, version):
    package = sys.modules["ninelives"]
    helpers = [module for name, module in sys.modules.items() if name.startswith("ninelives.")]
    verify_import_identity(source, version, package, helpers)
    verify_checkout(Path(source).resolve().parent, PINS[version])
    # Return bounded provenance, never provider configuration. Digests bind the
    # actual imported sources, including every transitive owned helper.
    modules = [package, *helpers]
    for module in modules:
        path = Path(module.__file__).resolve()
        assert COMPILED.get(module.__name__) == (str(path), hashlib.sha256(path.read_bytes()).hexdigest()), (
            "owned source changed or bypassed protected loader"
        )
    return {
        "revision": PINS[version],
        "version": version,
        "packageVersion": package.__version__,
        "modules": {
            module.__name__: {
                "path": str(Path(module.__file__).resolve().relative_to(Path(source).resolve())),
                "sha256": hashlib.sha256(Path(module.__file__).read_bytes()).hexdigest(),
            }
            for module in sorted(modules, key=lambda module: module.__name__)
        },
        "sourceOnly": True,
    }


def evaluate_requests(source, version, requests):
    """Invoke real pinned classification, parsing, strategy and direct Tier 1."""
    prepare(source, version)
    import asyncio
    from dataclasses import fields

    from ninelives.healing.parse import extract_failed_selector
    from ninelives.healing.strategy import FailureType, HealingStrategySelector, TestFailure
    from ninelives.healing.tier1 import Tier1LocatorHealer

    strategy = HealingStrategySelector()
    results = []
    for request in requests:
        message, stack = request.get("errorMessage", ""), request.get("stackTrace", "")
        classified = strategy.classify_failure(message, stack)
        extracted = extract_failed_selector(message, stack) or ""
        arguments = dict(
            failure_type=FailureType(request.get("failureType") or classified.value),
            error_message=message,
            stack_trace=stack,
            failed_selector=request.get("failedSelector") or extracted or None,
            test_code=request.get("testCode", ""),
            page_html=request.get("pageSnapshot", ""),
        )
        if "framework" in {field.name for field in fields(TestFailure)}:
            arguments["framework"] = request.get("framework", "playwright")
        failure = TestFailure(**arguments)
        result = asyncio.run(Tier1LocatorHealer().heal(failure))
        results.append(
            dict(
                classified=classified.value,
                extracted=extracted,
                selectedTier=strategy.select_strategy(failure).value,
                success=result.success,
                code=result.healed_code or "",
                confidence=result.confidence,
                metadata=result.metadata,
                requiresApproval=result.requires_approval,
                changes=result.changes_made,
            )
        )
    return {"results": results, "provenance": verify_loaded_identity(source, version)}


if __name__ == "__main__":
    import json

    print(json.dumps(evaluate_requests(sys.argv[1], sys.argv[2], json.load(sys.stdin))))
