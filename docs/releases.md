# 发版记录

每次 TestFlight 上传一行，`scripts/build-ios.sh` 上传成功后自动追加（`--archive-only` 不记）。commit 带 `-dirty` 表示打包时工作区有未提交改动。

| 日期 | 版本 | build | commit | 改了什么 |
|---|---|---|---|---|
| 2026-11-01 | 0.1.0 | 202611020527 | 8e84bda-dirty | 首个内测包：扫码配对、通话、挂断 |
