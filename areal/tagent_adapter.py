#!/usr/bin/env python3
"""AReaL agent workflow adapter for tagent.

This adapter bridges AReaL's Python RL training framework with tagent's Go runtime.
It implements AReaL's agent workflow interface (`async def run(data, **extra_kwargs) -> float | dict[str, float]`)
by communicating with tagent via HTTP.

Architecture:
    1. AReaL starts an OpenAI-compatible proxy server (captures logprobs + completion_ids)
    2. tagent is configured to use AReaL's proxy as its LLM backend (openai.WithBaseURL)
    3. This adapter sends tasks to tagent via HTTP API (POST /task)
    4. tagent processes tasks through its persistent event loop
    5. AReaL's proxy records all LLM interactions in its InteractionCache
    6. This adapter returns an episode-level reward to AReaL for PPO training
    7. AReaL handles reward-to-completion mapping internally via its cache

Note: tagent no longer stores or exposes trajectories. All RL data recording
is handled by AReaL's InteractionCache at the proxy level.

Usage in AReaL config:
    rollout:
      openai:
        mode: inline
        workflow: areal.tagent_adapter.TagentARealAdapter

Environment variables:
    TAGENT_URL    - Base URL of tagent's HTTP API (default: http://localhost:8089)
    TAGENT_USER_ID   - User ID for tagent session (default: rl-user)
    TAGENT_SESSION_ID - Session ID for tagent session (default: rl-session)
"""

from __future__ import annotations

import asyncio
import os
from typing import Any, Callable

try:
    import aiohttp
except ImportError:
    aiohttp = None  # type: ignore

try:
    import httpx
except ImportError:
    httpx = None  # type: ignore


class TagentARealAdapter:
    """AReaL agent workflow adapter for tagent.

    Implements AReaL's agent workflow interface. Each call to `run()` sends
    a task to tagent's HTTP API, waits for processing, and returns the reward.

    Parameters
    ----------
    tagent_url : str
        Base URL of tagent's HTTP API (e.g., "http://localhost:8089").
    reward_fn : Callable[[dict], float] | None
        Optional Python-side reward function. If provided, it receives the
        task data dict and returns a float. If None, returns a default reward.
    wait_time : float
        Time in seconds to wait for tagent to process the task (default: 60).
        During this wait, AReaL's proxy captures all LLM interactions.
        Adjust based on task complexity and model speed.
    user_id : str
        User ID for tagent session (default: rl-user).
    session_id : str
        Session ID for tagent session (default: rl-session).
    """

    def __init__(
        self,
        tagent_url: str | None = None,
        reward_fn: Callable[[dict[str, Any]], float] | None = None,
        wait_time: float = 60.0,
        user_id: str | None = None,
        session_id: str | None = None,
        **kwargs: Any,
    ):
        self.tagent_url = (tagent_url or os.getenv("TAGENT_URL", "http://localhost:8089")).rstrip("/")
        self.reward_fn = reward_fn
        self.wait_time = wait_time
        self._user_id = user_id or os.getenv("TAGENT_USER_ID", "rl-user")
        self._session_id = session_id or os.getenv("TAGENT_SESSION_ID", "rl-session")

    async def run(self, data: dict[str, Any], **extra_kwargs: Any) -> float:
        """Run a single rollout episode via tagent.

        Parameters
        ----------
        data : dict
            Input data from AReaL dataset. Expected keys:
            - "messages": list of {"role": str, "content": str} — task messages
        **extra_kwargs : Any
            AReaL-injected arguments:
            - base_url: AReaL proxy URL (dynamically allocated port).
              Passed to tagent via POST /task llm_base_url field so that
              tagent's LLM requests go through the proxy (capturing logprobs).
            - api_key: session API key for the proxy (not used by tagent —
              tagent uses its own API key from config).
            - http_client: async HTTP client (not used — adapter creates its own).

        Returns
        -------
        float
            Episode-level reward. AReaL maps this to all completions
            in the episode via its InteractionCache.
        """
        messages = data.get("messages", [])
        if not messages:
            return 0.0

        # Extract AReaL proxy URL — tagent will redirect LLM requests to it
        llm_base_url = extra_kwargs.get("base_url")

        # Submit task to tagent (async — POST /task returns 202 immediately)
        await self._post_task(messages, llm_base_url=llm_base_url)

        # Wait for tagent to process the task.
        # tagent's POST /task is asynchronous — it injects messages into the
        # persistent event loop's mailbox. We wait a fixed time for processing.
        # During this time, all LLM requests go through AReaL's proxy,
        # which records logprobs + completion_ids in its InteractionCache.
        await asyncio.sleep(self.wait_time)

        # Compute reward
        return self._compute_reward(data)

    async def _post_task(
        self, messages: list[dict[str, str]], llm_base_url: str | None = None
    ) -> None:
        """Submit a task to tagent's POST /task endpoint.

        Parameters
        ----------
        messages : list[dict[str, str]]
            Task messages to inject into tagent's persistent event loop.
        llm_base_url : str | None
            AReaL proxy URL. If provided, tagent swaps its LLM model to
            use this URL before processing the task. This ensures all
            LLM requests are captured by AReaL's proxy.
        """
        payload: dict[str, Any] = {
            "messages": messages,
            "user_id": self._user_id,
            "session_id": self._session_id,
        }
        if llm_base_url:
            payload["llm_base_url"] = llm_base_url

        async with self._get_client() as client:
            if aiohttp and isinstance(client, aiohttp.ClientSession):
                async with client.post(
                    f"{self.tagent_url}/task", json=payload
                ) as resp:
                    if resp.status not in (200, 202):
                        text = await resp.text()
                        raise RuntimeError(f"POST /task failed: {resp.status} {text}")
            elif httpx and isinstance(client, httpx.AsyncClient):
                resp = await client.post(f"{self.tagent_url}/task", json=payload)
                if resp.status_code not in (200, 202):
                    raise RuntimeError(f"POST /task failed: {resp.status_code} {resp.text}")

    def _compute_reward(self, data: dict[str, Any]) -> float:
        """Compute episode-level reward.

        Priority:
        1. If Python-side reward_fn is set, use it
        2. Default: 0.0 (AReaL can override via its own reward mechanism)
        """
        if self.reward_fn is not None:
            return self.reward_fn(data)

        return 0.0

    def _get_client(self):
        """Get an async HTTP client. Prefers aiohttp, falls back to httpx."""
        if aiohttp:
            return _AiohttpSession()
        elif httpx:
            return _HttpxSession()
        else:
            raise RuntimeError(
                "Neither aiohttp nor httpx is installed. "
                "Install one: pip install aiohttp httpx"
            )


class _AiohttpSession:
    """Context manager wrapper for aiohttp.ClientSession."""

    def __enter__(self):
        self._session = aiohttp.ClientSession()
        return self._session

    def __exit__(self, *args):
        loop = asyncio.get_event_loop()
        loop.run_until_complete(self._session.close())


class _HttpxSession:
    """Context manager wrapper for httpx.AsyncClient."""

    def __enter__(self):
        self._client = httpx.AsyncClient()
        return self._client

    def __exit__(self, *args):
        loop = asyncio.get_event_loop()
        loop.run_until_complete(self._client.aclose())
