# Bad Case:hex 断裂 key 静默空结果(资产化自 tests/README 教训)

**现象**:畸形 key 格式传入 recall 时曾静默返回空结果——故障被掩盖多日。
**资产化**:TestSuite_BadCase_HexBrokenKeyRejected 断言 ParseEventKey 显式拒绝。
**教训**:历史教训必须转为回归用例,否则会复发。
