# orchestration：host-layer

- H1a-d 并发（文件零相交：各一个新文件）→H2（消费 Host API）。
- wechat.go 终态 ≤900 行，tagent import ≤5。
- 行为等价：golden match＋wechat test。
