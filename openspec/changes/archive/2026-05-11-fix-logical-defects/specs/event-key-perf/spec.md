## ADDED Requirements

### Requirement: getCompressedEventKeys 使用 O(n+m) 消息指纹匹配
`getCompressedEventKeys` SHALL 使用预建的消息指纹 map 进行匹配，将时间复杂度从 O(n×m) 优化为 O(n+m)。

#### Scenario: 预建指纹 map 加速匹配
- **WHEN** `getCompressedEventKeys` 被调用
- **THEN** SHALL 首先遍历 oldMsgs 构建 `map[fingerprint]eventKeyInt64`（O(n)）
- **AND** 然后单次遍历 Session Events 在 map 中查找匹配（O(m)）

#### Scenario: 指纹匹配使用内容+角色作为唯一标识
- **WHEN** 构建消息指纹
- **THEN** SHALL 使用 `fmt.Sprintf("%s:%d", msg.Content, msg.Role)` 作为指纹 key
- **AND** 相同内容和角色的消息匹配到相同指纹

#### Scenario: 重复消息仅记录第一个 event_key
- **WHEN** 多条 Session Event 匹配到同一 oldMsg
- **THEN** SHALL 仅记录第一个匹配的 event_key（通过 `seen` set 去重）
- **AND** 跳过后续重复匹配
