"""Dockerfile sanity tests (no Docker required).

Verify:
    - Dockerfile parses correctly
    - Required base image is used
    - All COPY sources exist
    - Healthcheck is set
    - Non-root user is configured
    - Required ports are exposed
"""

from __future__ import annotations

import re
from pathlib import Path

import pytest
from dockerfile_parse import DockerfileParser


PROJECT_ROOT = Path(__file__).resolve().parent.parent
DOCKERFILE = PROJECT_ROOT / "Dockerfile"


@pytest.fixture(scope="module")
def parser() -> DockerfileParser:
    return DockerfileParser(path=str(DOCKERFILE))


@pytest.fixture(scope="module")
def raw_dockerfile() -> str:
    return DOCKERFILE.read_text()


class TestDockerfileStructure:
    def test_exists(self) -> None:
        assert DOCKERFILE.exists()

    def test_parses(self, parser: DockerfileParser) -> None:
        assert len(parser.structure) > 0

    def test_uses_python_base(self, parser: DockerfileParser) -> None:
        assert "python" in parser.baseimage.lower()


class TestDockerfileInstructions:
    def test_has_healthcheck(self, parser: DockerfileParser) -> None:
        hc = [i for i in parser.structure if i["instruction"] == "HEALTHCHECK"]
        assert len(hc) == 1
        assert "curl" in hc[0]["value"] or "wget" in hc[0]["value"]

    def test_exposes_8765(self, raw_dockerfile: str) -> None:
        # Find EXPOSE lines
        for line in raw_dockerfile.splitlines():
            s = line.strip()
            if s.startswith("EXPOSE "):
                ports = s[len("EXPOSE "):].split()
                assert "8765" in ports, f"8765 not in EXPOSE: {ports}"

    def test_uses_non_root_user(self, parser: DockerfileParser) -> None:
        users = [i for i in parser.structure if i["instruction"] == "USER"]
        assert len(users) >= 1
        last_user = users[-1]["value"].strip()
        assert last_user not in ("", "root", "0")

    def test_has_entrypoint(self, parser: DockerfileParser) -> None:
        ep = [i for i in parser.structure if i["instruction"] == "ENTRYPOINT"]
        assert len(ep) == 1
        assert "tini" in ep[0]["value"] or "python" in ep[0]["value"]

    def test_cmd_runs_web_main(self, parser: DockerfileParser) -> None:
        cmds = [i for i in parser.structure if i["instruction"] == "CMD"]
        assert len(cmds) == 1
        assert "web_main.py" in cmds[0]["value"]


class TestDockerfileCopySources:
    """Verify every COPY source path exists in the project tree."""

    COPY_RE = re.compile(r"^\s*COPY\s+(?P<args>.+)$", re.MULTILINE)

    def test_copy_sources_exist(self, raw_dockerfile: str) -> None:
        # Find COPY lines (ignoring --from=)
        for line in raw_dockerfile.splitlines():
            s = line.strip()
            if not s.startswith("COPY "):
                continue
            args = s[len("COPY "):]
            # Skip --from= builder references
            if "--from=" in args:
                continue
            # Skip flags like --chown=
            tokens = [t for t in args.split() if not t.startswith("--")]
            if len(tokens) != 2:
                # COPY src/ dst/ 形式
                continue
            src = tokens[0]
            # Accept variables / wildcards
            if "$" in src or "*" in src or "{" in src:
                continue
            target = PROJECT_ROOT / src
            assert target.exists(), f"COPY source does not exist: {src}"


class TestDockerignore:
    IGNORE = PROJECT_ROOT / ".dockerignore"

    def test_exists(self) -> None:
        assert self.IGNORE.exists()

    def test_excludes_git(self) -> None:
        content = self.IGNORE.read_text()
        assert ".git/" in content

    def test_excludes_pycache(self) -> None:
        content = self.IGNORE.read_text()
        assert "__pycache__/" in content

    def test_excludes_venv(self) -> None:
        content = self.IGNORE.read_text()
        assert ".venv" in content or "venv" in content

    def test_excludes_dockerfile_itself(self) -> None:
        content = self.IGNORE.read_text()
        assert "Dockerfile" in content


class TestDockerCompose:
    COMPOSE = PROJECT_ROOT / "docker-compose.yml"

    def test_exists(self) -> None:
        assert self.COMPOSE.exists()

    def test_has_bunkr_service(self) -> None:
        import yaml
        data = yaml.safe_load(self.COMPOSE.read_text())
        services = data.get("services", {})
        assert any("bunkr" in k.lower() for k in services.keys())

    def test_exposes_8765(self) -> None:
        import yaml
        data = yaml.safe_load(self.COMPOSE.read_text())
        for svc in data["services"].values():
            ports = svc.get("ports", [])
            port_strs = [str(p) for p in ports]
            assert any("8765" in p for p in port_strs), f"8765 not in {port_strs}"

    def test_has_volume_for_data(self) -> None:
        import yaml
        data = yaml.safe_load(self.COMPOSE.read_text())
        for svc in data["services"].values():
            vols = svc.get("volumes", [])
            vol_strs = [str(v) for v in vols]
            assert any("/data" in v for v in vol_strs), f"no /data volume: {vol_strs}"

    def test_has_healthcheck(self) -> None:
        import yaml
        data = yaml.safe_load(self.COMPOSE.read_text())
        for svc in data["services"].values():
            assert "healthcheck" in svc, "no healthcheck in service"


class TestGitHubWorkflows:
    """Verify the GitHub Actions workflow files are valid."""

    WORKFLOWS = PROJECT_ROOT / ".github" / "workflows"

    @pytest.mark.parametrize("name", ["docker.yml", "ci.yml", "release.yml", "pylint.yml"])
    def test_workflow_valid_yaml(self, name: str) -> None:
        import yaml
        path = self.WORKFLOWS / name
        assert path.exists(), f"missing: {name}"
        data = yaml.safe_load(path.read_text())
        assert isinstance(data, dict)
        assert "name" in data
        assert "jobs" in data
        assert len(data["jobs"]) > 0

    def test_docker_workflow_has_lint_build_smoke(self) -> None:
        import yaml
        data = yaml.safe_load((self.WORKFLOWS / "docker.yml").read_text())
        jobs = data["jobs"]
        assert "lint" in jobs
        assert "build" in jobs
        assert "smoke" in jobs

    def test_docker_workflow_uses_ghcr(self) -> None:
        content = (self.WORKFLOWS / "docker.yml").read_text()
        assert "ghcr.io" in content
        assert "packages: write" in content

    def test_docker_workflow_multi_arch(self) -> None:
        import yaml
        data = yaml.safe_load((self.WORKFLOWS / "docker.yml").read_text())
        # 检查 matrix 中包含 amd64 与 arm64
        build = data["jobs"]["build"]
        matrix = build.get("strategy", {}).get("matrix", {}).get("include", [])
        platforms = {m.get("platform") for m in matrix if "platform" in m}
        assert "linux/amd64" in platforms
        assert "linux/arm64" in platforms

    def test_docker_workflow_triggers_on_tag(self) -> None:
        import yaml
        data = yaml.safe_load((self.WORKFLOWS / "docker.yml").read_text())
        on = data.get(True, data.get("on", {}))  # yaml 1.1 -> True
        tags = on.get("push", {}).get("tags", [])
        assert any("v*" in t for t in tags), f"no v* tag trigger: {tags}"

    def test_release_workflow_uses_gh_release_action(self) -> None:
        content = (self.WORKFLOWS / "release.yml").read_text()
        assert "action-gh-release" in content or "softprops/action-gh-release" in content
