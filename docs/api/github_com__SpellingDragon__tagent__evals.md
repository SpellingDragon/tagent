package evals // import "github.com/SpellingDragon/tagent/evals"

Package evals 是组件级行为评估的执行体：票据可召回率（零幻觉契约）、畸形票据必须显式拒绝、 工具面 op 路由白名单、handoff
四段契约存在性。全部断言在 mock 下即可判红，故进 CI。

契约: docs/wiki/platform/evaluation-suites.md
