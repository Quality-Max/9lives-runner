"""Language-neutral execution receipt v1. JSON Schema is published in schemas/."""

from datetime import datetime
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

Text = Annotated[str, Field(max_length=512)]
Identifier = Annotated[str, Field(min_length=1, max_length=160)]
Count = Annotated[int, Field(ge=0)]
Number = Annotated[float, Field(ge=0, allow_inf_nan=False)]
Availability = Literal["available", "missing", "truncated", "expired", "redacted", "unavailable"]
Framework = Literal["playwright", "pytest", "api", "k6", "selenium", "appium", "limrun", "maven", "gradle", "junit", "unknown"]


class Contract(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class Correlation(Contract):
    workflow_id: Identifier | None = None
    job_id: Identifier | None = None
    run_id: Identifier | None = None
    attempt_id: Identifier | None = None
    attempt_number: Count | None = None
    parent_receipt_id: Identifier | None = None
    project_id: Count | None = None
    script_id: Count | None = None
    availability: Availability = "unavailable"


class Revisions(Contract):
    command: Text | None = None
    target: Identifier | None = None
    manifest: Identifier | None = None
    adapter: Identifier | None = None
    template: Identifier | None = None
    availability: Availability = "unavailable"


class TerminalProof(Contract):
    exit_code: int | None = None
    signal: Annotated[int, Field(gt=0)] | None = None
    timed_out: bool = False
    cancelled: bool = False
    started_at: Text | None = None
    completed_at: Text | None = None
    duration_seconds: Number | None = None
    source: Literal["runner", "local_report", "legacy"] = "legacy"
    availability: Availability = "unavailable"

    @model_validator(mode="after")
    def check_timing(self):
        timestamps = []
        for timestamp in (self.started_at, self.completed_at):
            if timestamp is not None:
                try:
                    value = datetime.fromisoformat(timestamp.replace("Z", "+00:00"))
                    if value.utcoffset() is None:
                        raise ValueError("timezone required")
                    timestamps.append(value)
                except ValueError as exc:
                    raise ValueError("Receipt timestamps must be ISO8601 with timezone") from exc
        if self.started_at is not None and self.completed_at is not None and timestamps[1] < timestamps[0]:
            raise ValueError("Receipt completed_at precedes started_at")
        if self.availability == "available":
            if not self.started_at or not self.completed_at or self.source == "legacy":
                raise ValueError("Available terminal proof needs runner timing and provenance")
            if self.exit_code is None and self.signal is None and not (self.timed_out or self.cancelled):
                raise ValueError("Available terminal proof needs a real termination result")
        return self


class Counts(Contract):
    total: Count | None = None
    passed: Count | None = None
    failed: Count | None = None
    skipped: Count | None = None
    errors: Count | None = None
    unit: Literal["tests", "checks", "thresholds", "unknown"] = "unknown"
    availability: Availability = "unavailable"

    @model_validator(mode="after")
    def check_total(self):
        parts = (self.passed, self.failed, self.skipped, self.errors)
        if self.total is not None and sum(value or 0 for value in parts) > self.total:
            raise ValueError("Receipt count components exceed total")
        if self.availability == "available" and (self.total is None or self.passed is None or self.failed is None):
            raise ValueError("Available counts require total, passed and failed")
        return self


class Assertions(Contract):
    total: Count | None = None
    passed: Count | None = None
    failed: Count | None = None
    summary: Text | None = None
    availability: Availability = "unavailable"

    @model_validator(mode="after")
    def check_assertions(self):
        if self.total is not None and (self.passed or 0) + (self.failed or 0) > self.total:
            raise ValueError("Assertion components exceed total")
        if self.availability == "available" and None in (self.total, self.passed, self.failed):
            raise ValueError("Available assertions require total, passed and failed")
        return self


class Failure(Contract):
    category: Literal["product", "test", "environment", "dependency", "infrastructure", "policy", "budget", "unknown"]
    code: Identifier
    confidence: Annotated[float, Field(ge=0, le=1)]
    evidence_ids: Annotated[list[Identifier], Field(max_length=32)]

    @model_validator(mode="after")
    def check_unknown_confidence(self):
        if self.category == "unknown" and self.confidence != 0:
            raise ValueError("Unknown failure classification requires zero confidence")
        return self


class Artifact(Contract):
    id: Identifier
    kind: Literal["stdout", "stderr", "screenshot", "video", "trace", "report", "device_log", "other"]
    availability: Availability
    reference: Annotated[str, Field(pattern=r"^receipt-artifact:[a-f0-9]{64}$")] | None = None
    checksum_sha256: Annotated[str, Field(pattern=r"^[a-f0-9]{64}$")] | None = None
    provenance: Literal["runner", "local_report", "legacy"]
    retention_until: Text | None = None
    access: Literal["opaque", "unverified", "withheld"]
    reason: Text | None = None


class Resources(Contract):
    cpu_seconds: Number | None = None
    peak_memory_bytes: Count | None = None
    cost_amount: Number | None = None
    cost_currency: Annotated[str, Field(pattern="^[A-Z]{3}$")] | None = None
    availability: Availability = "unavailable"


class Decision(Contract):
    confidence: Annotated[float, Field(ge=0, le=1)]
    evidence_ids: Annotated[list[Identifier], Field(max_length=32)]
    reason: Text


class ExecutionReceipt(Contract):
    schema_version: Literal["1.0"]
    receipt_id: Identifier
    framework: Framework
    correlation: Correlation
    revisions: Revisions
    state: Literal["pending", "running", "terminal", "unknown"]
    stage: Literal["queued", "execution", "finished", "unknown"]
    verdict: Literal["passed", "failed", "timed_out", "cancelled", "unknown"]
    terminal: TerminalProof
    counts: Counts
    assertions: Assertions
    failure: Failure | None
    artifacts: Annotated[list[Artifact], Field(max_length=64)]
    resources: Resources
    decision: Decision
    limitations: Annotated[list[Text], Field(max_length=32)]

    @model_validator(mode="after")
    def check_verdict(self):
        if self.verdict != "unknown" and (self.state != "terminal" or self.terminal.availability != "available"):
            raise ValueError("A verdict requires terminal state and available terminal proof")
        if self.verdict == "passed":
            if (
                self.terminal.availability != "available"
                or self.terminal.exit_code != 0
                or self.terminal.signal is not None
                or self.terminal.timed_out
                or self.terminal.cancelled
                or self.counts.availability != "available"
                or not self.counts.total
                or not self.counts.passed
                or self.counts.skipped is None
                or self.counts.errors is None
                or self.counts.passed + self.counts.skipped != self.counts.total
                or self.counts.failed != 0
                or (self.counts.errors or 0) != 0
                or (self.assertions.failed or 0) != 0
                or self.failure is not None
                or not self.decision.evidence_ids
            ):
                raise ValueError("Passed requires successful termination and nonempty passing results")
        if self.verdict == "timed_out" and not self.terminal.timed_out:
            raise ValueError("Timeout verdict requires timeout evidence")
        if self.verdict == "cancelled" and not self.terminal.cancelled:
            raise ValueError("Cancellation verdict requires cancellation evidence")
        if self.verdict == "failed" and not (
            self.terminal.availability == "available"
            and (
                self.terminal.exit_code not in (None, 0)
                or self.terminal.signal
                or (self.counts.failed or 0) > 0
                or (self.counts.errors or 0) > 0
                or (self.assertions.failed or 0) > 0
            )
        ):
            raise ValueError("Failed requires terminal failure evidence")
        if self.verdict in {"failed", "timed_out", "cancelled"} and self.failure is None:
            raise ValueError("Failure verdict requires a stable failure code")
        return self
