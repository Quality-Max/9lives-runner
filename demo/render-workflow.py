"""Render the QualityMax Demo Bank result replay and feature storyboard as a GIF and poster.

Requires Pillow, Playwright, and its Chromium browser. No runner is executed.
"""

from io import BytesIO
from pathlib import Path

from PIL import Image
from playwright.sync_api import sync_playwright


def main():
    root = Path(__file__).resolve().parent
    frames = []
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch()
        page = browser.new_page(viewport={"width": 1280, "height": 720}, device_scale_factor=1)
        page.goto((root / "workflow-illustration.html").as_uri())
        page.locator(".bank-capture").evaluate("el => el.decode()")
        for index in range(10):
            page.evaluate("index => window.showFrame(index)", index)
            with Image.open(BytesIO(page.screenshot())) as screenshot:
                frames.append(screenshot.convert("RGB"))
        browser.close()
    frames[0].save(root / "workflow-illustration.png", optimize=True)
    frames[0].save(
        root / "workflow-illustration.gif",
        save_all=True,
        append_images=frames[1:],
        duration=[3500, 4500, 4000, 4000, 4000, 4000, 4000, 4000, 4000, 4000],
        loop=0,
        optimize=True,
        disposal=2,
    )
    print("Rendered ten replay/tour frames at 1280×720; original results are documented separately.")


if __name__ == "__main__":
    main()
