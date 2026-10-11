"""Render an edited Demo Bank replay from checked results and browser captures.

Run `npm run demo:bank` first. Requires Pillow and Python Playwright with Chromium.
This renderer never runs a test or calls a model; it refuses unverified evidence.
"""

import hashlib
import json
from io import BytesIO
from pathlib import Path

from PIL import Image
from playwright.sync_api import sync_playwright


def main():
    root = Path(__file__).resolve().parent
    evidence = json.loads((root / "demo-bank-evidence.json").read_text())
    draft_hash = hashlib.sha256((root / "bank/tests/bank.spec.ts").read_bytes()).hexdigest()
    if evidence["generation"]["sourceSHA256"] != draft_hash:
        raise RuntimeError("Draft changed: refresh evidence with npm run demo:bank")
    healing = evidence["healing"]
    if not (
        evidence["original"]["outcome"] == "failed"
        and evidence["original"]["failed"] == 1
        and "fixed-wait" in evidence["assessment"]["findingCodes"]
        and evidence["assessment"]["fixedWaitRemovedBeforeExecution"]
        and healing["state"] == "verified"
        and healing["candidateVerified"]
        and healing["executedTests"] == 1
        and healing["onlyLocatorEdit"]
        and healing["assertionsPreservedByteForByte"]
        and healing["providerCalls"] == 0
        and not healing["applied"]
    ):
        raise RuntimeError("Replay requires a failed original and verified offline locator-only repair")
    preview = root.parent / ".context" / "demo-gif-preview"
    preview.mkdir(parents=True, exist_ok=True)
    frames = []
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch()
        page = browser.new_page(viewport={"width": 1280, "height": 880}, device_scale_factor=1)
        page.goto((root / "workflow-illustration.html").as_uri())
        durations = page.evaluate("window.frameDurations")
        for index in range(page.evaluate("window.frameCount")):
            page.evaluate("index => window.showFrame(index)", index)
            # Fail if a frame's text clips, including at the bottom of its panel.
            overflow = page.evaluate("""() => [...document.querySelectorAll('h1, #intro, #stage, #note, pre, .explanation, .panelbar')]
              .filter(el => el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1)
              .map(el => el.id || el.className || el.tagName)""")
            if overflow:
                raise RuntimeError(f"Frame {index + 1} clips: {overflow}")
            with Image.open(BytesIO(page.screenshot())) as screenshot:
                frame = screenshot.convert("RGB")
            frame.save(preview / f"frame-{index + 1:02d}.png")
            frames.append(frame)
        browser.close()
    frames[0].save(root / "demo-bank.png", optimize=True)
    frames[0].save(
        root / "demo-bank.gif",
        save_all=True,
        append_images=frames[1:],
        duration=durations,
        loop=0,
        optimize=True,
        disposal=2,
    )
    print(f"Rendered {len(frames)} frames at 1280×880, {sum(durations) / 1000:g}s total; 32px code text.")
    print(f"GIF size: {(root / 'demo-bank.gif').stat().st_size / 1024 / 1024:.2f} MiB")


if __name__ == "__main__":
    main()
