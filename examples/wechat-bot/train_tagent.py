#!/usr/bin/env python3
"""AReaL PPO 训练脚本 — tagent × AReaL RL 训练入口。

此脚本使用 tagent adapter 作为 rollout workflow，通过 AReaL 的 PPOTrainer
进行强化学习训练。

架构:
    1. AReaL 启动 OpenAI 兼容 proxy（捕获 logprobs + completion_ids）
    2. AReaL 自动将 TagentARealAdapter 包装在 OpenAIProxyWorkflow 中
    3. Adapter 通过 HTTP 向 tagent 发送任务（POST /task，异步提交）
    4. tagent 处理任务，LLM 请求经过 AReaL proxy
    5. AReaL 的 InteractionCache 在 proxy 层记录所有 LLM 交互
    6. Adapter 等待 wait_time 后返回 episode-level reward
    7. AReaL PPO trainer 使用 (logprobs + reward) 进行策略更新

启动方式:
    # 前台运行（调试用）
    python train_tagent.py --config areal_config.yaml scheduler.type=local

    # 分布式训练
    torchrun --nproc_per_node=8 train_tagent.py --config areal_config.yaml

环境变量:
    TAGENT_URL          tagent HTTPAPI 地址 (默认: http://localhost:8089)
    TAGENT_USER_ID      tagent session 用户 ID (默认: rl-user)
    TAGENT_SESSION_ID   tagent session ID (默认: rl-session)
"""

import os
import pathlib
import sys

# 将 tagent 的 areal 目录加入 Python 路径，使 tagent_adapter 可被导入
_TAGENT_AREAL_DIR = str(pathlib.Path(__file__).resolve().parent.parent.parent / "areal")
if _TAGENT_AREAL_DIR not in sys.path:
    sys.path.insert(0, _TAGENT_AREAL_DIR)

from dataclasses import dataclass, field

from areal import PPOTrainer
from areal.api.cli_args import PPOConfig, load_expr_config
from areal.dataset import get_custom_dataset
from areal.utils.hf_utils import load_hf_tokenizer


@dataclass
class TagentRLConfig(PPOConfig):
    """tagent RL 训练配置。

    继承 PPOConfig，添加 tagent adapter 相关参数。
    """

    workflow: str = field(
        default="areal.tagent_adapter.TagentARealAdapter",
        metadata={"help": "tagent adapter workflow 路径。"},
    )
    eval_workflow: str = field(
        default="areal.tagent_adapter.TagentARealAdapter",
        metadata={"help": "评估时使用的 workflow 路径。"},
    )

    # tagent adapter 参数（作为 workflow_kwargs 传递给 TagentARealAdapter.__init__）
    tagent_url: str = field(
        default="http://localhost:8089",
        metadata={"help": "tagent HTTPAPI 地址。"},
    )
    tagent_user_id: str = field(
        default="rl-user",
        metadata={"help": "tagent session 用户 ID。"},
    )
    tagent_session_id: str = field(
        default="rl-session",
        metadata={"help": "tagent session ID。"},
    )
    tagent_wait_time: float = field(
        default=60.0,
        metadata={"help": "等待 tagent 处理任务的时间（秒）。期间 AReaL proxy 捕获所有 LLM 交互。"},
    )


def main(args):
    config, _ = load_expr_config(args, TagentRLConfig)
    tokenizer = load_hf_tokenizer(config.tokenizer_path)

    train_dataset = get_custom_dataset(
        split="train",
        dataset_config=config.train_dataset,
        tokenizer=tokenizer,
    )

    valid_dataset = get_custom_dataset(
        split="test",
        dataset_config=config.valid_dataset,
        tokenizer=tokenizer,
    )

    # workflow_kwargs 传递给 TagentARealAdapter.__init__()
    # 环境变量覆盖配置文件中的值
    workflow_kwargs = dict(
        tagent_url=os.getenv("TAGENT_URL", config.tagent_url),
        user_id=os.getenv("TAGENT_USER_ID", config.tagent_user_id),  # adapter 内部用 user_id
        session_id=os.getenv("TAGENT_SESSION_ID", config.tagent_session_id),
        wait_time=config.tagent_wait_time,
    )

    eval_workflow_kwargs = workflow_kwargs.copy()

    with PPOTrainer(
        config,
        train_dataset=train_dataset,
        valid_dataset=valid_dataset,
    ) as trainer:
        trainer.train(
            workflow=config.workflow,
            eval_workflow=config.eval_workflow,
            workflow_kwargs=workflow_kwargs,
            eval_workflow_kwargs=eval_workflow_kwargs,
        )


if __name__ == "__main__":
    main(sys.argv[1:])
