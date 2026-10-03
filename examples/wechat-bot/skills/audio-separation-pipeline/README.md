# audio-separation-pipeline — 音乐伴奏/人声分离交付 SOP

三首歌（植物/Take You To Hell/bin）验证过的完整管线。模型：MSST MelBand-RoFormer **Karaoke**（用户已禁用 UMX 系）。

## 标准流程（音源到手 → 双件交付）

1. **音源获取**：网易 `http://music.163.com/song/media/outer/url?id=<id>`（可试播曲目直链 128k）；QQ MV/YouTube/VIP 曲目六通道全堵（详见会话终账），直接请用户发文件/cookie，不重复探测
2. **身份门**：元信息 API 三重对齐（歌名/歌手/时长）+ `ffprobe` 实测时长/bitrate，防错包（《植物》曾错发第一首）
3. **转 WAV**：`ffmpeg -i src.mp3 -ar 44100 -ac 2 input.wav`（karaoke_sep.py 读死 `/tmp/umx/input.wav`）
4. **点火（三选一；10-03 事故增量：裸 nohup 点火→实体完成零事件、监视器盯漏写标记空转、5min 静默清死，三连失败靠用户催问才暴露。点火前逐字执行本步）**：
   - a. 托管直跑 + `ttl=14400`（首选；若框架 reconcile 误杀静默推理则转 b）
   - b. tmux 实体 + **必配** watch 监视器（resident + `tail -f log` + watch `DONE|EXIT_CODE`）
   - c. tmux 实体 + 单次完成探针（ETA 兜底）
   - 命令尾必须 `echo EXIT_CODE=$? >> log`
   - **禁止 nohup/& 裸点火**（框架判"后台化走私"：无结算通知/无 TTL/失败静默——10-03 实测）；监视器自身防静默清死：quiet_timeout 必须 > 完成等待上限，零输出轮询环会被提前清死（10-03：300s 阈值 5min 即亡，早于 DONE）
5. **完成判定**：日志尾 `DONE` + `EXIT_CODE=0` + CHUNK 推进（0 CHUNK + RSS 停在 ~400MB = 内存窒息，清场后重点火）
6. **转码**：产物 `karaoke_vocal.wav` / `karaoke_accomp.wav`（单数名，别猜）→ ffmpeg 320k → 交付目录 `bin_*/plant_*` 风格命名
7. **交付**：时长硬门（与源 ±0.5s）+ 成色预告（电子编曲残影风险如实说）

## 内存预算（3.7G 机器硬约束）
- bot ~1.6G + qqrobot ~575M 常驻，分离需 ~1.5G 峰值：点火前 `free -m` 看 available ≥700M，不足先清场
- 窒息识别：RSS 卡 400MB + CHUNK 0 + 日志静默 >10min；处置：杀实体 → 清场 → nice 15 重点火
- 判进程看 **ps 全表**：pgrep|head -1 可能拿到 bash 壳（256KB），python 实体在另一行

## 通知纪律（用户等两小时的教训）
点火的 settle 是启动器的（秒回），**实体完成不产生任何事件**（tmux 在托管体系外）。收尾话术禁止"等结算自动唤醒"——按 4 选监视形态并明确说明唤醒机制。

## 裁剪规格（用户偏好，人声听感优先）
开场人声前留 3s（淡入），人声结束后 3s 收尾（淡出）；伴奏不裁。
