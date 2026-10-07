package contracttest

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// Each test uses a private clone of an actual known checkout. The real
// installed package and oracle checkouts are never modified by cache attacks.
func TestPinnedPythonOracleIdentityAndCaches(t *testing.T) {
	python := Python(t)
	source := UpstreamSource(t, "ninelives-0.2.1")
	program := `
import sys,subprocess,types,hashlib,importlib.util,importlib._bootstrap_external
from pathlib import Path
helper=types.ModuleType("owned_oracle")
exec(compile(Path(sys.argv[1]).read_bytes(),sys.argv[1],"exec"),helper.__dict__)
original,case,target=Path(sys.argv[2]).resolve(),sys.argv[3],Path(sys.argv[4]).resolve()
subprocess.run(["git","clone","--quiet","--no-hardlinks",str(original.parent),str(target)],check=True)
source=target/"src"
request={"framework":"playwright","failureType":"locator_not_found","errorMessage":"locator not found","failedSelector":"#save","testCode":"await page.locator('#save').click();","pageSnapshot":"<button id='save-new'>x</button>"}
if case=="poisoned-cache":
    for path in [source/"ninelives/__init__.py",source/"ninelives/healing/tier1.py"]:
        cache=Path(importlib.util.cache_from_source(str(path))); cache.parent.mkdir(parents=True,exist_ok=True)
        code=compile("raise RuntimeError('poisoned bytecode executed')",str(path),"exec")
        cache.write_bytes(importlib._bootstrap_external._code_to_timestamp_pyc(code,int(path.stat().st_mtime),path.stat().st_size))
    result=helper.evaluate_requests(source,"0.2.1",[request])
    assert result["results"][0]["success"] and result["provenance"]["sourceOnly"]
    assert "ninelives.healing.tier1" in result["provenance"]["modules"]
    for entry in result["provenance"]["modules"].values():
        assert entry["sha256"]==hashlib.sha256((source/entry["path"]).read_bytes()).hexdigest()
    sys.exit(0)
try:
    if case=="wrong-pin": helper.prepare(source,"0.1.3")
    elif case=="not-exact-root": helper.verify_checkout(target/"src",helper.PINS["0.2.1"])
    elif case=="dirty-source":
        p=source/"ninelives/healing/tier1.py";p.write_bytes(p.read_bytes()+b"\n# changed\n")
        helper.prepare(source,"0.2.1")
    elif case=="ignored-source":
        (target/".git/info/exclude").write_text("src/ninelives/ignored.py\n")
        (source/"ninelives/ignored.py").write_text("x=1\n")
        helper.prepare(source,"0.2.1")
    elif case=="preimport":
        sys.modules["ninelives"]=types.ModuleType("ninelives")
        helper.prepare(source,"0.2.1")
    elif case=="foreign-owned-module":
        helper.evaluate_requests(source,"0.2.1",[request])
        fake=types.ModuleType("ninelives.fake");fake.__file__=str(target/"fake.py")
        sys.modules["ninelives.fake"]=fake
        helper.verify_loaded_identity(source,"0.2.1")
    elif case=="loader-bypass":
        helper.evaluate_requests(source,"0.2.1",[request])
        fake=types.ModuleType("ninelives.fake");fake.__file__=str(source/"ninelives/__init__.py")
        sys.modules["ninelives.fake"]=fake
        helper.verify_loaded_identity(source,"0.2.1")
    elif case=="changed-after-import":
        helper.evaluate_requests(source,"0.2.1",[request])
        p=source/"ninelives/healing/tier1.py";p.write_bytes(p.read_bytes()+b"\n# race\n")
        helper.verify_loaded_identity(source,"0.2.1")
    else: raise RuntimeError("unknown test")
except AssertionError:
    sys.exit(0)
raise RuntimeError("provenance violation accepted")
`
	for _, name := range []string{"poisoned-cache", "wrong-pin", "not-exact-root", "dirty-source", "ignored-source", "preimport", "foreign-owned-module", "loader-bypass", "changed-after-import"} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(python, "-I", "-c", program, filepath.Join(RunnerRoot(), "internal/contracttest/python_oracle.py"), source, name, filepath.Join(t.TempDir(), "oracle"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("oracle identity case %s: %v: %s", name, err, out)
			}
		})
	}
}
