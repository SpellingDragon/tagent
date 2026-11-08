# hot-update-and-purity-closure 编排文档

## 一、结论

抵达 DoD 的唯一路径：W0 先立两锚红+exec 探针证明事件指引整代装配可行并冻结共享契约 → W1 并发清交付/读面/门禁文档 → W2 hygiene → W3 全清单消费者每代装配 → W4 混沌+收尾；当前最大风险＝storage 根变更的在途安全（5.7），探针失败则具名保留 restart 档并回写 proposal，不阻主线。

## 二、编排四问

### 2.1 次序（就绪孙任务表；派发原子=孙任务）

| 孙任务 | 域 | 前置 | 状态 | 触碰集 | 派发单 |
|---|---|---|---|---|---|
| 1.1/2.1/5.1 锚红 | 01/02/05 | 立案 | ready | 各自新增测试文件 | W0-1 |
| 5.2 探针 | 05 | 5.1 | ready | tool/exec+assembly/run.go(探针隔离) | W0-2 |
| 5.3 契约冻结 | 05 | 5.2 | ready | design D6（不产代码） | W0-3 |
| 1.1-1.8 | 01 | restart-recovery 归档（host_delivery 活跃编辑重叠） | blocked | host_delivery/host_tools/loop | W1-1 |
| 2.2-2.5 | 02 | 2.1 | ready | read/get/localfile | W1-2 |
| 4.1-4.4 | 04 | 无 | ready | fuse/request_assembly/window_bounds/gates/docs | W1-3 |
| 3.1-3.5 | 03 | restart-recovery 归档 | blocked | commit/host/host_capture/run/store_searcher/recovery | W2-1 |
| 5.15/5.16 注册表+归位 | 05 | 5.3+03 入 main | blocked | assembly 注册表缝(编排者)+各消费者包 | W3-0 |
| 5.4-5.12 | 05 | 5.15/5.16 入 main | blocked | 各消费者包+登记调用（run.go 缝归编排者） | W3-* |
| 5.13/5.14/F | 05/一级 | W3 全绿 | blocked | integration/chaos/wiki | W4-1 |

并发准入=触碰集不相交（实测 `go list -deps`）；W1 三单触碰集互斥已核（01 delivery 簇 / 02 read 簇 / 04 gates 簇）。本轮做不了：03/05（启动门未过）。

### 2.2 授权

每单白名单精确到上表触碰集文件；禁 git commit/checkout、禁动他域文件；过程件落 /tmp/<change>-<单号>/。每单含"自清白名单内 finding"条款（门命令 `scripts/gates/lint.sh` 随令下发，自面清零判据随令）；共享面/全局 finding 归编排者；编排者终跑门是唯一终态判据（agent 各自绿≠整体绿）。共享面 run.go 装配缝、host_reload.go、api/event、api/config 归编排者单写（05 消费者经 D6 冻结签名只改本包+登记调用）。偏离分级：白名单内替身/mock 预授权；扩 409 保留档、改 D6 签名、storage 不可热换裁决=上报。

### 2.3 凭据

五查按派发单核；每条孙任务 verify 三锚 exit 0（正锚 -run 逐字符+非空转用例数>0；负锚具名硬门；消费者锚=Host.Reload/Run.Invoke/recall_get/boot 公共入口+负例）。必亲验数字：409 清单 grep 计数=0、混沌重复次数≥10、race 报告空、锚 A/B 红绿退出码、F1 diff 空。

### 2.4 释放（锁必携等待表）

| 锁/占用 | 释放条件 | 等待表（释放后当场补派） |
|---|---|---|
| restart-recovery 归档门 | 该 change archive | 补派 W2-1(03)；03 入 main 后补派 W3-* |
| run.go 单写锁 | 编排者落 D6 缝 | 补派 05 各消费者单 |
| 探针设备/chaos 槽 | 5.2 / 5.13 结束 | 补派 W3 消费者 / W4 收尾 |

## 三、明细

派发单-孙任务矩阵见 2.1；合单判据：同域同文件簇合单（01 的 host_delivery 簇含 1.2/1.3/1.4/1.7）；单均<2 不拆。预算：W1 三单并发、W3 消费者按包并发上限=不相交包数；混沌 5.13 为长跑单后台跑不占并发槽（R32）。档位与真实路径欠账：storage 远端 daemon 真实路径若缺凭据按 R24 三态（无门 Skip/有门缺凭据 Fatal/门开非空转）。

## 四、台账

| 轮 | 五查结论 | 集成验证 | 跨域裁决 | 勾选凭据 | 下轮情报 | 锁释放 |
|---|---|---|---|---|---|---|
| 规划 | 自审见下 | — | — | — | 基线 dev 036c3500；两锚基线红证据=host_reload.go:790(409)/host_delivery.go:547+1481(trim) | — |

（执行期逐轮回填；本档为派发唯一依据，派发前重读二节。）
| 终审轮 | R35 两轮：首轮三阻断(doc-budget/meditation 水位代际/文档脱节)由 R4 修；复审余 R35-1(拒绝候选水位滞留)已修+TestRefusedMeditationOverNoMeditationBootClearsWatermark 钉；concept 基线重指可达 main(3b727199)、removed_min 校准 5；CI 待绿后 squash | 见 /tmp/hupc-R4、C5、R35 报告 | 登记例外 anchors/approvals/RL 为受治理债(review_by 2027-05-05) | 6.3/6.4 勾 | CI watch bd0rgbyxs | — |
