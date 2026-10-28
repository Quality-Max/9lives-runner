package main

import (
	"os/exec"
	"testing"

	"github.com/Quality-Max/9lives-runner/internal/contracttest"
)

func TestAndroidRuntimeSchemasRequireTrustedTestBinding(t *testing.T) {
	python := contracttest.Python(t, "jsonschema")
	program := `import json,sys,jsonschema
from pathlib import Path
runtime={"testId":"a"*64,"appPackage":"dev.ninelives.fixture","apkSHA256":"b"*64,
         "api":"35","release":"15","abi":"arm64-v8a","emulator":"37.1.11",
         "appium":"3.8.0","uiautomator2":"8.7.0","worker":"c"*64}
for name in ("receipt","run-result","run-status"):
    schema=json.loads((Path(sys.argv[1])/"docs/contracts"/(name+".schema.json")).read_text())
    validator=jsonschema.Draft202012Validator(schema["$defs"]["AndroidRuntime"])
    assert validator.is_valid(runtime), name+": bound runtime rejected"
    physical=dict(runtime)
    del physical["emulator"]
    physical.update(device="physical",deviceHash="d"*64)
    assert validator.is_valid(physical), name+": physical runtime rejected"
    unbound=dict(physical)
    del unbound["testId"]
    assert not validator.is_valid(unbound), name+": unbound runtime accepted"
`
	output, err := exec.Command(python, "-c", program, contracttest.RunnerRoot()).CombinedOutput()
	if err != nil {
		t.Fatalf("Android runtime schema attribution failed: %v: %s", err, output)
	}
}
