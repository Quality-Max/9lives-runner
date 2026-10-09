"""Independent draft 2020-12 validation; never print document/error payloads."""
import json
import sys
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker

root = Path(__file__).resolve().parent.parent / "docs" / "contracts"
validators = {}
try:
    # The producer bounds its buffer; bound the validator's input too.
    raw = sys.stdin.buffer.read((4 << 20) + 1)
    if len(raw) > 4 << 20:
        raise ValueError("input limit")
    samples = json.loads(raw)
    for sample in samples:
        name = sample["schema"]
        if name not in validators:
            schema = json.loads((root / (name + ".schema.json")).read_text())
            Draft202012Validator.check_schema(schema)
            validators[name] = Draft202012Validator(schema, format_checker=FormatChecker())
        if validators[name].is_valid(sample["document"]) != sample["valid"]:
            raise ValueError("schema mismatch")
except Exception:
    print("CLI schema validation failed", file=sys.stderr)
    sys.exit(1)
