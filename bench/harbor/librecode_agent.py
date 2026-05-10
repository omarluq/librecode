"""Harbor installed-agent adapter for librecode.

Usage:
    harbor run -p path/to/task -m openai/gpt-5 \
      --agent-import-path bench.harbor.librecode_agent:LibrecodeAgent

The adapter installs librecode with LIBRECODE_INSTALL_COMMAND when provided,
then runs librecode non-interactively inside Harbor's agent environment.
"""

from __future__ import annotations

import os
import shlex
from pathlib import Path


_REPO_ROOT = Path(__file__).resolve().parents[2]
_DEFAULT_BINARY = _REPO_ROOT / "bin" / "librecode"
_CONTAINER_BINARY = "/usr/local/bin/librecode"

from harbor.agents.installed.base import BaseInstalledAgent, with_prompt_template
from harbor.environments.base import BaseEnvironment
from harbor.models.agent.context import AgentContext


class LibrecodeAgent(BaseInstalledAgent):
    """Run librecode as a Harbor installed agent."""

    @staticmethod
    def name() -> str:
        return "librecode"

    def get_version_command(self) -> str | None:
        return "librecode --version"

    async def install(self, environment: BaseEnvironment) -> None:
        install_command = os.environ.get("LIBRECODE_INSTALL_COMMAND")
        if install_command:
            await self.exec_as_agent(environment, command=install_command)
            return

        binary_path = Path(os.environ.get("LIBRECODE_BINARY", _DEFAULT_BINARY))
        if not binary_path.exists():
            msg = (
                "librecode binary not found. Build it first with "
                "`mise exec -- task build`, or set LIBRECODE_BINARY=/path/to/librecode, "
                "or set LIBRECODE_INSTALL_COMMAND to a command that installs librecode "
                "inside the Harbor environment."
            )
            raise RuntimeError(msg)

        await self.exec_as_root(environment, command="mkdir -p /usr/local/bin")
        await environment.upload_file(binary_path, _CONTAINER_BINARY)
        await self.exec_as_root(
            environment,
            command=f"chmod 755 {_CONTAINER_BINARY} && {_CONTAINER_BINARY} --version",
        )

    @with_prompt_template
    async def run(
        self,
        instruction: str,
        environment: BaseEnvironment,
        context: AgentContext,
    ) -> None:
        prompt = shlex.quote(instruction)
        await self.exec_as_agent(
            environment,
            command=(
                "mkdir -p /logs/agent && "
                f"librecode --no-extensions prompt {prompt} "
                "2>&1 | tee /logs/agent/librecode.log"
            ),
        )

    def populate_context_post_run(self, context: AgentContext) -> None:
        log_path = Path("/logs/agent/librecode.log")
        if not log_path.exists():
            return

        content = log_path.read_text(encoding="utf-8", errors="replace")
        if hasattr(context, "output"):
            context.output = content
        elif hasattr(context, "result"):
            context.result = content
