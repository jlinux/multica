# 企业微信访客配置界面：本地验收

日期：2026-10-05。范围：`feat/wecom-guest-access` 上的配置界面、数据库授权接管及任务授权检查。本次未部署，未发送真实企业微信消息，也未调用模型。

## 已实现

设置 → 企业微信 → 访客访问。工作区 owner/admin 可配置无账号访问开关、授权群、私聊权限；普通成员只看到状态和群数量。复用既有安装记录、身份解析和按群内发送人隔离的会话机制，无需新增表或迁移。

首次原样保存保留环境授权的上下文；数据库显式关闭不会回退至环境变量。实际授权变化生成新的授权代次，旧的排队任务和重试不能在关闭再开启后恢复。已有相同机器人轮换凭据保留授权；新安装、撤销、换绑和重新安装均显式关闭，需管理员重新授权。只有升级前已存在且未保存数据库策略的活动安装继续使用环境授权。

## 自动测试

使用专用本地 PostgreSQL（15439）和 Redis（16439），不连接生产服务。以下 Go 命令在 `server/` 下执行并设置这两个测试服务的环境变量：

```sh
go test -race -p 1 -json ./internal/channelaccess ./internal/integrations/channel/engine ./internal/integrations/wecom
go test -race -p 1 -json ./internal/service ./internal/handler -run 'Wecom|WeCom|Guest|Retry' -count=1
go test -race -p 1 -json ./internal/channelaccess ./internal/integrations/wecom ./internal/service ./internal/handler -run 'TestEffectiveWecom|TestWecomGuestPolicy|TestWecomGuestAccess|TestWecomGuestTaskAuthorization' -count=1
go vet ./internal/channelaccess ./internal/integrations/wecom ./internal/service ./internal/handler ./cmd/server
```

结果：依次 876、65、16 个测试及子测试通过，均无失败或跳过；vet 通过。最后一轮针对最终配置异常校验改动，覆盖实际数据库保存、并发、生命周期、所有者变化和撤销后的任务授权。

生命周期复核发现并修复：机器人跨工作区迁移后，删除最后一条安装记录再装回原工作区，可能恢复旧环境授权。加入真实数据库回归，并对 WeCom 包重新运行 race 测试：341 个顶层测试、603 个测试及子测试通过，无跳过或竞争告警。

连接复核发现并修复：通用连接管理器原先将整份安装配置用于连接指纹，访客配置保存会触发不必要的重连。现仅对企业微信排除访客授权字段，并规范化其余配置；凭据和机器人身份变化仍触发正常重连，其他渠道行为不变。实际保存与安装读取的集成用例通过；Lark 通用安装存储和共享连接引擎 race 测试共 742 个测试及子测试通过，无失败或跳过，相关 vet 通过。

前端：核心包 279 个测试、界面包 26 个测试通过。核心包、共享界面、Web、桌面端的类型检查全部通过；修改文件的 ESLint 和差异空白检查通过。文档 MDX 生成通过。

```sh
pnpm --filter @multica/core exec vitest run api/wecom-guest-access.test.ts api/schemas.test.ts api/client.test.ts wecom/guest-access.test.ts wecom/mutations.test.tsx
pnpm --filter @multica/views exec vitest run settings/components/wecom-tab.test.tsx settings/components/wecom-guest-access-dialog.test.tsx
pnpm --filter @multica/core typecheck
pnpm --filter @multica/views typecheck
pnpm --filter @multica/web --filter @multica/desktop typecheck
```

## 浏览器验收

Chromium 加载真实共享 `WecomTab`、API 客户端、查询/身份/语言提供器及项目样式，HTTP 使用本地模拟响应。后端权限与持久化由上述真实数据库测试覆盖；此浏览器验收不代表企业微信平台到模型的完整端到端测试。

六组场景通过：

1. 现有四群授权展示、原样接管、重新打开后显示保存者和时间。
2. 手动添加和选择已有群、移除群、私聊开关、保存失败保留输入、409 冲突阻止覆盖并可显式重新加载、关闭后列表状态更新。
3. 加载失败不能保存，重试成功恢复表单。
4. 格式异常响应不能形成空白可写配置。
5. 普通成员只读，不请求完整配置接口。
6. 英文 390 像素窄屏显示，长群 ID 换行，无横向溢出；所有场景无浏览器运行错误。

验收发现并修复：动态增删群导致基于索引的复选框标签错位；现改为基于群身份的稳定标识，并加入回归测试。启用却未授权任何群或私聊时，也会提示并禁止保存。

本机证据位于 `/tmp/wecom-ui-browser-results.json`，截图为 `/tmp/wecom-ui-dialog-zh.png`、`/tmp/wecom-ui-mobile-en.png`、`/tmp/wecom-ui-member.png`。临时验收页面已移出仓库；没有增加产品内测试入口。

## 代码复核

前端与后端分别完成独立规格及质量复核，整分支复核未发现待修复的代码问题。复核中发现的生命周期授权恢复、保存触发连接重启以及旧运维文档授权说明均纳入修复。

专用本地测试 PostgreSQL 和 Redis 已停止，功能分支保留，未合并或推送。

## 发布边界

需同时发布后端与前端，先确保所有后端副本更新。线上发布仍需先备份及验证恢复。回滚到仅认识环境授权的旧后端时，必须同步核对或关闭环境授权，否则数据库里的撤销状态不会被旧代码读取。

领取任务的撤权验证由真实数据库服务测试、领取路径代码复核及领取失败处理测试共同覆盖，未额外执行“保存关闭后调用领取 HTTP 接口”的完整串联用例。

本次没有运行整个仓库的全部测试或生产构建；验证范围为上述受影响模块、跨端类型检查及本地浏览器操作。实际机器人回答仍要求原有执行运行端在线。
