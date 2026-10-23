# lean-sweep：删旧＋wechat 重写

## Why
审计实证：148K 行中 ~16,700 行零消费者/过程产物。每轮"归核"实际复制到核而非删除旧面。用户裁定：诸事皆允，删。

## What
A 类硬删（7 面零消费者＋旧接口＋二进制）；B 类裁剪（meditation/rl 孤面）；wechat-bot 非 main 全删重写为 wechat.go+main.go 两文件。

## Capabilities
lean-sweep：零消费者面删除＋唯一装配面保留＋行为等价。
