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
import yaml
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

    def test_apt_get_runs_hadolint_ignore_for_dl3008(self, raw_dockerfile: str) -> None:
        """`python:3.12-slim` 的包版本由基础镜像锁定，
        不应在 Dockerfile 中硬编码 apt 包版本。改用 # hadolint ignore=DL3008。

        hadolint 指令必须出现于 RUN 指令之前一行（可以跨续行块）。
        """
        lines = raw_dockerfile.splitlines()
        for idx, line in enumerate(lines):
            stripped = line.strip()
            if not stripped.startswith("RUN"):
                continue
            # 扫描 RUN 块（从当前行到下一个 instruction）
            block = [stripped]
            for j in range(idx + 1, len(lines)):
                next_line = lines[j].strip()
                if next_line and next_line.split(" ")[0].split("\t")[0].isupper() and (
                    next_line.startswith(("RUN", "COPY", "ADD", "ENV", "EXPOSE",
                                          "CMD", "ENTRYPOINT", "USER", "WORKDIR",
                                          "LABEL", "VOLUME", "HEALTHCHECK", "ARG",
                                          "ONBUILD", "STOPSIGNAL", "SHELL", "FROM"))
                ):
                    break
                block.append(next_line)
            block_text = "\n".join(block)
            if "apt-get install" not in block_text:
                continue
            # 查找 RUN 前一行的 hadolint ignore 指令（必须以 # 开头）
            preceding = lines[idx - 1] if idx > 0 else ""
            if "hadolint ignore=DL3008" in preceding or "hadolint ignore=DL3008" in block_text:
                continue
            pytest.fail(
                "apt-get install RUN 块前一行必须包含 # hadolint ignore=DL3008 "
                "注释（line " + str(idx + 1) + "）：\n" + "\n".join(block)
            )

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

    def test_excludes_dev_requirements(self) -> None:
        content = self.IGNORE.read_text()
        # requirements-dev.txt 不应被复制到运行镜像
        assert "requirements-dev.txt" in content

    def test_excludes_github_dir(self) -> None:
        content = self.IGNORE.read_text()
        assert ".github/" in content


class TestDockerCompose:
    COMPOSE = PROJECT_ROOT / "docker-compose.yml"

    def test_exists(self) -> None:
        assert self.COMPOSE.exists()

    def test_has_bunkr_service(self) -> None:
        data = yaml.safe_load(self.COMPOSE.read_text())
        services = data.get("services", {})
        assert any("bunkr" in k.lower() for k in services.keys())

    def test_exposes_8765(self) -> None:
        data = yaml.safe_load(self.COMPOSE.read_text())
        for svc in data["services"].values():
            ports = svc.get("ports", [])
            port_strs = [str(p) for p in ports]
            assert any("8765" in p for p in port_strs), f"8765 not in {port_strs}"

    def test_has_volume_for_data(self) -> None:
        data = yaml.safe_load(self.COMPOSE.read_text())
        for svc in data["services"].values():
            vols = svc.get("volumes", [])
            vol_strs = [str(v) for v in vols]
            assert any("/data" in v for v in vol_strs), f"no /data volume: {vol_strs}"

    def test_has_healthcheck(self) -> None:
        data = yaml.safe_load(self.COMPOSE.read_text())
        for svc in data["services"].values():
            assert "healthcheck" in svc, "no healthcheck in service"


class TestGitHubWorkflows:
    """Verify the GitHub Actions workflow files are valid."""

    WORKFLOWS = PROJECT_ROOT / ".github" / "workflows"

    @pytest.mark.parametrize("name", ["docker.yml", "ci.yml", "release.yml", "pylint.yml"])
    def test_workflow_valid_yaml(self, name: str) -> None:
        path = self.WORKFLOWS / name
        assert path.exists(), f"missing: {name}"
        data = yaml.safe_load(path.read_text())
        assert isinstance(data, dict)
        assert "name" in data
        assert "jobs" in data
        assert len(data["jobs"]) > 0

    def test_docker_workflow_has_lint_build_smoke(self) -> None:
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
        data = yaml.safe_load((self.WORKFLOWS / "docker.yml").read_text())
        build = data["jobs"]["build"]
        # multi-arch 通过 buildx 的一次调用完成，应在 build-push-action 的
        # platforms 输入中同时包含两个架构。
        content = (self.WORKFLOWS / "docker.yml").read_text()
        assert "linux/amd64" in content
        assert "linux/arm64" in content
        # 不再需要 matrix（会重复构建）
        matrix = build.get("strategy", {}).get("matrix")
        assert matrix is None, (
            "multi-arch 应使用 buildx 一次构建，不应该用 matrix"
        )

    def test_docker_workflow_triggers_on_tag(self) -> None:
        data = yaml.safe_load((self.WORKFLOWS / "docker.yml").read_text())
        on = data.get(True, data.get("on", {}))  # yaml 1.1 -> True
        tags = on.get("push", {}).get("tags", [])
        assert any("v*" in t for t in tags), f"no v* tag trigger: {tags}"

    def test_docker_workflow_uses_valid_hadolint_inputs(self) -> None:
        """验证 hadolint action 使用合法的输入参数。

        正确输入是 `ignore` (不是过时的 `ignored-rules`)。
        参考 hadolint/hadolint-action@v3.1.0 文档。
        """
        content = (self.WORKFLOWS / "docker.yml").read_text()
        # 不应该使用废弃的 ignored-rules
        assert "ignored-rules" not in content, (
            "hadolint action 不支持 'ignored-rules'，"
            "请改用 'ignore'"
        )
        # 应该使用 ignore（可以换行到下一行）
        assert re.search(r"ignore\s*:", content), (
            "应使用 'ignore:' 作为 hadolint action 的输入"
        )

    def test_docker_workflow_attest_has_valid_subject_digest(self) -> None:
        """验证 SLSA provenance attestation 的 subject-digest 来源。

        常见 bug：attest 步骤中的 steps.X.outputs.digest 引用了一个
        没有 id 的 step，导致 digest 为空，attest 报错：
          'Error: One of subject-path, subject-digest, or subject-checksums
           must be provided'

        修正：build-push-action 必须设 id: build，attest 才能正确读取 digest。
        """
        content = (self.WORKFLOWS / "docker.yml").read_text()

        # 找到 build-push-action 行与其 id
        match = re.search(
            r"uses:\s*docker/build-push-action@[^\n]+\s*\n"
            r"[\s\S]*?",
            content,
        )
        # 验证 build 步骤设了 id: build
        assert re.search(
            r"-\s*name:\s*Build\s*&\s*push\s*\n\s*id:\s*build\s*\n"
            r"\s*uses:\s*docker/build-push-action@v\d+",
            content,
        ), "docker/build-push-action 步骤必须设 id: build 以便 attest 读取 digest"

        # 验证 attest 步骤引用了 steps.build.outputs.digest
        assert "subject-digest: ${{ steps.build.outputs.digest }}" in content, (
            "attest 步骤的 subject-digest 必须从 steps.build.outputs.digest 读取"
        )

    def test_release_workflow_uses_gh_release_action(self) -> None:
        content = (self.WORKFLOWS / "release.yml").read_text()
        assert "action-gh-release" in content or "softprops/action-gh-release" in content
