from pathlib import Path

WEBSITE = Path(__file__).parent
ROOT = WEBSITE.parent


def test_public_installer_matches_canonical_script():
    assert (WEBSITE / "public" / "install.sh").read_bytes() == (ROOT / "install" / "9l.run.sh").read_bytes()


def test_landing_page_keeps_install_and_ecosystem_links():
    page = (WEBSITE / "app" / "page.tsx").read_text()

    assert "curl -fsSL https://9lives.run/install.sh | sh" in page
    assert "https://github.com/Quality-Max/9lives-runner/blob/main/docs/run-existing-project.md" in page
    assert "https://github.com/Quality-Max/9lives-runner" in page
    brand = (WEBSITE / "app" / "brand.tsx").read_text()
    assert 'href="https://qualitymax.io"' in brand
    for destination in (
        "https://aiinqa.com/ai-in-qa-issue-18/",
        "https://github.com/Quality-Max/qmax-code",
        "https://github.com/Quality-Max/qualitymax-grader",
        "https://github.com/Quality-Max/free-qa-skills",
    ):
        assert destination in page


def test_landing_page_presents_the_page_checks_built_into_the_runner():
    page = (WEBSITE / "app" / "page.tsx").read_text()

    assert "https://qmax.run" not in page
    assert "9l scan https://your-app.dev --format markdown" in page
    assert "claude mcp add 9lives -- 9l mcp" in page
    for tool in ("scan_url", "inspect_page", "generate_playwright_repro", "confirm_finding"):
        assert tool in page


def test_installer_defaults_to_the_release_with_page_checks():
    installer = (ROOT / "install" / "9l.run.sh").read_text()

    # The installer follows the release marked Latest, which the release
    # workflow sets only after verification; no version is pinned here.
    assert 'NINELIVES_VERSION:-v' not in installer
    assert '"$REPOSITORY/releases/latest"' in installer
    assert "9l scan" in installer


def test_site_uses_the_qualitymax_pendant_favicon():
    layout = (WEBSITE / "app" / "layout.tsx").read_text()

    assert "/qualitymax-pendant.svg" in layout


def test_root_uses_accept_header_for_installer_content_negotiation():
    proxy = (WEBSITE / "proxy.ts").read_text()

    assert "text/html" in proxy
    assert "'/install.sh'" in proxy
    assert "NextResponse.redirect" in proxy
    assert "matcher: ['/']" in proxy


def test_public_demo_matches_canonical_offline_demo():
    assert (WEBSITE / "public" / "demo.html").read_bytes() == (ROOT / "docs" / "explainer" / "demo.html").read_bytes()


LLMS = WEBSITE / "public" / "llms.txt"
RAW_PREFIX = "https://raw.githubusercontent.com/Quality-Max/9lives-runner/main/"
# The subcommands `cmd/9l` dispatches; an example naming anything else is wrong.
CLI_COMMANDS = {
    "plan", "run", "status", "result", "cancel", "heal", "heal-native", "mcp", "tier1", "assess",
    "provenance", "prove", "confirm", "scan", "inspect", "doctor", "update", "star", "version", "help",
}


def _llms_sections():
    import re

    parts = re.split(r"^## (.+)$", LLMS.read_text(), flags=re.MULTILINE)
    return {title.strip(): body for title, body in zip(parts[1::2], parts[2::2])}


def test_llms_txt_follows_the_convention_and_states_what_a_result_means():
    content = LLMS.read_text()

    assert content.startswith("# 9lives\n\n> ")
    preamble = content.split("\n## ", 1)[0]
    assert "does not prove that the assertions cover the requirements" in preamble
    assert "Android is experimental" in preamble and "iOS" in preamble and "planned, not shipped" in preamble
    assert "without `Accept: text/html` returns the installer script" in preamble
    assert list(_llms_sections())[:2] == ["Start here", "Examples"]
    assert list(_llms_sections())[-1] == "Optional"


def test_llms_txt_links_only_to_files_that_exist_in_this_repository():
    import re

    content = LLMS.read_text()
    raw = re.findall(r"\]\((https://raw\.githubusercontent\.com/[^)]+)\)", content)

    assert len(raw) >= 30
    for url in raw:
        assert url.startswith(RAW_PREFIX), url
        assert (ROOT / url.removeprefix(RAW_PREFIX)).is_file(), url
    links = re.findall(r"\]\((https://[^)]+)\)", content)
    assert len(links) == len(set(links)), "duplicate link"


def test_llms_txt_examples_use_real_commands_tools_and_no_release_versions():
    import re

    content = LLMS.read_text()
    blocks = re.findall(r"^```(?:sh|bash)\n(.*?)^```", content, flags=re.MULTILINE | re.DOTALL)
    used = set(re.findall(r"(?:^|\s)9l (\w[\w-]*)", "\n".join(blocks)))

    assert used, "no CLI examples"
    assert used <= CLI_COMMANDS, used - CLI_COMMANDS
    assert {"run", "plan", "assess", "prove", "confirm", "heal", "scan", "inspect", "mcp", "doctor"} <= used

    # The homepage names no version so a release needs no site change; so does this file.
    assert not re.search(r"\b\d+\.\d+\.\d+\b", content)
    assert "@9l/playwright@" not in content


def test_llms_txt_advertises_the_complete_documented_mcp_tool_set():
    import re

    mcp_doc = (ROOT / "docs" / "mcp.md").read_text()
    tools_section = mcp_doc.split("\n## Tools\n", 1)[1].split("\n## ", 1)[0]
    documented = set(re.findall(r"^\|\s*`([a-z][a-z0-9_]*)`\s*\|", tools_section, flags=re.MULTILINE))
    advertised_line = re.search(r"^The server exposes these tools: (.+?)\.", LLMS.read_text(), flags=re.MULTILINE)
    assert advertised_line, "missing MCP tool list"
    advertised = re.findall(r"`([a-z][a-z0-9_]*)`", advertised_line[1])

    assert documented, "no documented MCP tools"
    assert len(advertised) == len(set(advertised)), "duplicate advertised MCP tool"
    assert set(advertised) == documented, {"missing": documented - set(advertised), "extra": set(advertised) - documented}


def test_llms_txt_copies_the_release_lab_specs_verbatim():
    content = LLMS.read_text()
    weak = (ROOT / "demo" / "release-lab" / "tests" / "checkout.spec.ts").read_text().strip()
    strong = (ROOT / "demo" / "release-lab" / "tests" / "checkout-complete.spec.ts").read_text()
    recommendation = strong.splitlines()[-3:-1]

    assert weak in content
    assert "\n".join(recommendation) in content
    lines = {line for line in content.splitlines() if "(experimental)" in line}
    for heading in ("Confirm that a fix", "Heal a broken locator", "Android with Appium"):
        assert any(heading in line for line in lines), heading
